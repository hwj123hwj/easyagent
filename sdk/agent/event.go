package agent

import (
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/session"
)

type AgentEvent interface {
	agentEventMarker()
}

type EventAgentStart struct{}

func (EventAgentStart) agentEventMarker() {}

type EventAgentEnd struct {
	Messages []ai.Message
}

func (EventAgentEnd) agentEventMarker() {}

type EventTurnStart struct{}

func (EventTurnStart) agentEventMarker() {}

type EventTurnEnd struct {
	Message     ai.Message
	ToolResults []ai.Message
}

func (EventTurnEnd) agentEventMarker() {}

type EventToolExecutionStart struct {
	ToolCallID string
	ToolName   string
	Args       any
}

func (EventToolExecutionStart) agentEventMarker() {}

type EventToolExecutionUpdate struct {
	ToolCallID    string
	ToolName      string
	Args          any
	PartialResult any
}

func (EventToolExecutionUpdate) agentEventMarker() {}

type EventToolExecutionEnd struct {
	ToolCallID string
	ToolName   string
	Result     any
	Details    any // structured附加数据（如 PlayDetails），可选
	IsError    bool
}

func (EventToolExecutionEnd) agentEventMarker() {}

// EventCompacted 上下文压缩完成事件。
type EventCompacted struct {
	Info        *session.CompactionInfo
	Summary     string
	TrimmedFrom int
	TrimmedTo   int
}

func (EventCompacted) agentEventMarker() {}

// EventCompactionFailed 上下文压缩失败事件。
type EventCompactionFailed struct {
	Error string
}

func (EventCompactionFailed) agentEventMarker() {}

// EventToolBatchStart 在每个工具批次开始执行时发出。
// 用于 UI 展示和调试，帮助区分并行批次和串行批次。
type EventToolBatchStart struct {
	BatchIndex int      // 批次序号（从 0 开始）
	Safe       bool     // true = 并行批次, false = 串行批次
	ToolNames  []string // 批次内工具名称列表
}

func (EventToolBatchStart) agentEventMarker() {}

// EventGoalCompleted is emitted when the agent signals that the current goal
// has been fully achieved. UI layers can use this to display completion status.
type EventGoalCompleted struct {
	Goal string // the goal text that was completed
}

func (EventGoalCompleted) agentEventMarker() {}

// EventConfirmationRequest 在危险工具执行前、需要用户确认时发出。
// UI（chat TUI）据此弹出确认对话框；单向流入口（serve/feishu）可忽略。
type EventConfirmationRequest struct {
	ToolCallID  string
	ToolName    string
	Description string // 工具给出的操作描述
}

func (EventConfirmationRequest) agentEventMarker() {}

// EventConfirmationResult 携带用户对确认请求的裁决。
type EventConfirmationResult struct {
	ToolCallID string
	Approved   bool
	Reason     string // 拒绝理由（Approved=false 时）
}

func (EventConfirmationResult) agentEventMarker() {}

// EventLoopDetected 在检测到 Agent 连续重复调用同一工具（相同参数）时发出。
// UI 可据此展示"⚠ 检测到循环"提示；Agent 会同时收到一条提醒 follow-up。
type EventLoopDetected struct {
	ToolName    string
	RepeatCount int
}

func (EventLoopDetected) agentEventMarker() {}

// EventMicroCompacted 在 MicroCompact（清旧 tool result）执行后发出。
// UI 可据此展示"已清理 N 个旧工具结果，节省上下文"。
type EventMicroCompacted struct {
	TokensAfter    int
	ClearedResults int
	TokensBefore   int
}

func (EventMicroCompacted) agentEventMarker() {}

// EventContextUsage describes this request, never cumulative billed tokens.
type EventContextUsage struct{ Usage ContextUsage }

func (EventContextUsage) agentEventMarker() {}
