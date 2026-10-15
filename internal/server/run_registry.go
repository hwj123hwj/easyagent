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
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/tools"
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
	UserEntryID      string   `json:"user_entry_id,omitempty"`
	AssistantEntryID string   `json:"assistant_entry_id,omitempty"`
	Usage            ai.Usage `json:"usage,omitempty"` // cumulative tokens across all turns
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
	ready            chan struct{} // accepted boundary precedes tool approvals
}

type sessionRunState struct {
	seq         uint64
	run         *sessionRun
	admission   *runAdmission
	replay      []wsServerMessage
	replayBytes int
	subscribers map[*wsConn]struct{}
}

type runRegistry struct {
	mu                  sync.Mutex
	sessions            map[string]*sessionRunState
	requests            map[string]*sessionRun
	admissions          map[string]*runAdmission
	requestOrder        []string
	confirmationTimeout time.Duration
}

func newRunRegistry() *runRegistry {
	return &runRegistry{sessions: make(map[string]*sessionRunState), requests: make(map[string]*sessionRun), admissions: make(map[string]*runAdmission), confirmationTimeout: 2 * time.Minute}
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
	var lastTurnEnd ai.Usage
	for event := range stream {
		s.runs.mu.Lock()
		state := s.runs.sessionLocked(run.SessionID)
		if state.run == run {
			if event.Type == agent.StreamEventDone {
				run.last = event.FinalMessage
				// 最后一轮以 error/aborted 结束时不发 turn_end（loop.go 提前
				// 返回），其用量只能在这里补记；成功路径的最后一轮已计入，
				// 用量相同则跳过。极小概率两轮用量完全一致导致漏记一轮，
				// 优于整体漏记失败 run 的收尾轮。
				if event.FinalMessage.Usage != lastTurnEnd {
					addTurnUsage(&run.Usage, event.FinalMessage.Usage)
				}
			}
			if event.Type == agent.StreamEventTurnEnd {
				// Every turn reports its own request usage; the run total is the
				// sum across turns (tool loops issue one LLM call per turn).
				if assistant, ok := event.Message.(ai.AssistantMessage); ok {
					addTurnUsage(&run.Usage, assistant.Usage)
					lastTurnEnd = assistant.Usage
				}
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

// addTurnUsage 把单轮请求的用量累加进 run 的累计值；CachedInputTokens 首次
// 出现时才分配指针，nil 表示该轮未上报缓存命中。
func addTurnUsage(total *ai.Usage, turn ai.Usage) {
	total.InputTokens += turn.InputTokens
	total.OutputTokens += turn.OutputTokens
	if turn.CachedInputTokens != nil {
		if total.CachedInputTokens == nil {
			total.CachedInputTokens = new(int)
		}
		*total.CachedInputTokens += *turn.CachedInputTokens
	}
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
	if run.ready != nil {
		select {
		case <-run.ready:
		case <-ctx.Done():
			return agent.ConfirmDecision{Reason: "任务已取消，操作未执行"}
		}
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
	// Questions are user-driven: do not silently expire them on the short
	// dangerous-action approval timer. Explicit reply/skip/cancel resolves them.
	var deadline <-chan time.Time
	if request.ToolName != "ask_user_question" {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		deadline = timer.C
	}
	decision := agent.ConfirmDecision{Reason: "确认等待超时，操作未执行"}
	select {
	case decision = <-pending.decision:
	case <-ctx.Done():
		decision.Reason = "任务已取消，操作未执行"
	case <-deadline:
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

func (s *Server) confirmRun(sessionID, runID, confirmationID string, approved bool, reason string, response ...json.RawMessage) error {
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
	decision := agent.ConfirmDecision{Approved: approved, Reason: reason}
	if pending.ToolName == "ask_user_question" && approved {
		if len(response) > 0 {
			decision.Response = response[0]
		}
		if _, err := tools.ValidateAskUserAnswers(pending.Args, decision.Response); err != nil {
			return &runAdmissionError{"invalid_answers", err.Error()}
		}
	}
	if pending.ToolName == "ask_user_question" && !approved && reason == "" {
		decision.Reason = "User skipped the questions; no answers were submitted. Ask in plain text if still necessary."
	}
	select {
	case pending.decision <- decision:
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
	if state == nil || sessionBusy(state) {
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
	if sessionBusy(state) {
		writeError(w, http.StatusConflict, "此会话正在执行任务，请先取消或等待完成")
		return true
	}
	return false
}

func serializeRunMessages(messages []ai.Message, sessionIDs ...string) []map[string]any {
	sessionID := ""
	if len(sessionIDs) > 0 {
		sessionID = sessionIDs[0]
	}
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
					image := map[string]string{"attachmentId": historyImageID(block.Image), "sessionId": sessionID}
					if sessionID == "" {
						image = map[string]string{"data_url": "data:" + block.Image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(block.Image.Data)}
					}
					entry["images"] = append(images, image)
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
		RunID          string          `json:"run_id"`
		ConfirmationID string          `json:"confirmation_id"`
		Approved       bool            `json:"approved"`
		Reason         string          `json:"reason"`
		Answers        json.RawMessage `json:"answers"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&request) != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := s.confirmRun(r.PathValue("id"), request.RunID, request.ConfirmationID, request.Approved, request.Reason, request.Answers); err != nil {
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
		if admission.code == "invalid_prompt" || admission.code == "invalid_request_id" || admission.code == "invalid_answers" {
			code = http.StatusBadRequest
		}
		if admission.code == "updating" {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "code": admission.code})
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
