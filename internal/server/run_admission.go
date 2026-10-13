package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

// A request flight pins its identity across receipt I/O and runtime preparation.
// Its session reservation prevents branch/permission mutations before acceptance.
type runAdmission struct {
	prompt, sessionID string
	inputs            promptInputs
	done              chan struct{}
	run               *sessionRun
	err               error
}

func sessionBusy(state *sessionRunState) bool {
	return state != nil && (state.admission != nil || state.run != nil && runActive(state.run))
}

func retryRun(run *sessionRun, sessionID, prompt string, inputs promptInputs) (*sessionRun, bool, error) {
	if run.Prompt != prompt || !sameInputs(run.Inputs, inputs) || (sessionID != "" && run.SessionID != sessionID) {
		return nil, false, &runAdmissionError{"request_conflict", "request_id 已用于另一条消息"}
	}
	return run, true, nil
}

func (s *Server) startRun(sessionID, prompt, requestID string, values ...promptInputs) (run *sessionRun, duplicate bool, err error) {
	inputs := promptInputs{}
	if len(values) > 0 {
		inputs = values[0]
	}
	if len(requestID) > 128 {
		return nil, false, &runAdmissionError{"invalid_request_id", "request_id 不能超过 128 字节"}
	}
	if requestID == "" {
		requestID = newRunID("request_")
	}
	g := s.runs
	g.mu.Lock()
	if previous := g.requests[requestID]; previous != nil {
		g.mu.Unlock()
		return retryRun(previous, sessionID, prompt, inputs)
	}
	if flight := g.admissions[requestID]; flight != nil {
		conflict := flight.prompt != prompt || !sameInputs(flight.inputs, inputs) || sessionID != "" && flight.sessionID != "" && flight.sessionID != sessionID
		g.mu.Unlock()
		if conflict {
			return nil, false, &runAdmissionError{"request_conflict", "request_id 已用于另一条消息"}
		}
		select {
		case <-flight.done:
			if flight.err != nil {
				return nil, false, flight.err
			}
			return retryRun(flight.run, sessionID, prompt, inputs)
		case <-s.ctx.Done():
			return nil, false, s.ctx.Err()
		}
	}
	flight := &runAdmission{prompt: prompt, sessionID: sessionID, inputs: inputs, done: make(chan struct{})}
	g.admissions[requestID] = flight
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		if state := g.sessions[flight.sessionID]; state != nil && state.admission == flight {
			state.admission = nil
		}
		flight.run, flight.err = run, err
		delete(g.admissions, requestID)
		close(flight.done)
		g.mu.Unlock()
	}()

	// Disk access is outside the registry lock. The flight excludes another
	// acceptance for the same identity until this lookup is published.
	previous, loadErr := s.loadRunReceipt(requestID)
	if loadErr != nil {
		return nil, false, loadErr
	}
	if previous != nil {
		g.mu.Lock()
		g.rememberRunLocked(previous)
		if state := g.sessionLocked(previous.SessionID); state.run == nil {
			state.run = previous
		}
		g.mu.Unlock()
		return retryRun(previous, sessionID, prompt, inputs)
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > 128*1024 {
		return nil, false, &runAdmissionError{"invalid_prompt", "prompt is empty"}
	}
	if s.app.DynamicWorkflows().ActiveActorSession(sessionID) {
		return nil, false, &runAdmissionError{"workflow_busy", "此 Actor 正由工作流管理"}
	}
	g.mu.Lock()
	if sessionID != "" {
		state := g.sessionLocked(sessionID)
		if sessionBusy(state) {
			g.mu.Unlock()
			return nil, false, &runAdmissionError{"busy", "此会话正在处理消息，请等待完成或取消当前任务"}
		}
		state.admission = flight
	}
	g.mu.Unlock()
	release, ok := s.activity.begin()
	if !ok {
		return nil, false, &runAdmissionError{"updating", "服务正在更新，请稍后重试；消息尚未执行"}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	accepted := false
	defer func() {
		if !accepted {
			cancel()
			release()
		}
	}()
	sess, err := s.resolveSession(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}
	if sess.SessionID() != sessionID {
		g.mu.Lock()
		if state := g.sessions[flight.sessionID]; state != nil && state.admission == flight {
			state.admission = nil
		}
		flight.sessionID = sess.SessionID()
		g.sessionLocked(flight.sessionID).admission = flight
		g.mu.Unlock()
	}
	if sess.Agent().State() == agent.StateRunning {
		return nil, false, &runAdmissionError{"busy", "此会话正在处理另一入口的请求"}
	}
	baseline, err := sess.Session().BuildDisplayContext(ctx)
	if err != nil {
		return nil, false, err
	}
	message, err := s.buildPromptMessage(sess, prompt, inputs)
	if err != nil {
		return nil, false, &runAdmissionError{"invalid_context", err.Error()}
	}
	// Keep the runtime's busy guard and MCP tool lease as the authoritative
	// execution boundary; the reservation only protects server-side mutations.
	if err := sess.TrySetConfirmFunc(s.confirmRunTool); err != nil {
		return nil, false, admissionRuntimeError(err)
	}
	run = &sessionRun{Inputs: inputs, ID: newRunID("run_"), RequestID: requestID, SessionID: sess.SessionID(), Prompt: prompt, DisplayPrompt: message.DisplayText, State: "running", StartedAt: time.Now(), baseline: serializeSessionContext(sess, baseline), pending: make(map[string]*pendingConfirmation), done: make(chan struct{}), cancel: cancel, ready: make(chan struct{})}
	run.Attachments = s.resolvePromptAttachments(run.SessionID, inputs.Attachments)
	if err := s.saveRunReceipt(run); err != nil {
		return nil, false, fmt.Errorf("无法保存消息接受凭据，消息尚未执行")
	}
	s.beginFiles(run, sess.Workspace())
	unsubscribeFiles := sess.Agent().Subscribe(func(eventCtx context.Context, event agent.AgentEvent) {
		if eventCtx.Value(runContextKey{}) != run {
			return
		}
		switch e := event.(type) {
		case agent.EventToolExecutionStart:
			s.observeFiles(run, agent.AgentStreamEvent{Type: agent.StreamEventToolStart, ToolName: e.ToolName, ToolCallID: e.ToolCallID, ToolArgs: e.Args})
		case agent.EventToolExecutionEnd:
			s.observeFiles(run, agent.AgentStreamEvent{Type: agent.StreamEventToolEnd, ToolName: e.ToolName, ToolCallID: e.ToolCallID, IsError: e.IsError})
		}
	})
	stream, err := sess.PromptMessageStream(context.WithValue(ctx, runContextKey{}, run), message)
	if err != nil {
		unsubscribeFiles()
		if removeErr := os.Remove(s.receiptPath(requestID)); removeErr != nil {
			slog.Error("cannot remove rejected run receipt", "request_id", requestID, "error", removeErr)
		}
		return nil, false, admissionRuntimeError(err)
	}
	g.mu.Lock()
	state := g.sessionLocked(run.SessionID)
	state.run = run
	state.admission = nil // the accepted run now owns session exclusion
	state.replay, state.replayBytes = nil, 0
	g.rememberRunLocked(run)
	g.publishLocked(state, wsServerMessage{Type: "accepted", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID, State: run.State, Prompt: displayRunPrompt(run), Run: cloneRun(run)})
	close(run.ready)
	g.mu.Unlock()
	accepted = true
	go func() { defer unsubscribeFiles(); s.consumeRun(ctx, run, stream, release) }()
	return run, false, nil
}

func (g *runRegistry) rememberRunLocked(run *sessionRun) {
	g.requests[run.RequestID] = run
	g.requestOrder = append(g.requestOrder, run.RequestID)
	g.pruneRequestsLocked()
}

func admissionRuntimeError(err error) error {
	if errors.Is(err, agent.ErrAgentBusy) {
		return &runAdmissionError{"busy", "此会话正在处理另一入口的请求"}
	}
	if errors.Is(err, runtime.ErrToolsUpdating) {
		return &runAdmissionError{"updating", err.Error()}
	}
	return err
}
