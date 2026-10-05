package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/hwj123hwj/easyagent/sdk/session"
)

type EventHandler func(ctx context.Context, event AgentEvent)

type State int

const (
	StateIdle State = iota
	StateRunning
	StateWaiting
	StateError
)

type Options struct {
	Model              ai.Model
	Registry           *providers.Registry
	System             string
	Tools              []Tool
	MaxTurns           int
	Goal               string                   // 可选：当前会话目标（goal-driven loop，非空时取消 maxTurns 限制）
	Session            *session.Session         // 可选：会话持久化
	CompactionSettings compaction.Settings      // 上下文压缩设置
	SummarizeFunc      compaction.SummarizeFunc // 可选：摘要生成函数
	LifecycleHooks     LifecycleHooks           // 可选：工具执行生命周期钩子
	HookSystem         HookSystemInterface      // 可选：增强型 Hook 系统（优先级排序、超时）
	ConfirmFunc        ConfirmFunc              // 可选：危险工具执行前向用户确认（未注入则默认放行）
	LoopDetectSettings LoopDetectSettings       // 循环检测设置（默认启用，连续相同 tool call 触发提醒）
}

type Agent struct {
	mu                 sync.RWMutex
	state              State
	registry           *providers.Registry
	model              ai.Model
	system             string
	tools              map[string]Tool
	listeners          []*eventSubscription
	steeringQueue      *MessageQueue
	followUpQueue      *MessageQueue
	maxTurns           int
	goal               string // 当前会话目标（非空时启用 goal-driven loop，取消 maxTurns 限制）
	session            *session.Session
	compactionSettings compaction.Settings
	summarizeFunc      compaction.SummarizeFunc
	lifecycleHooks     LifecycleHooks
	hookSystem         HookSystemInterface // 增强型 Hook 系统（可选）
	confirmFunc        ConfirmFunc
	loopDetectSettings LoopDetectSettings
	loopDetect         loopDetector
}

func New(opts Options) *Agent {
	tools := make(map[string]Tool, len(opts.Tools))
	for _, tool := range opts.Tools {
		tools[tool.Name()] = tool
	}
	goalLog("[goal-debug] Agent.New called with goal=%q\n", opts.Goal)

	// 循环检测：补齐零值字段。Enabled/Threshold 由调用方传入；未设 ReminderTemplate 用默认。
	// 调用方（rebuildAgent、测试）应通过 DefaultLoopDetectSettings() 拿到"启用+阈值5"的默认。
	loopSettings := opts.LoopDetectSettings
	if loopSettings.Threshold <= 0 {
		loopSettings.Threshold = DefaultLoopDetectSettings().Threshold
	}
	if loopSettings.ReminderTemplate == "" {
		loopSettings.ReminderTemplate = DefaultLoopDetectSettings().ReminderTemplate
	}

	return &Agent{
		state:              StateIdle,
		registry:           opts.Registry,
		model:              opts.Model,
		system:             opts.System,
		tools:              tools,
		listeners:          make([]*eventSubscription, 0),
		steeringQueue:      NewMessageQueue(),
		followUpQueue:      NewMessageQueue(),
		maxTurns:           opts.MaxTurns,
		goal:               opts.Goal,
		session:            opts.Session,
		compactionSettings: opts.CompactionSettings,
		summarizeFunc:      opts.SummarizeFunc,
		lifecycleHooks:     opts.LifecycleHooks,
		hookSystem:         opts.HookSystem,
		confirmFunc:        opts.ConfirmFunc,
		loopDetectSettings: loopSettings,
	}
}

type eventSubscription struct{ handler EventHandler }

func (a *Agent) Subscribe(handler EventHandler) func() {
	subscription := &eventSubscription{handler: handler}
	a.mu.Lock()
	a.listeners = append(a.listeners, subscription)
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		for i, item := range a.listeners {
			if item == subscription {
				a.listeners = append(a.listeners[:i], a.listeners[i+1:]...)
				break
			}
		}
	}
}
func (a *Agent) emit(ctx context.Context, event AgentEvent) {
	a.mu.RLock()
	listeners := append([]*eventSubscription(nil), a.listeners...)
	a.mu.RUnlock()
	for _, listener := range listeners {
		listener.handler(ctx, event)
	}
}

func (a *Agent) State() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// ToolNames returns the names of all registered tools.
func (a *Agent) ToolNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, 0, len(a.tools))
	for name := range a.tools {
		names = append(names, name)
	}
	return names
}

// GetTool returns a registered tool by name.
// Returns the tool and true if found, nil and false otherwise.
func (a *Agent) GetTool(name string) (Tool, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	tool, ok := a.tools[name]
	return tool, ok
}

// Goal returns the current session goal, if any.
func (a *Agent) Goal() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.goal
}

// SetGoal sets the current session goal and activates goal-driven looping.
func (a *Agent) SetGoal(goal string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.goal = goal
}

// ClearGoal clears the current session goal.
func (a *Agent) ClearGoal() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.goal = ""
}

// Prompt 发起一次 Agent 对话，等待完成后返回最终 assistant message。
func (a *Agent) Prompt(ctx context.Context, msg ai.Message) (ai.AssistantMessage, error) {
	a.mu.Lock()
	if a.state == StateRunning {
		a.mu.Unlock()
		return ai.AssistantMessage{}, ErrAgentBusy
	}
	a.state = StateRunning
	a.mu.Unlock()

	// per-prompt 重置循环检测器，避免跨对话累积误判
	a.loopDetect.reset()

	a.emit(ctx, EventAgentStart{})
	if a.hookSystem != nil {
		a.hookSystem.RunSessionStart(ctx, a.lifecycleHooks.SessionStart, SessionStartEvent{Goal: a.goal})
	} else {
		runSessionStartHooks(ctx, a.lifecycleHooks.SessionStart, SessionStartEvent{Goal: a.goal})
	}
	a.steeringQueue.Enqueue(msg)
	assistant, err := RunLoop(ctx, a)
	a.mu.Lock()
	if err != nil {
		a.state = StateError
	} else {
		a.state = StateIdle
	}
	a.mu.Unlock()
	if a.hookSystem != nil {
		a.hookSystem.RunSessionEnd(ctx, a.lifecycleHooks.SessionEnd, SessionEndEvent{Err: err})
	} else {
		runSessionEndHooks(ctx, a.lifecycleHooks.SessionEnd, SessionEndEvent{Err: err})
	}
	a.emit(ctx, EventAgentEnd{Messages: []ai.Message{msg}})
	return assistant, err
}

// PromptStream 发起一次 Agent 对话，返回一个 event channel 供流式消费。
// 消费者可以从 channel 读取 AgentStreamEvent，channel 关闭表示对话完成。
// 最终结果通过最后一个 AgentStreamResult 事件传递。
func (a *Agent) PromptStream(ctx context.Context, msg ai.Message) (<-chan AgentStreamEvent, error) {
	ch := make(chan AgentStreamEvent, 64)
	var timingMu sync.Mutex
	toolStarts := map[string]int64{}

	// 订阅事件，转发到 channel（带背压控制）
	unsubscribe := a.Subscribe(func(ctx context.Context, event AgentEvent) {
		var ev AgentStreamEvent
		switch e := event.(type) {
		case EventContextUsage:
			ev = AgentStreamEvent{Type: StreamEventContextUsage, ContextUsage: &e.Usage}
		case EventTurnEnd:
			ev = AgentStreamEvent{Type: StreamEventTurnEnd, Message: e.Message}
		case EventToolExecutionStart:
			ev = AgentStreamEvent{Type: StreamEventToolStart, ToolName: e.ToolName, ToolCallID: e.ToolCallID, ToolArgs: e.Args}
		case EventToolExecutionUpdate:
			ev = AgentStreamEvent{Type: StreamEventToolUpdate, ToolName: e.ToolName, ToolCallID: e.ToolCallID, PartialResult: e.PartialResult}
		case EventToolExecutionEnd:
			ev = AgentStreamEvent{Type: StreamEventToolEnd, ToolName: e.ToolName, ToolCallID: e.ToolCallID, ToolResult: e.Result, ToolDetails: e.Details, IsError: e.IsError}
		case EventCompacted:
			ev = AgentStreamEvent{Type: StreamEventCompacted, Summary: e.Summary, TrimmedFrom: e.TrimmedFrom, TrimmedTo: e.TrimmedTo}
		case EventCompactionFailed:
			ev = AgentStreamEvent{Type: StreamEventError, Error: "compaction failed: " + e.Error}
		case EventConfirmationRequest:
			ev = AgentStreamEvent{Type: StreamEventConfirmationReq, ToolCallID: e.ToolCallID, ToolName: e.ToolName, Description: e.Description}
		case EventConfirmationResult:
			ev = AgentStreamEvent{Type: StreamEventConfirmationRes, ToolCallID: e.ToolCallID, Approved: e.Approved, Description: e.Reason}
		case EventLoopDetected:
			ev = AgentStreamEvent{Type: StreamEventLoopDetected, ToolName: e.ToolName, RepeatCount: e.RepeatCount}
		case EventMicroCompacted:
			ev = AgentStreamEvent{Type: StreamEventMicroCompacted, ClearedCount: e.ClearedResults}
		default:
			return
		}
		ev.Timestamp = time.Now().UnixMilli()
		timingMu.Lock()
		if ev.Type == StreamEventToolStart {
			toolStarts[ev.ToolCallID] = ev.Timestamp
		}
		if ev.Type == StreamEventToolEnd {
			if start, ok := toolStarts[ev.ToolCallID]; ok {
				ev.DurationMS = ev.Timestamp - start
				delete(toolStarts, ev.ToolCallID)
			}
		}
		timingMu.Unlock()
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	})

	a.mu.Lock()
	if a.state == StateRunning {
		a.mu.Unlock()
		unsubscribe()
		return nil, ErrAgentBusy
	}
	a.state = StateRunning
	a.mu.Unlock()

	// per-prompt 重置循环检测器，避免跨对话累积误判
	a.loopDetect.reset()

	go func() {
		defer close(ch)
		defer unsubscribe()

		provider, pErr := a.provider()
		if pErr != nil {
			ch <- AgentStreamEvent{Type: StreamEventError, Error: pErr.Error()}
			a.mu.Lock()
			a.state = StateError
			a.mu.Unlock()
			return
		}

		a.emit(ctx, EventAgentStart{})
		if a.hookSystem != nil {
			a.hookSystem.RunSessionStart(ctx, a.lifecycleHooks.SessionStart, SessionStartEvent{Goal: a.goal})
		} else {
			runSessionStartHooks(ctx, a.lifecycleHooks.SessionStart, SessionStartEvent{Goal: a.goal})
		}
		a.steeringQueue.Enqueue(msg)

		// PromptStream 的 stream consumer：转发 text delta 事件到 channel
		consume := func(stream *ai.EventStream) (ai.StreamAssistantMessage, error) {
			var streamMsg ai.StreamAssistantMessage
			var thinkingStart time.Time
			var thinkingDuration time.Duration
			finishThinking := func() {
				if thinkingStart.IsZero() {
					return
				}
				duration := time.Since(thinkingStart)
				thinkingDuration += duration
				select {
				case ch <- AgentStreamEvent{Type: StreamEventThinkingEnd, Timestamp: time.Now().UnixMilli(), DurationMS: duration.Milliseconds()}:
				case <-ctx.Done():
				}
				thinkingStart = time.Time{}
			}
			defer finishThinking()
			for event := range stream.Events() {
				select {
				case <-ctx.Done():
					return streamMsg, ctx.Err()
				default:
				}
				switch e := event.(type) {
				case ai.EventThinkingDelta:
					if thinkingStart.IsZero() {
						thinkingStart = time.Now()
					}
					select {
					case ch <- AgentStreamEvent{Type: StreamEventThinkingDelta, TextDelta: e.Delta, Timestamp: time.Now().UnixMilli()}:
					case <-ctx.Done():
						return streamMsg, ctx.Err()
					}
				case ai.EventToolCallStart:
					finishThinking()
				case ai.EventTextDelta:
					finishThinking()
					select {
					case ch <- AgentStreamEvent{Type: StreamEventTextDelta, TextDelta: e.Delta}:
					case <-ctx.Done():
						return streamMsg, ctx.Err()
					}
				case ai.EventDone:
					finishThinking()
					streamMsg = e.Message
					streamMsg.ThinkingDurationMS = thinkingDuration.Milliseconds()
				case ai.EventError:
					select {
					case ch <- AgentStreamEvent{Type: StreamEventError, Error: e.Error}:
					case <-ctx.Done():
						return streamMsg, ctx.Err()
					}
				}
			}
			return streamMsg, nil
		}

		lastAssistant, err := runAgentLoop(ctx, a, provider, consume)
		if err != nil {
			ch <- AgentStreamEvent{Type: StreamEventError, Error: err.Error()}
		}
		ch <- AgentStreamEvent{Type: StreamEventDone, FinalMessage: lastAssistant, Usage: lastAssistant.Usage}

		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		if a.hookSystem != nil {
			a.hookSystem.RunSessionEnd(ctx, a.lifecycleHooks.SessionEnd, SessionEndEvent{Err: err})
		} else {
			runSessionEndHooks(ctx, a.lifecycleHooks.SessionEnd, SessionEndEvent{Err: err})
		}
		a.emit(ctx, EventAgentEnd{})
	}()

	return ch, nil
}

// CompactNow manually triggers context compaction.
// It reads the full history from session storage, splits it, generates an LLM
// summary, and persists a compaction entry. Returns summary, trimmedFrom,
// trimmedTo, error.
func (a *Agent) CompactNow(ctx context.Context, customInstructions string) (string, int, int, error) {
	if a.session == nil {
		return "", 0, 0, fmt.Errorf("no session for compaction")
	}
	if a.summarizeFunc == nil {
		return "", 0, 0, fmt.Errorf("compaction not available (no summarizer configured)")
	}

	// 1. Load full history from session storage.
	history, err := a.session.BuildContext(ctx)
	if err != nil {
		return "", 0, 0, fmt.Errorf("build context: %w", err)
	}

	if len(history) < 2 {
		return "", 0, 0, fmt.Errorf("not enough messages to compact (have %d)", len(history))
	}

	// 2. Split into history (to summarize) and recent (to keep).
	historyPart, recentPart := compaction.SplitMessages(history, a.compactionSettings.KeepRecentTokens)
	if len(historyPart) == 0 {
		return "", 0, 0, fmt.Errorf("nothing to compact (recent context too large)")
	}

	// 3. Generate summary via LLM.
	summary, err := compaction.Compact(ctx, historyPart, recentPart, customInstructions, a.summarizeFunc)
	if err != nil {
		return "", 0, 0, fmt.Errorf("compact: %w", err)
	}

	// 4. Persist compaction entry to session storage.
	if err := a.session.AppendCompaction(ctx, summary); err != nil {
		return "", 0, 0, fmt.Errorf("persist compaction: %w", err)
	}

	trimmedFrom := len(history)
	trimmedTo := len(recentPart) + 1 // +1 for the summary message

	a.emit(ctx, EventCompacted{
		Summary:     summary,
		TrimmedFrom: trimmedFrom,
		TrimmedTo:   trimmedTo,
	})

	return summary, trimmedFrom, trimmedTo, nil
}

func (a *Agent) Steer(ctx context.Context, msg ai.Message) {
	a.steeringQueue.Enqueue(msg)
}

func (a *Agent) FollowUp(ctx context.Context, msg ai.Message) {
	a.followUpQueue.Enqueue(msg)
}

func (a *Agent) provider() (providers.Provider, error) {
	provider, ok := a.registry.Get(a.model.Provider)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", a.model.Provider)
	}
	return provider, nil
}

func (a *Agent) toolDefinitions() []ai.ToolDefinition {
	defs := make([]ai.ToolDefinition, 0, len(a.tools))
	for _, tool := range a.tools {
		defs = append(defs, ai.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return defs
}

func (a *Agent) appendToolResult(call ai.ToolCall, result ToolResult) ai.ToolResultMessage {
	content := result.Content
	if content == "" {
		content = "ok"
	}
	return ai.ToolResultMessage{ToolCallID: call.ID, Content: content, IsError: result.IsError, Details: result.Details}
}

func (a *Agent) llmRequest(messages []ai.Message) ai.StreamRequest {
	return ai.StreamRequest{
		Model:      a.model,
		Messages:   messages,
		System:     a.system,
		Tools:      a.toolDefinitions(),
		MaxTokens:  nil,
		ToolChoice: nil,
	}
}

func (a *Agent) handleAssistantMessage(message ai.StreamAssistantMessage) (ai.AssistantMessage, error) {
	assistant := ai.AssistantMessage{
		Text:               message.Text,
		Thinking:           message.Thinking,
		ThinkingDurationMS: message.ThinkingDurationMS,
		ToolCalls:          message.ToolCalls,
		StopReason:         message.StopReason,
		ErrorMsg:           message.ErrorMsg,
		Usage:              message.Usage,
	}
	return assistant, nil
}

func (a *Agent) decodeToolArgs(raw string, tool Tool) (json.RawMessage, error) {
	if raw == "" {
		raw = "{}"
	}
	validated, err := tool.Validate(json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	return validated, nil
}

// ─── Stream Event 类型 ───────────────────────────────────────────────────────

type StreamEventType string

const (
	StreamEventTextDelta       StreamEventType = "text_delta"
	StreamEventThinkingDelta   StreamEventType = "thinking_delta"
	StreamEventThinkingEnd     StreamEventType = "thinking_end"
	StreamEventContextUsage    StreamEventType = "context_usage"
	StreamEventTurnEnd         StreamEventType = "turn_end"
	StreamEventToolStart       StreamEventType = "tool_start"
	StreamEventToolUpdate      StreamEventType = "tool_update"
	StreamEventToolEnd         StreamEventType = "tool_end"
	StreamEventDone            StreamEventType = "done"
	StreamEventError           StreamEventType = "error"
	StreamEventCompacted       StreamEventType = "compacted"
	StreamEventConfirmationReq StreamEventType = "confirmation_request"
	StreamEventConfirmationRes StreamEventType = "confirmation_result"
	StreamEventLoopDetected    StreamEventType = "loop_detected"
	StreamEventMicroCompacted  StreamEventType = "micro_compacted"
)

type AgentStreamEvent struct {
	ContextUsage  *ContextUsage       `json:"context_usage,omitempty"`
	Timestamp     int64               `json:"timestamp,omitempty"`
	DurationMS    int64               `json:"duration_ms,omitempty"`
	Type          StreamEventType     `json:"type"`
	TextDelta     string              `json:"text_delta,omitempty"`
	Message       ai.Message          `json:"message,omitempty"`
	ToolName      string              `json:"tool_name,omitempty"`
	ToolCallID    string              `json:"tool_call_id,omitempty"`
	ToolArgs      any                 `json:"tool_args,omitempty"` // raw arguments for diff preview
	ToolResult    any                 `json:"tool_result,omitempty"`
	ToolDetails   any                 `json:"tool_details,omitempty"` // 结构化附加数据（如 PlayDetails）
	PartialResult any                 `json:"partial_result,omitempty"`
	IsError       bool                `json:"is_error,omitempty"`
	FinalMessage  ai.AssistantMessage `json:"final_message,omitempty"`
	Error         string              `json:"error,omitempty"`
	Summary       string              `json:"summary,omitempty"`
	TrimmedFrom   int                 `json:"trimmed_from,omitempty"`
	TrimmedTo     int                 `json:"trimmed_to,omitempty"`
	Description   string              `json:"description,omitempty"`   // 确认请求：工具给出的操作描述
	Approved      bool                `json:"approved,omitempty"`      // 确认结果：是否放行
	RepeatCount   int                 `json:"repeat_count,omitempty"`  // 循环检测：连续重复次数
	ClearedCount  int                 `json:"cleared_count,omitempty"` // MicroCompact：清理的 tool result 数
	Usage         ai.Usage            `json:"usage,omitempty"`         // token usage from final assistant message
}
