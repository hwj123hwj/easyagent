package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

const runReplayLimit = 256
const runReplayBytesLimit = 1024 * 1024
const runRequestLimit = 512

type runContextKey struct{}

type pendingConfirmation struct {
	ID          string          `json:"confirmation_id"`
	ToolCallID  string          `json:"tool_call_id"`
	ToolName    string          `json:"tool_name"`
	Description string          `json:"description"`
	Args        json.RawMessage `json:"args"`
	decision    chan agent.ConfirmDecision
}

// sessionRun is owned by the session, never by a socket or HTTP request.
type sessionRun struct {
	UserEntryID      string `json:"user_entry_id,omitempty"`
	AssistantEntryID string `json:"assistant_entry_id,omitempty"`
	files            *runFiles
	DisplayPrompt    string            `json:"display_prompt,omitempty"`
	Inputs           promptInputs      `json:"inputs,omitempty"`
	Attachments      []inputAttachment `json:"attachments,omitempty"`
	ID               string            `json:"run_id"`
	RequestID        string            `json:"request_id"`
	SessionID        string            `json:"session_id"`
	Prompt           string            `json:"prompt"`
	State            string            `json:"state"`
	StartedAt        time.Time         `json:"started_at"`
	EndedAt          *time.Time        `json:"ended_at,omitempty"`
	Error            string            `json:"error,omitempty"`
	baseline         []map[string]any
	projection       []agent.AgentStreamEvent
	projectionText   strings.Builder
	pending          map[string]*pendingConfirmation
	last             ai.AssistantMessage
	restored         bool
	done             chan struct{}
	cancel           context.CancelFunc
}

type sessionRunState struct {
	seq         uint64
	run         *sessionRun
	replay      []wsServerMessage
	replayBytes int
	subscribers map[*wsConn]struct{}
}

type runRegistry struct {
	mu                  sync.Mutex
	sessions            map[string]*sessionRunState
	requests            map[string]*sessionRun
	requestOrder        []string
	confirmationTimeout time.Duration
}

func newRunRegistry() *runRegistry {
	return &runRegistry{sessions: make(map[string]*sessionRunState), requests: make(map[string]*sessionRun), confirmationTimeout: 2 * time.Minute}
}

func newRunID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

type runAdmissionError struct{ code, message string }

func (e *runAdmissionError) Error() string { return e.message }

func (s *Server) startRun(sessionID, prompt, requestID string, values ...promptInputs) (*sessionRun, bool, error) {
	inputs := promptInputs{}
	if len(values) > 0 {
		inputs = values[0]
	}
	g := s.runs
	g.mu.Lock()
	defer g.mu.Unlock()
	if requestID != "" {
		previous := g.requests[requestID]
		if previous == nil {
			var err error
			previous, err = s.loadRunReceiptLocked(requestID)
			if err != nil {
				return nil, false, err
			}
		}
		if previous != nil {
			if previous.Prompt != prompt || !sameInputs(previous.Inputs, inputs) || (sessionID != "" && previous.SessionID != sessionID) {
				return nil, false, &runAdmissionError{"request_conflict", "request_id 已用于另一条消息"}
			}
			return previous, true, nil
		}
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > 128*1024 {
		return nil, false, &runAdmissionError{"invalid_prompt", "prompt is empty"}
	}
	if len(requestID) > 128 {
		return nil, false, &runAdmissionError{"invalid_request_id", "request_id 不能超过 128 字节"}
	}
	if s.app.DynamicWorkflows().ActiveActorSession(sessionID) {
		return nil, false, &runAdmissionError{"workflow_busy", "此 Actor 正由工作流管理"}
	}
	if state := g.sessions[sessionID]; state != nil && state.run != nil && runActive(state.run) {
		return nil, false, &runAdmissionError{"busy", "此会话正在处理消息，请等待完成或取消当前任务"}
	}
	release, ok := s.activity.begin()
	if !ok {
		return nil, false, &runAdmissionError{"updating", "服务正在更新，请稍后重试；消息尚未执行"}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	sess, err := s.resolveSession(ctx, sessionID)
	if err != nil {
		cancel()
		release()
		return nil, false, err
	}
	if sess.Agent().State() == agent.StateRunning {
		cancel()
		release()
		return nil, false, &runAdmissionError{"busy", "此会话正在处理另一入口的请求"}
	}
	baseline, err := sess.Session().BuildDisplayContext(ctx)
	message, inputErr := s.buildPromptMessage(sess, prompt, inputs)
	if inputErr != nil {
		cancel()
		release()
		return nil, false, &runAdmissionError{"invalid_context", inputErr.Error()}
	}
	if err != nil {
		cancel()
		release()
		return nil, false, err
	}
	// 与 runtime 的 busy guard 协作，不能在别的入口启动期间替换审批所有者。
	if err := sess.TrySetConfirmFunc(s.confirmRunTool); err != nil {
		cancel()
		release()
		if errors.Is(err, agent.ErrAgentBusy) {
			return nil, false, &runAdmissionError{"busy", "此会话正在处理另一入口的请求"}
		}
		return nil, false, err
	}
	if requestID == "" {
		requestID = newRunID("request_")
	}
	run := &sessionRun{Inputs: inputs, ID: newRunID("run_"), RequestID: requestID, SessionID: sess.SessionID(), Prompt: prompt, State: "running", StartedAt: time.Now(), baseline: serializeSessionContext(sess, baseline), pending: make(map[string]*pendingConfirmation), done: make(chan struct{}), cancel: cancel}
	if err := s.saveRunReceipt(run); err != nil {
		cancel()
		release()
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
	// 审批回调已在启动前安装，断线也不会把需要确认的工具变成自动放行。
	stream, err := sess.PromptMessageStream(context.WithValue(ctx, runContextKey{}, run), message)
	if err != nil {
		unsubscribeFiles()
		cancel()
		release()
		if removeErr := os.Remove(s.receiptPath(requestID)); removeErr != nil {
			slog.Error("cannot remove rejected run receipt", "request_id", requestID, "error", removeErr)
		}
		if errors.Is(err, agent.ErrAgentBusy) {
			return nil, false, &runAdmissionError{"busy", "此会话正在处理另一入口的请求"}
		}
		if errors.Is(err, runtime.ErrToolsUpdating) {
			return nil, false, &runAdmissionError{"updating", err.Error()}
		}
		return nil, false, err
	}
	// Admission failures have not executed the message and must remain retryable
	// with the same request_id; only accepted executions enter the registry.
	state := g.sessionLocked(run.SessionID)
	state.run = run
	run.DisplayPrompt = message.DisplayText
	run.Attachments = s.resolvePromptAttachments(run.SessionID, run.Inputs.Attachments)
	state.replay, state.replayBytes = nil, 0
	g.requests[requestID] = run
	g.requestOrder = append(g.requestOrder, requestID)
	g.pruneRequestsLocked()
	// Existing subscribers need an authoritative new-run boundary before its deltas,
	// including turns admitted by the server-owned queue.
	g.publishLocked(state, wsServerMessage{Type: "accepted", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID, State: run.State, Prompt: displayRunPrompt(run), Run: cloneRun(run)})
	go func() { defer unsubscribeFiles(); s.consumeRun(ctx, run, stream, release) }()
	return run, false, nil
}

func runActive(run *sessionRun) bool {
	return run.State == "running" || run.State == "waiting_confirmation"
}

func displayRunPrompt(run *sessionRun) string {
	if run.DisplayPrompt != "" {
		return run.DisplayPrompt
	}
	return run.Prompt
}

func (g *runRegistry) sessionLocked(id string) *sessionRunState {
	state := g.sessions[id]
	if state == nil {
		state = &sessionRunState{subscribers: make(map[*wsConn]struct{})}
		g.sessions[id] = state
	}
	return state
}

func (g *runRegistry) pruneRequestsLocked() {
	if len(g.requests) <= runRequestLimit {
		return
	}
	kept := g.requestOrder[:0]
	for _, id := range g.requestOrder {
		run := g.requests[id]
		if len(g.requests) > runRequestLimit && run != nil && !runActive(run) {
			delete(g.requests, id)
			continue
		}
		kept = append(kept, id)
	}
	g.requestOrder = kept
}

func (g *runRegistry) publishLocked(state *sessionRunState, msg wsServerMessage) {
	state.seq++
	msg.Seq = state.seq
	state.replay = append(state.replay, msg)
	encoded, _ := json.Marshal(msg)
	state.replayBytes += len(encoded)
	for len(state.replay) > runReplayLimit || state.replayBytes > runReplayBytesLimit {
		old, _ := json.Marshal(state.replay[0])
		state.replayBytes -= len(old)
		state.replay[0] = wsServerMessage{}
		state.replay = state.replay[1:]
	}
	for ws := range state.subscribers {
		_ = ws.writeJSON(msg)
	}
}

func (s *Server) consumeRun(ctx context.Context, run *sessionRun, stream <-chan agent.AgentStreamEvent, release func()) {
	defer func() {
		if s.ctx.Err() != nil {
			return
		}
		if run.State == "failed" {
			_, _ = s.changeQueue(run.SessionID, queueMutation{Action: "pause"})
		} else {
			s.pumpQueue(run.SessionID)
		}
	}()
	defer release()
	defer run.cancel()
	for event := range stream {
		s.runs.mu.Lock()
		state := s.runs.sessionLocked(run.SessionID)
		if state.run == run {
			if event.Type == agent.StreamEventDone {
				run.last = event.FinalMessage
			}
			if event.Type == agent.StreamEventError {
				run.Error = event.Error
			}
			appendRunProjection(run, event)
			s.runs.publishLocked(state, wsServerMessage{Type: "event", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID, Event: event})
		}
		s.runs.mu.Unlock()
	}
	s.finishFiles(run)
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	// 旧 run 的迟到收尾不得清除后来任务的运行状态。
	state := s.runs.sessionLocked(run.SessionID)
	if ctx.Err() != nil {
		run.State = "cancelled"
	} else if run.Error != "" {
		run.State = "failed"
	} else {
		run.State = "completed"
	}
	if sess, ok := s.app.SessionStore().Get(run.SessionID); ok {
		messages, err := sess.Session().BuildContext(s.ctx)
		ids, idErr := sess.Session().BuildContextEntryIDs(s.ctx)
		if err == nil && idErr == nil && len(ids) == len(messages) {
			baselineIDs := make(map[string]bool, len(run.baseline))
			for _, entry := range run.baseline {
				id, _ := entry["entry_id"].(string)
				baselineIDs[id] = true
			}
			for i := len(messages) - 1; i >= 0; i-- {
				if user, ok := messages[i].(ai.UserMessage); ok && !baselineIDs[ids[i]] && len(user.Content) > 0 && user.Content[0].Type == "text" && user.Content[0].Text == run.Prompt {
					run.UserEntryID = ids[i]
					// Only the terminal answer after this run's user is a reply fork
					// anchor. Never attach an earlier answer to an empty/failed run.
					if run.State == "completed" && i < len(messages)-1 {
						last := len(messages) - 1
						if answer, ok := messages[last].(ai.AssistantMessage); ok && answer.Text != "" && len(answer.ToolCalls) == 0 && answer.StopReason != ai.StopReasonError && answer.StopReason != ai.StopReasonAborted && answer.StopReason != ai.StopReasonToolUse {
							run.AssistantEntryID = ids[last]
						}
					}
					break
				}
			}
		}
	}
	ended := time.Now()
	run.EndedAt = &ended
	if err := s.saveRunReceipt(run); err != nil {
		slog.Error("cannot save final run receipt", "request_id", run.RequestID, "error", err)
	}
	if state.run == run {
		s.runs.publishLocked(state, wsServerMessage{Type: "status", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID, State: run.State, Streaming: false, Message: run.Error, Run: cloneRun(run)})
	}
	close(run.done)
	s.runs.pruneRequestsLocked()
}

// 文本片段合并，工具进度只保留最后一次；恢复快照不依赖有限 replay 窗口。
func appendRunProjection(run *sessionRun, event agent.AgentStreamEvent) {
	if event.Type == agent.StreamEventTextDelta || event.Type == agent.StreamEventThinkingDelta {
		if len(run.projection) == 0 || run.projection[len(run.projection)-1].Type != event.Type {
			if run.projectionText.Len() > 0 {
				run.projection[len(run.projection)-1].TextDelta = run.projectionText.String()
				run.projectionText.Reset()
			}
			run.projection = append(run.projection, agent.AgentStreamEvent{Type: event.Type, Timestamp: event.Timestamp})
		}
		run.projectionText.WriteString(event.TextDelta)
		return
	}
	if run.projectionText.Len() > 0 {
		run.projection[len(run.projection)-1].TextDelta = run.projectionText.String()
		run.projectionText.Reset()
	}
	if event.Type == agent.StreamEventToolUpdate {
		for i := len(run.projection) - 1; i >= 0; i-- {
			previous := run.projection[i]
			if previous.ToolCallID == event.ToolCallID && previous.Type == event.Type {
				run.projection[i] = event
				return
			}
		}
	}
	run.projection = append(run.projection, event)
}

func runProjection(run *sessionRun) []agent.AgentStreamEvent {
	events := append([]agent.AgentStreamEvent{}, run.projection...)
	if run.projectionText.Len() > 0 {
		events[len(events)-1].TextDelta = run.projectionText.String()
	}
	return events
}

func (s *Server) confirmRunTool(ctx context.Context, request agent.ConfirmationRequest) agent.ConfirmDecision {
	run, ok := ctx.Value(runContextKey{}).(*sessionRun)
	if !ok {
		return agent.ConfirmDecision{Reason: "此执行入口没有交互审批通道"}
	}
	pending := &pendingConfirmation{ID: newRunID("confirm_"), ToolCallID: request.ToolCallID, ToolName: request.ToolName, Description: request.Description, Args: request.Args, decision: make(chan agent.ConfirmDecision, 1)}
	s.runs.mu.Lock()
	state := s.runs.sessionLocked(run.SessionID)
	if state.run != run || !runActive(run) {
		s.runs.mu.Unlock()
		return agent.ConfirmDecision{Reason: "任务已结束"}
	}
	run.pending[pending.ID] = pending
	run.State = "waiting_confirmation"
	s.runs.publishLocked(state, wsServerMessage{Type: "confirmation", SessionID: run.SessionID, RunID: run.ID, Confirmation: pending, State: run.State})
	wait := s.runs.confirmationTimeout
	s.runs.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	decision := agent.ConfirmDecision{Reason: "确认等待超时，操作未执行"}
	select {
	case decision = <-pending.decision:
	case <-ctx.Done():
		decision.Reason = "任务已取消，操作未执行"
	case <-timer.C:
	}
	s.runs.mu.Lock()
	delete(run.pending, pending.ID)
	if state.run == run {
		s.runs.publishLocked(state, wsServerMessage{Type: "confirmed", SessionID: run.SessionID, RunID: run.ID, ConfirmationID: pending.ID})
	}
	if len(run.pending) == 0 && runActive(run) {
		run.State = "running"
		if state.run == run {
			s.runs.publishLocked(state, wsServerMessage{Type: "status", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID, State: run.State, Streaming: true})
		}
	}
	s.runs.mu.Unlock()
	return decision
}

func (s *Server) confirmRun(sessionID, runID, confirmationID string, approved bool, reason string) error {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	state := s.runs.sessions[sessionID]
	if state == nil || state.run == nil || state.run.ID != runID || !runActive(state.run) {
		return &runAdmissionError{"stale_run", "任务已结束或 run_id 已过期"}
	}
	pending := state.run.pending[confirmationID]
	if pending == nil {
		return &runAdmissionError{"stale_confirmation", "确认请求不存在或已经处理"}
	}
	select {
	case pending.decision <- agent.ConfirmDecision{Approved: approved, Reason: reason}:
		delete(state.run.pending, confirmationID)
		return nil
	default:
		return &runAdmissionError{"stale_confirmation", "确认请求已经处理"}
	}
}

func (s *Server) cancelRun(sessionID, runID string) error {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	state := s.runs.sessions[sessionID]
	if runID == "" {
		return &runAdmissionError{"run_id_required", "取消必须指定 run_id"}
	}
	if state == nil || state.run == nil || state.run.ID != runID || !runActive(state.run) {
		return &runAdmissionError{"stale_run", "任务已结束或 run_id 已过期"}
	}
	state.run.cancel()
	return nil
}

func (s *Server) runSnapshot(sessionID string, afterSeq uint64, runID string, ws *wsConn) (wsServerMessage, error) {
	queue, queueErr := s.readQueue(sessionID)
	if queueErr != nil {
		return wsServerMessage{}, queueErr
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	state := s.runs.sessionLocked(sessionID)
	msg := wsServerMessage{Type: "snapshot", SessionID: sessionID, Seq: state.seq, Run: cloneRun(state.run), Queue: &queue}
	if state.run != nil {
		if state.run.restored {
			msg.Reset = true
			if err := s.hydrateRestoredRun(state.run); err != nil {
				return wsServerMessage{}, err
			}
		}
		msg.RunID = state.run.ID
		msg.PendingConfirmations = make([]*pendingConfirmation, 0, len(state.run.pending))
		for _, pending := range state.run.pending {
			msg.PendingConfirmations = append(msg.PendingConfirmations, pending)
		}
		if afterSeq > 0 && afterSeq <= state.seq && (runID == "" || runID == state.run.ID) && (afterSeq == state.seq || len(state.replay) > 0 && afterSeq >= state.replay[0].Seq-1) {
			msg.Type = "replay"
			replay := []wsServerMessage{}
			for _, event := range state.replay {
				if event.Seq > afterSeq {
					replay = append(replay, event)
				}
			}
			msg.Events = replay
		} else {
			msg.Messages = state.run.baseline
			msg.Events = runProjection(state.run)
		}
	} else {
		if !s.app.SessionManager().Exists(sessionID) {
			return wsServerMessage{}, fmt.Errorf("session not found")
		}
		sess, err := s.resolveSession(s.ctx, sessionID)
		if err != nil {
			return wsServerMessage{}, err
		}
		messages, err := sess.Session().BuildDisplayContext(s.ctx)
		if err != nil {
			return wsServerMessage{}, err
		}
		msg.Messages = serializeSessionContext(sess, messages)
		msg.Events = []agent.AgentStreamEvent{}
	}
	if ws != nil {
		// 快照先进入该 socket 的 FIFO，再挂订阅，避免先收到增量后收到旧快照。
		_ = ws.writeJSON(msg)
		if ws.legacy && msg.Type == "snapshot" {
			if events, ok := msg.Events.([]agent.AgentStreamEvent); ok {
				for _, event := range events {
					_ = ws.writeJSON(wsServerMessage{Type: "event", SessionID: sessionID, RunID: msg.RunID, Event: event})
				}
			}
		}
		state.subscribers[ws] = struct{}{}
	}
	return msg, nil
}

func (s *Server) unsubscribeRun(ws *wsConn, sessionID string) {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	for id, state := range s.runs.sessions {
		if sessionID == "" || id == sessionID {
			delete(state.subscribers, ws)
		}
	}
}

// History commands change the authoritative branch independently of the last run.
// Discard that obsolete projection and notify every attached client of the new base.
func (s *Server) invalidateRunSnapshot(sessionID string) {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	state := s.runs.sessions[sessionID]
	if state == nil || state.run != nil && runActive(state.run) {
		return
	}
	sess, err := s.resolveSession(s.ctx, sessionID)
	if err != nil {
		return
	}
	messages, err := sess.Session().BuildDisplayContext(s.ctx)
	if err != nil {
		return
	}
	state.run = nil
	state.replay = nil
	state.replayBytes = 0
	state.seq++
	msg := wsServerMessage{Type: "snapshot", SessionID: sessionID, Seq: state.seq, Messages: serializeSessionContext(sess, messages), Events: []agent.AgentStreamEvent{}}
	for subscriber := range state.subscribers {
		_ = subscriber.writeJSON(msg)
	}
}

func (s *Server) rejectActiveRun(w http.ResponseWriter, sessionID string) bool {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	state := s.runs.sessions[sessionID]
	if state != nil && state.run != nil && runActive(state.run) {
		writeError(w, http.StatusConflict, "此会话正在执行任务，请先取消或等待完成")
		return true
	}
	return false
}

func serializeRunMessages(messages []ai.Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		entry := map[string]any{"role": string(message.Role()), "content": ""}
		switch msg := message.(type) {
		case ai.UserMessage:
			text := ""
			for _, block := range msg.Content {
				if block.Type == "text" {
					text += block.Text
				}
				if block.Type == "image" && block.Image != nil {
					images, _ := entry["images"].([]map[string]string)
					entry["images"] = append(images, map[string]string{"data_url": "data:" + block.Image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(block.Image.Data)})
				}
			}
			entry["content"] = text
			if msg.DisplayText != "" {
				entry["content"] = msg.DisplayText
			}
		case ai.AssistantMessage:
			entry["content"], entry["thinking"], entry["tool_calls"] = msg.Text, msg.Thinking, msg.ToolCalls
			entry["stop_reason"] = msg.StopReason
			entry["thinking_duration_ms"], entry["usage"] = msg.ThinkingDurationMS, msg.Usage
		case ai.ToolResultMessage:
			entry["duration_ms"] = msg.DurationMS
			entry["content"], entry["tool_call_id"], entry["is_error"], entry["tool_details"] = msg.Content, msg.ToolCallID, msg.IsError, msg.Details
		}
		result = append(result, entry)
	}
	return result
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	msg, err := s.runSnapshot(r.PathValue("id"), 0, "", nil)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg)
}

func (s *Server) cancelRunHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RunID string `json:"run_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request) != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := s.cancelRun(r.PathValue("id"), request.RunID); err != nil {
		runHTTPError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"cancelled": true})
}

func (s *Server) confirmRunHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RunID          string `json:"run_id"`
		ConfirmationID string `json:"confirmation_id"`
		Approved       bool   `json:"approved"`
		Reason         string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request) != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := s.confirmRun(r.PathValue("id"), request.RunID, request.ConfirmationID, request.Approved, request.Reason); err != nil {
		runHTTPError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"confirmed": true})
}

func writeRunSSE(w http.ResponseWriter, eventType string, value any) {
	data, err := json.Marshal(value)
	if err == nil {
		writeSSE(w, eventType, string(data))
	}
}

func runHTTPError(w http.ResponseWriter, err error) {
	var admission *runAdmissionError
	if errors.As(err, &admission) {
		code := http.StatusConflict
		if admission.code == "invalid_prompt" || admission.code == "invalid_request_id" {
			code = http.StatusBadRequest
		}
		if admission.code == "updating" {
			code = http.StatusServiceUnavailable
		}
		writeError(w, code, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// Retained REST results are safe to read after done closes, while projections are always read under mu.
func (s *Server) waitRun(ctx context.Context, run *sessionRun) (ai.AssistantMessage, error) {
	select {
	case <-ctx.Done():
		return ai.AssistantMessage{}, ctx.Err()
	case <-run.done:
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	if run.State == "cancelled" {
		return run.last, context.Canceled
	}
	if run.Error != "" {
		return run.last, errors.New(run.Error)
	}
	return run.last, nil
}

func cloneRun(run *sessionRun) *sessionRun {
	if run == nil {
		return nil
	}
	copy := *run
	if run.restored {
		// The restored baseline already contains any persisted user/tool messages.
		copy.Prompt = ""
		copy.DisplayPrompt = ""
	}
	return &copy
}
