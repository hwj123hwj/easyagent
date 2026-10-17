package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type runInfo struct {
	Inputs         map[string]any   `json:"inputs"`
	ID             string           `json:"run_id"`
	RequestID      string           `json:"request_id"`
	State          string           `json:"state"`
	Prompt         string           `json:"prompt"`
	DisplayPrompt  string           `json:"display_prompt"`
	UserEntry      string           `json:"user_entry_id"`
	AssistantEntry string           `json:"assistant_entry_id"`
	Error          string           `json:"error"`
	Attachments    []map[string]any `json:"attachments"`
}

func (r *runInfo) active() bool {
	return r != nil && (r.State == "running" || r.State == "waiting_confirmation")
}

type confirmation struct {
	ID          string `json:"confirmation_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Args        any    `json:"args"`
}
type queueItem struct {
	ID, Prompt string
	Inputs     map[string]any
}
type messageQueue struct {
	Items    []queueItem
	Paused   bool
	Revision uint64
}
type storedMessage struct {
	DurationMS float64 `json:"duration_ms"`
	Role       string  `json:"role"`
	Content    any     `json:"content"`
	Thinking   string  `json:"thinking"`
	EntryID    string  `json:"entry_id"`
	ToolID     string  `json:"tool_call_id"`
	Error      bool    `json:"is_error"`
	StopReason string  `json:"stop_reason"`
	ToolCalls  []struct {
		ID, Name string
		Args     any
	} `json:"tool_calls"`
	Compaction map[string]any `json:"compaction"`
	Details    map[string]any `json:"tool_details"`
	Images     []messageImage `json:"images"`
}
type agentEvent struct {
	DurationMS float64        `json:"duration_ms"`
	Type       string         `json:"type"`
	Text       string         `json:"text_delta"`
	ToolID     string         `json:"tool_call_id"`
	ToolName   string         `json:"tool_name"`
	Args       any            `json:"tool_args"`
	Result     any            `json:"tool_result"`
	Partial    any            `json:"partial_result"`
	Details    map[string]any `json:"tool_details"`
	Error      bool           `json:"is_error"`
	ErrorText  string         `json:"error"`
	Summary    string         `json:"summary"`
	Usage      map[string]any `json:"context_usage"`
	Final      struct {
		Text    string
		Content any
	} `json:"final_message"`
}
type envelope struct {
	Type           string            `json:"type"`
	SessionID      string            `json:"session_id"`
	RequestID      string            `json:"request_id"`
	RunID          string            `json:"run_id"`
	Seq            uint64            `json:"seq"`
	Reset          bool              `json:"reset"`
	State          string            `json:"state"`
	Streaming      bool              `json:"streaming"`
	Prompt         string            `json:"prompt"`
	Message        string            `json:"message"`
	Error          string            `json:"error"`
	Run            *runInfo          `json:"run"`
	Messages       []storedMessage   `json:"messages"`
	Events         []json.RawMessage `json:"events"`
	Event          *agentEvent       `json:"event"`
	Confirmation   *confirmation     `json:"confirmation"`
	ConfirmationID string            `json:"confirmation_id"`
	Confirmations  []confirmation    `json:"pending_confirmations"`
	Queue          *messageQueue     `json:"queue"`
}

func displayText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, p := range v {
			if obj, ok := p.(map[string]any); ok {
				if str(obj["type"]) == "image" {
					b.WriteString("[图片]")
				} else {
					b.WriteString(displayText(obj["text"]))
				}
			} else {
				b.WriteString(displayText(p))
			}
		}
		return b.String()
	case map[string]any:
		for _, key := range []string{"UserFacing", "user_facing", "Content", "content", "text"} {
			if v[key] != nil {
				return displayText(v[key])
			}
		}
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
func str(v any) string { s, _ := v.(string); return s }
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func list(v any) []any   { s, _ := v.([]any); return s }
func boolean(v any) bool { b, _ := v.(bool); return b }
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
func (s *session) history(records []storedMessage) {
	s.Messages = nil
	s.Plan = nil
	tools := map[string]*message{}
	for _, m := range records {
		switch m.Role {
		case "user":
			s.Messages = append(s.Messages, &message{User: true, Text: displayText(m.Content), EntryID: m.EntryID, Images: m.Images})
		case "assistant":
			if m.Thinking != "" {
				s.Messages = append(s.Messages, &message{Tool: "思考过程", Text: m.Thinking, Kind: "thought"})
			}
			if text := displayText(m.Content); text != "" {
				id := m.EntryID
				if len(m.ToolCalls) > 0 || m.StopReason == "error" || m.StopReason == "aborted" {
					id = ""
				}
				s.Messages = append(s.Messages, &message{Text: text, EntryID: id})
			}
			for _, call := range m.ToolCalls {
				tool := &message{Tool: call.Name, CallID: call.ID, Args: call.Args, Text: displayText(call.Args), Kind: "tool", Status: "未完成"}
				tools[call.ID] = tool
				s.Messages = append(s.Messages, tool)
			}
		case "tool":
			if tool := tools[m.ToolID]; tool != nil {
				tool.Result = displayText(m.Content)
				if !m.Error {
					s.updatePlan(tool.Tool, tool.Args)
				}
				tool.Details = m.Details
				tool.DurationMS = m.DurationMS
				tool.Status = "完成"
				if m.Error {
					tool.Status = "失败"
				}
				if str(m.Details["approval"]) == "declined" {
					tool.Status = "已拒绝"
				}
			}
		case "compaction":
			s.Messages = append(s.Messages, &message{Tool: "上下文压缩", Text: str(m.Compaction["summary"]), Kind: "compaction"})
		}
	}
}
func (s *session) event(e agentEvent) {
	if e.Usage != nil {
		s.Usage = e.Usage
	}
	switch e.Type {
	case "text_delta", "thinking_delta":
		kind := ""
		if e.Type == "thinking_delta" {
			kind = "thought"
		}
		var m *message
		if len(s.Messages) > 0 {
			last := s.Messages[len(s.Messages)-1]
			if !last.User && last.Kind == kind && last.RunID == s.runID() && last.Tool == "" {
				m = last
			}
			if kind == "thought" && last.Kind == kind && last.RunID == s.runID() {
				m = last
			}
		}
		if m == nil {
			m = &message{Kind: kind, RunID: s.runID()}
			if kind == "thought" {
				m.Tool = "思考过程"
			}
			s.Messages = append(s.Messages, m)
		}
		m.Text += e.Text
	case "tool_start":
		for _, m := range s.Messages {
			if m.CallID == e.ToolID && m.RunID == s.runID() {
				return
			}
		}
		s.Messages = append(s.Messages, &message{Tool: e.ToolName, CallID: e.ToolID, RunID: s.runID(), Kind: "tool", Status: "执行中", Args: e.Args, Text: displayText(e.Args)})
	case "tool_update", "tool_end":
		for _, m := range s.Messages {
			if m.CallID == e.ToolID && m.RunID == s.runID() {
				result := e.Partial
				if e.Type == "tool_end" {
					result = e.Result
					m.Status = "完成"
					m.DurationMS = e.DurationMS
					if !e.Error {
						s.updatePlan(m.Tool, m.Args)
					}
					if e.Error {
						m.Status = "失败"
						m.Expanded = true
					}
					if str(e.Details["approval"]) == "declined" {
						m.Status = "已拒绝"
					}
				}
				m.Result = displayText(result)
				if e.Details != nil {
					m.Details = e.Details
				}
				return
			}
		}
	case "compacted":
		s.Messages = append(s.Messages, &message{Tool: "上下文压缩", Text: e.Summary, Kind: "compaction"})
	case "error", "compaction_failed":
		s.Messages = append(s.Messages, &message{Text: e.ErrorText, Kind: "error"})
	case "done":
		text := e.Final.Text
		if text == "" {
			text = displayText(e.Final.Content)
		}
		has := false
		for _, m := range s.Messages {
			if m.RunID == s.runID() && m.Kind == "" && !m.User {
				has = true
			}
		}
		if !has && text != "" {
			s.Messages = append(s.Messages, &message{Text: text, RunID: s.runID()})
		}
	}
}
func (s *session) runID() string {
	if s.Run == nil {
		return ""
	}
	return s.Run.ID
}
func (s *session) reduce(e envelope) {
	if e.Queue != nil && e.Queue.Revision >= s.Queue.Revision {
		s.Queue = *e.Queue
	}
	if e.Type == "snapshot" {
		if !e.Reset && e.Seq < s.Seq {
			return
		}
		s.history(e.Messages)
		s.Run = e.Run
		s.Seq = e.Seq
		s.Confirmations = e.Confirmations
		if e.Run != nil && e.Run.Prompt != "" {
			found := false
			for _, m := range s.Messages {
				if e.Run.UserEntry != "" && m.EntryID == e.Run.UserEntry {
					found = true
				}
			}
			if !found {
				text := e.Run.DisplayPrompt
				if text == "" {
					text = e.Run.Prompt
				}
				s.Messages = append(s.Messages, &message{User: true, Text: text, EntryID: e.Run.UserEntry, RunID: e.Run.ID})
			}
		}
		for _, raw := range e.Events {
			var event agentEvent
			if json.Unmarshal(raw, &event) == nil {
				s.event(event)
			}
		}
	} else if e.Type == "replay" {
		if s.Run == nil && e.Run != nil {
			copy := *e.Run
			copy.State = "running"
			s.Run = &copy
		}
		for _, raw := range e.Events {
			var next envelope
			if json.Unmarshal(raw, &next) == nil {
				s.reduce(next)
			}
		}
		s.Run = e.Run
		s.Seq = max(s.Seq, e.Seq)
		s.Confirmations = e.Confirmations
	} else if e.Type == "accepted" {
		if s.runID() != e.RunID {
			s.Run = &runInfo{ID: e.RunID, RequestID: e.RequestID, State: e.State, Prompt: e.Prompt}
			if e.Prompt != "" {
				s.Messages = append(s.Messages, &message{User: true, Text: e.Prompt, RunID: e.RunID})
			}
		}
	} else {
		if e.Seq > 0 && e.Seq <= s.Seq {
			return
		}
		if e.RunID != "" && s.runID() != "" && e.RunID != s.runID() {
			return
		}
		s.Seq = max(s.Seq, e.Seq)
		switch e.Type {
		case "event":
			if e.Event != nil {
				s.event(*e.Event)
			}
		case "confirmation":
			if e.Confirmation != nil {
				found := false
				for _, p := range s.Confirmations {
					if p.ID == e.Confirmation.ID {
						found = true
					}
				}
				if !found {
					s.Confirmations = append(s.Confirmations, *e.Confirmation)
				}
			}
		case "confirmed":
			var pending []confirmation
			for _, p := range s.Confirmations {
				if p.ID != e.ConfirmationID {
					pending = append(pending, p)
				}
			}
			s.Confirmations = pending
		case "status":
			if e.Run != nil {
				s.Run = e.Run
			}
			if s.Run != nil {
				s.Run.State = e.State
				if e.State == "" {
					s.Run.State = "completed"
					if e.Streaming {
						s.Run.State = "running"
					}
				}
			}
		case "error":
			s.Messages = append(s.Messages, &message{Kind: "error", Text: e.Message + e.Error})
		}
	}
	s.Running = s.Run.active()
	s.Pending = len(s.Confirmations) > 0
	if s.Run != nil && !s.Run.active() {
		s.Confirmations = nil
		s.Pending = false
		for i := len(s.Messages) - 1; i >= 0; i-- {
			m := s.Messages[i]
			if m.RunID == s.Run.ID && !m.User && m.Kind == "" && m.Tool == "" {
				if s.Run.State == "completed" {
					m.EntryID = s.Run.AssistantEntry
				}
				break
			}
		}
	}
}
func (s *session) statusText() string {
	if s.Sending {
		return "正在提交"
	}
	if s.Pending {
		return "等待审批"
	}
	if s.Running {
		return "生成中"
	}
	if s.Run != nil && s.Run.State == "failed" {
		return "任务失败"
	}
	if s.Run != nil && s.Run.State == "interrupted" {
		return "任务已中断"
	}
	return "就绪"
}
func pretty(v any) string      { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
func formatCount(v any) string { return fmt.Sprintf("%.0f", number(v)) }

func (s *session) updatePlan(name string, args any) {
	if name != "todo_write" && name != "update_plan" {
		return
	}
	entries := list(object(args)["todos"])
	if entries == nil {
		entries = list(object(args)["plan"])
	}
	s.Plan = nil
	for _, raw := range entries {
		entry := object(raw)
		content := str(entry["content"])
		if content == "" {
			content = str(entry["step"])
		}
		s.Plan = append(s.Plan, nativePlanEntry{content, str(entry["status"])})
	}
}
