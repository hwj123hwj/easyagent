package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("Hello, this is a test message that has some length to it."),
		ai.AssistantMessage{Text: "And here is a response that also has some content in it."},
	}
	tokens := EstimateTokens(msgs)
	assert.Greater(t, tokens, 0)
	// 大约 (72 + 60) / 4 ≈ 33 tokens
	assert.Less(t, tokens, 100)
}

func TestShouldCompact(t *testing.T) {
	settings := DefaultSettings()

	// 未启用
	settings.Enabled = false
	assert.False(t, ShouldCompact(100000, 128000, settings))

	// 启用但未超限
	settings.Enabled = true
	assert.False(t, ShouldCompact(100000, 128000, settings))

	// 超过限制
	assert.True(t, ShouldCompact(120000, 128000, settings))
}

func TestSplitMessages(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("msg 1"),
		ai.AssistantMessage{Text: "response 1"},
		ai.NewTextUserMessage("msg 2"),
		ai.AssistantMessage{Text: "response 2"},
		ai.NewTextUserMessage("msg 3"),
		ai.AssistantMessage{Text: "response 3"},
	}

	history, recent := SplitMessages(msgs, 5) // 极小的 budget
	// 应该至少保留一些 recent 消息
	assert.NotNil(t, recent)
	// history 应该比 recent 少或相等
	assert.LessOrEqual(t, len(history), len(msgs))
}

func TestSplitMessages_Empty(t *testing.T) {
	history, recent := SplitMessages(nil, 1000)
	assert.Nil(t, history)
	assert.Nil(t, recent)
}

func TestSplitMessages_AllRecent(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("short"),
	}
	history, recent := SplitMessages(msgs, 10000)
	assert.Nil(t, history)
	assert.Equal(t, msgs, recent)
}

func TestSummarizePrompt(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("Build a web server"),
		ai.AssistantMessage{Text: "I'll create a web server using Go."},
	}
	prompt := SummarizePrompt(msgs, "")
	assert.Contains(t, prompt, "Build a web server")
	assert.Contains(t, prompt, "I'll create a web server")
	assert.Contains(t, prompt, "Goal")
}

func TestSummarizePrompt_WithCustomInstructions(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("Refactor the auth module"),
		ai.AssistantMessage{Text: "I'll start by reviewing the existing code."},
	}
	prompt := SummarizePrompt(msgs, "Focus on security implications")
	assert.Contains(t, prompt, "Additional instructions from user")
	assert.Contains(t, prompt, "Focus on security implications")
	assert.Contains(t, prompt, "Refactor the auth module")
}

func TestSummarizePrompt_NoCustomInstructions(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("Fix the bug"),
	}
	prompt := SummarizePrompt(msgs, "")
	assert.NotContains(t, prompt, "Additional instructions from user")
}

// 工具调用是 Agent 的典型运行序列：user → assistant(tool_use) → tool → ...
// → assistant(stop)。旧版 SplitMessages 只认 user→assistant 边界，导致工具序列
// 全部进 recent、historyPart 为空、压缩根本不发生。本测试锁定修复后的行为。
func TestSplitMessages_ToolCallSequence_Compactable(t *testing.T) {
	hist := []ai.Message{ai.NewTextUserMessage("start")}
	for i := 0; i < 10; i++ {
		hist = append(hist,
			ai.AssistantMessage{
				Text:       "thinking " + strings.Repeat("x", 50),
				ToolCalls:  []ai.ToolCall{{ID: "c", Name: "bash"}},
				StopReason: ai.StopReasonToolUse,
			},
			ai.ToolResultMessage{ToolCallID: "c", Content: "output " + strings.Repeat("y", 50)},
		)
	}
	hist = append(hist, ai.AssistantMessage{Text: "done", StopReason: ai.StopReasonStop})

	history, recent := SplitMessages(hist, 200)

	// 工具序列必须能切出可压缩的 history（修复前 hist=0）
	assert.NotEmpty(t, history, "工具调用序列应能切出 history")
	// history 末尾不能是带 tool_call 的 assistant——会割裂 tool_call/tool_result 配对
	last := history[len(history)-1]
	if a, ok := last.(ai.AssistantMessage); ok {
		assert.Empty(t, a.ToolCalls, "切割点不应割裂 tool_call 配对")
	}
	// recent 非空
	assert.NotEmpty(t, recent)
}

// 切割点不得把 assistant(tool_use) 和它紧随的 tool_result 拆开。
func TestSplitMessages_NoBrokenToolCallPair(t *testing.T) {
	msgs := []ai.Message{
		ai.NewTextUserMessage("q1"),
		ai.AssistantMessage{Text: strings.Repeat("a", 100), StopReason: ai.StopReasonStop},
		ai.NewTextUserMessage("q2"),
		ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read"}}, StopReason: ai.StopReasonToolUse},
		ai.ToolResultMessage{ToolCallID: "c1", Content: strings.Repeat("r", 100)},
		ai.AssistantMessage{Text: "final", StopReason: ai.StopReasonStop},
	}

	history, _ := SplitMessages(msgs, 80)
	// 找 history 里有没有孤立的 assistant(tool_use)（其 tool_result 被切到 recent）
	for i, m := range history {
		if a, ok := m.(ai.AssistantMessage); ok && len(a.ToolCalls) > 0 {
			// 这条 assistant 的 tool_result 必须也在 history 里
			if i+1 >= len(history) {
				t.Errorf("history[%d] 是带 tool_call 的 assistant，但其 tool_result 被切到 recent", i)
			}
		}
	}
}

func TestShouldMicroCompact(t *testing.T) {
	settings := DefaultSettings() // MicroCompactRatio=0.6

	// 60% 阈值：78000/128000 ≈ 60.9% → 触发
	assert.True(t, ShouldMicroCompact(78000, 128000, settings))
	// 59%：不触发
	assert.False(t, ShouldMicroCompact(75000, 128000, settings))
	// 远低于阈值：不触发
	assert.False(t, ShouldMicroCompact(10000, 128000, settings))

	// Disabled 不触发
	disabled := settings
	disabled.Enabled = false
	assert.False(t, ShouldMicroCompact(120000, 128000, disabled))

	// 阈值可配：ratio=0.3 时 40000/128000≈31% 触发
	lowRatio := settings
	lowRatio.MicroCompactRatio = 0.3
	assert.True(t, ShouldMicroCompact(40000, 128000, lowRatio))

	// contextWindow<=0 不触发（防除零/无意义）
	assert.False(t, ShouldMicroCompact(100, 0, settings))
}

func TestDefaultSettings_MicroFields(t *testing.T) {
	s := DefaultSettings()
	assert.InDelta(t, 0.6, s.MicroCompactRatio, 0.001)
	assert.Equal(t, 5, s.MicroKeepRecent)
}

func TestSplitMessagesKeepsEntireParallelToolBatch(t *testing.T) {
	messages := []ai.Message{
		ai.NewTextUserMessage("older task"), ai.AssistantMessage{Text: "older answer"},
		ai.NewTextUserMessage("new task"),
		ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "a", Name: "read"}, {ID: "b", Name: "read"}}},
		ai.ToolResultMessage{ToolCallID: "a", Content: strings.Repeat("a", 100)},
		ai.ToolResultMessage{ToolCallID: "b", Content: strings.Repeat("b", 100)},
		ai.AssistantMessage{Text: "finished"},
	}
	// Fits the final result plus answer, but not the first result: previously
	// this split between a and b, orphaning the b result from its tool call.
	history, recent := SplitMessages(messages, 30)
	assert.Equal(t, messages[:2], history)
	assert.Equal(t, messages[2:], recent)
}

func TestCompactRejectsEmptySummary(t *testing.T) {
	_, err := Compact(context.Background(), []ai.Message{ai.NewTextUserMessage("must preserve")}, nil, "", func(context.Context, []ai.Message, []ai.Message, string) (string, error) { return " \n", nil })
	assert.ErrorContains(t, err, "empty summary")
}

func TestCompactionThresholdReservesOutputAndHonorsRatio(t *testing.T) {
	settings := DefaultSettings()
	assert.False(t, ShouldCompact(102400, 128000, settings))
	assert.True(t, ShouldCompact(102401, 128000, settings))
	settings.AutoCompactRatio = 0.95
	assert.True(t, ShouldCompact(112000, 128000, settings), "reserve boundary overrides higher ratio")
	settings.AutoCompactRatio = 0
	assert.True(t, ShouldCompact(102401, 128000, settings), "zero uses default")
	assert.False(t, ShouldCompact(1000, 0, settings))
}
