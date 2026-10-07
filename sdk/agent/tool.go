package agent

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Validate(params json.RawMessage) (json.RawMessage, error)
	Execute(ctx context.Context, params json.RawMessage, onUpdate func(PartialResult)) (ToolResult, error)
}

// ToolResult 描述一次工具执行的结果。
//
//   - Content: 回告给 LLM 的内容（经过截断/格式化，适合塞进上下文）。
//   - UserFacing: 给用户展示的内容（可更详细或带格式）；为空时 fallback 到 Content。
//     分离两者让"给人看的"和"给模型看的"可以采用不同策略，例如 bash 输出
//     给用户看完整、给模型看截断版。
//   - Details: 预留的结构化附加信息（当前未使用）。
type ToolResult struct {
	Content    string
	UserFacing string
	Details    any
	IsError    bool
}

// DisplayText 返回适合展示给用户的内容：优先 UserFacing，为空则 fallback 到 Content。
func (r ToolResult) DisplayText() string {
	if r.UserFacing != "" {
		return r.UserFacing
	}
	return r.Content
}

type PartialResult struct {
	Content string
	Done    bool
}

// ExecutionMode 控制并行/顺序执行。
type ExecutionMode int

const (
	ExecutionModeParallel ExecutionMode = iota
	ExecutionModeSequential
)

// ToolWithMode 可选接口：工具可覆盖默认执行模式。
type ToolWithMode interface {
	Tool
	ExecutionMode() ExecutionMode
}

// ToolWithPromptInfo 可选接口：工具提供系统提示中的 snippet 和 guidelines。
// 工具可以实现此接口，让系统提示构建器自动收集这些信息。
type ToolWithPromptInfo interface {
	Tool
	PromptSnippet() string      // 一句话描述工具用途，用于 Available tools 摘要
	PromptGuidelines() []string // 使用指南条目，追加到 Guidelines 区域
}

// ConcurrencySafeChecker 是一个可选接口（与 ToolWithMode 类似）。
// 工具可实现此接口以声明其是否可安全并发执行。
// 未实现此接口的工具默认视为不安全（保守策略）。
//
// 分区调度器 (partitionToolCalls) 在分批次时查询此接口。
// 若工具同时实现了 ToolWithMode 且 ExecutionMode() == Sequential，
// 即使 IsConcurrencySafe 返回 true，也会被保守地视为不安全。
//
// Extension 工具也可以实现此接口（Go duck typing 的自然结果，无需额外注册代码）。
type ConcurrencySafeChecker interface {
	Tool
	IsConcurrencySafe(params json.RawMessage) bool
}

// ToolWithConfirmation 是一个可选接口（与 ConcurrencySafeChecker 类似）。
// 工具可实现此接口，声明某次调用在执行前需要用户确认（如危险 shell 命令、文件覆盖）。
// 未实现此接口的工具视为不需要确认，直接执行。
//
// 执行链 (executeOneTool) 在 before hooks 之后、Execute 之前查询此接口：
// 若 RequiresConfirmation 返回 ok=true，则调用 Agent 的 ConfirmFunc（若已注入）
// 等待用户裁决；拒绝时阻断执行并把"用户拒绝"作为 tool result 回告 LLM。
// 当未注入 ConfirmFunc（如 serve/feishu 单向流入口），默认放行——确认行为由各入口自行决定。
//
// description 用于在确认对话框中向用户展示即将执行的操作及其影响。
type ToolWithConfirmation interface {
	Tool
	RequiresConfirmation(params json.RawMessage) (description string, ok bool)
}

// ToolRequiringConfirmation opts a tool into refusing execution when its
// confirmation cannot be presented. External tools use this to keep an
// untrusted server from silently inheriting a headless entrypoint's access.
type ToolRequiringConfirmation interface {
	Tool
	RequiresConfirmationAvailable() bool
}

// ToolWithMandatoryConfirmation keeps a tool's interactive approval independent
// of the entrypoint's global full-access / auto-approve setting.
type ToolWithMandatoryConfirmation interface {
	Tool
	RequiresInteractiveConfirmation() bool
}
