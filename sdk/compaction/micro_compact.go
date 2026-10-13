package compaction

import (
	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// clearedPlaceholder 替换旧 tool result 内容的占位符。
// 保留痕迹让 Agent 知道此处曾有工具输出（可重读/重查），而非凭空消失。
const clearedPlaceholder = "[older tool result cleared to save context]"

// compactableTools 可被 MicroCompact 清理的只读/输出型工具。
// 这些工具的 result 体积大、价值随时间递减（旧命令输出、旧文件内容早用完）。
// 写操作（edit/write）的 result 通常是简短确认，价值高体积小，不清理。
var compactableTools = map[string]bool{
	"computer":  true,
	"read":      true,
	"bash":      true,
	"grep":      true,
	"find":      true,
	"ls":        true,
	"web_fetch": true,
}

// MicroCompact 清理历史中旧的只读工具结果，保留最近 keepRecent 个完整。
// 不调 LLM——纯本地文本替换，零成本零延迟零幻觉。
// 返回（新历史, 实际清理掉的 tool result 数）。
//
// 算法（参考 cc-haha microCompact.ts）：
//  1. 遍历历史，建立 ToolCallID → 工具名 映射（ToolResultMessage 只有 ID，
//     不带工具名，需从 AssistantMessage.ToolCalls 回溯关联）
//  2. 收集所有"可压缩工具"的 tool result 的（索引, ID）
//  3. 除最近 keepRecent 个外，把其余的 Content 替换为占位符
//
// 原地替换消息内容，不改变历史长度/结构，保持每个 turn 的完整性
// （assistant tool_call + tool result 配对不变）。
func MicroCompact(history []ai.Message, keepRecent int) ([]ai.Message, int) {
	if keepRecent < 0 {
		keepRecent = 0
	}

	// 1. 建 ToolCallID → 工具名 映射
	toolNameByID := make(map[string]string)
	for _, msg := range history {
		if am, ok := msg.(ai.AssistantMessage); ok {
			for _, tc := range am.ToolCalls {
				toolNameByID[tc.ID] = tc.Name
			}
		}
	}

	// 2. 收集可压缩 tool result 的（索引, ID），按出现顺序
	type resultRef struct {
		idx int
		id  string
	}
	var compactable []resultRef
	for i, msg := range history {
		if tr, ok := msg.(ai.ToolResultMessage); ok {
			if name, found := toolNameByID[tr.ToolCallID]; found && compactableTools[name] {
				compactable = append(compactable, resultRef{idx: i, id: tr.ToolCallID})
			}
		}
	}

	// 不足或刚好 keepRecent 个 → 不清理
	if len(compactable) <= keepRecent {
		return history, 0
	}

	// 3. 保留最近 keepRecent 个（切片末尾），清理其余
	keepFrom := len(compactable) - keepRecent
	cleared := 0
	// 保留集合：最近 keepRecent 个的索引
	keepIdx := make(map[int]bool, keepRecent)
	for _, ref := range compactable[keepFrom:] {
		keepIdx[ref.idx] = true
	}
	// 清理 keepFrom 之前的
	for _, ref := range compactable[:keepFrom] {
		if tr, ok := history[ref.idx].(ai.ToolResultMessage); ok {
			// 占位符可能比原内容长（原内容极短时），但通常短得多；只在确实能省时清理
			if len(clearedPlaceholder) < len(tr.Content) || len(tr.Images) > 0 {
				tr.Content = clearedPlaceholder
				tr.Images = nil
				history[ref.idx] = tr // 值类型，需重新赋值回 slice
				cleared++
			}
		}
	}

	return history, cleared
}
