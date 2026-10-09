package agent

import (
	"context"
	"encoding/json"
	"log/slog"
)

// ToolCallContext carries the full context for a single tool invocation through
// the lifecycle stages (prepare → before → execute → after → finish).
type ToolCallContext struct {
	ToolCallID string
	ToolName   string
	RawArgs    json.RawMessage // original args from LLM
	Args       json.RawMessage // after Validate (and optionally PrepareArguments)
}

// ToolExecutionResult holds the outcome of a tool execution.
type ToolExecutionResult struct {
	Result ToolResult
	Err    error
}

// ─── Hook function types ─────────────────────────────────────────────────────

// BeforeToolCallHook is called before a tool executes.
//
//   - Return the (possibly modified) ToolCallContext and nil to continue.
//   - Return a non-nil error to block execution (the tool will not run).
//     The error message becomes the tool result content.
type BeforeToolCallHook func(ctx context.Context, call ToolCallContext) (ToolCallContext, error)

// AfterToolCallHook is called after a tool finishes execution.
//
//   - Return the (possibly modified) ToolResult and nil to continue.
//   - Return a non-nil error to treat the overall execution as failed.
//     The pre-hook ToolResult is preserved in AfterHookError for debugging.
type AfterToolCallHook func(ctx context.Context, call ToolCallContext, result ToolResult) (ToolResult, error)

// ─── Confirmation (pre-execution approval) ──────────────────────────────────

// ConfirmationRequest 描述一次需要用户确认的工具调用。
// 当工具实现了 ToolWithConfirmation 且 RequiresConfirmation 返回 ok=true 时构造。
type ConfirmationRequest struct {
	// RequiresApproval marks integrations whose trust is independent of the
	// ordinary dangerous-tool confirmation toggle.
	RequiresApproval bool
	// ForceConfirmation prevents global auto-approve from bypassing this request.
	ForceConfirmation bool
	ToolCallID        string
	ToolName          string
	Args              json.RawMessage // validated + prepared args
	Description       string          // 工具给出的操作描述，展示给用户
}

// ConfirmDecision 是用户对一次确认请求的裁决。
type ConfirmDecision struct {
	Response json.RawMessage // Structured user input, if the tool needs more than approval.
	Approved bool            // true=放行执行；false=阻断
	Reason   string          // 拒绝理由（approved=false 时回告 LLM，让 Agent 知道动作未做成）
}

// ConfirmFunc 由各入口（chat/serve/feishu）注入，负责向用户发起确认并同步等待裁决。
//
//   - 注入了 ConfirmFunc：执行链在确认点暂停，调用此函数等待用户回复。
//   - 未注入（nil）：执行链默认放行（适用于 serve/feishu 等单向流入口）。
//
// 返回的 ConfirmDecision.Approved=false 时，工具不执行，"用户拒绝"作为
// tool result 回告 LLM（IsError=false，避免 Agent 误判为系统错误而重试）。
type ConfirmFunc func(ctx context.Context, req ConfirmationRequest) ConfirmDecision

// AfterHookError wraps an error from an after-tool-call hook while preserving
// the original (pre-hook) ToolResult for debugging.
type AfterHookError struct {
	Err    error
	Result ToolResult
}

func (e *AfterHookError) Error() string { return e.Err.Error() }
func (e *AfterHookError) Unwrap() error { return e.Err }

// NewAfterHookError creates an AfterHookError that preserves the pre-hook result.
func NewAfterHookError(err error, result ToolResult) *AfterHookError {
	return &AfterHookError{Err: err, Result: result}
}

// ─── Observer hooks (session-level, non-blocking) ───────────────────────────
//
// 以下三个 hook 是观察型：签名 func(ctx, Event) error，返回的 error 仅被
// slog.Warn 记录，不阻断主流程。与 DeepV 的 PreCompress 语义一致（只能观察）。
// 用于会话启动/结束、上下文压缩前注入副作用（预加载、清理、记录）。

// SessionStartEvent 携带会话启动时的上下文。
type SessionStartEvent struct {
	Goal string // 当前 goal（若有）
}

// SessionStartHook 在会话开始时触发（对应 EventAgentStart）。观察型。
type SessionStartHook func(ctx context.Context, e SessionStartEvent) error

// SessionEndEvent 携带会话结束时的上下文。
type SessionEndEvent struct {
	Err error // 会话是否因错误结束（nil=正常完成）
}

// SessionEndHook 在会话结束时触发（对应 EventAgentEnd）。观察型。
type SessionEndHook func(ctx context.Context, e SessionEndEvent) error

// PreCompressEvent 携带上下文压缩前的上下文。
type PreCompressEvent struct {
	ContextTokens int // 估算的当前 token 数
	ContextWindow int // 模型上下文窗口
	MessageCount  int // 待压缩的消息数
}

// PreCompressHook 在上下文压缩前触发。观察型——不能阻止或修改压缩。
type PreCompressHook func(ctx context.Context, e PreCompressEvent) error

// LifecycleHooks aggregates the hook slices consumed by the agent.
//
// Before/After 是工具执行级 hook（可阻断）。
// SessionStart/SessionEnd/PreCompress 是会话级 hook（观察型，error 仅记录不阻断）。
type LifecycleHooks struct {
	Before       []BeforeToolCallHook
	After        []AfterToolCallHook
	SessionStart []SessionStartHook
	SessionEnd   []SessionEndHook
	PreCompress  []PreCompressHook
}

// HookSystemInterface defines the interface for the enhanced hook system.
// This allows the agent to use the hook system without directly importing
// the hooks package, avoiding circular dependencies.
type HookSystemInterface interface {
	// RunBefore runs all before-tool-call hooks (registered + existing lifecycle hooks)
	// in priority order. Returns the (possibly modified) ToolCallContext or an error to block.
	RunBefore(ctx context.Context, existing []BeforeToolCallHook, call ToolCallContext) (ToolCallContext, error)
	// RunAfter runs all after-tool-call hooks.
	RunAfter(ctx context.Context, existing []AfterToolCallHook, call ToolCallContext, result ToolResult) (ToolResult, error)
	// RunSessionStart runs all session-start hooks (non-blocking).
	RunSessionStart(ctx context.Context, existing []SessionStartHook, e SessionStartEvent)
	// RunSessionEnd runs all session-end hooks (non-blocking).
	RunSessionEnd(ctx context.Context, existing []SessionEndHook, e SessionEndEvent)
	// RunPreCompress runs all pre-compress hooks (non-blocking).
	RunPreCompress(ctx context.Context, existing []PreCompressHook, e PreCompressEvent)
}

// 观察型 hook 的统一执行模式：遍历调用，error 仅 slog.Warn 记录，不阻断主流程。
// 三个函数分别对应三种 Event 类型（Go 具名 hook 类型不隐式转换，故不合并）。

func runSessionStartHooks(ctx context.Context, hooks []SessionStartHook, e SessionStartEvent) {
	for _, h := range hooks {
		if err := h(ctx, e); err != nil {
			slog.Warn("SessionStart hook failed (non-blocking)", "error", err)
		}
	}
}

func runSessionEndHooks(ctx context.Context, hooks []SessionEndHook, e SessionEndEvent) {
	for _, h := range hooks {
		if err := h(ctx, e); err != nil {
			slog.Warn("SessionEnd hook failed (non-blocking)", "error", err)
		}
	}
}

func runPreCompressHooks(ctx context.Context, hooks []PreCompressHook, e PreCompressEvent) {
	for _, h := range hooks {
		if err := h(ctx, e); err != nil {
			slog.Warn("PreCompress hook failed (non-blocking)", "error", err)
		}
	}
}

// ─── Optional tool interfaces ────────────────────────────────────────────────

// ToolWithPrepareArguments is an optional interface that tools can implement to
// normalize or enrich their validated arguments before execution.
//
// The execution order is:
//
//  1. raw args → tool.Validate(...)
//  2. validated args → tool.PrepareArguments(...)  (if implemented)
//  3. prepared args → before hooks → execute → after hooks
type ToolWithPrepareArguments interface {
	Tool
	PrepareArguments(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}

// ToolWithConfirmedExecution consumes structured input collected at the confirmation gate.
// The decision belongs to this invocation, never shared mutable tool state.
type ToolWithConfirmedExecution interface {
	Tool
	ExecuteConfirmed(context.Context, json.RawMessage, ConfirmDecision, func(PartialResult)) (ToolResult, error)
}
