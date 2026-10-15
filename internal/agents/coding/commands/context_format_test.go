package commands

import (
	"context"
	"fmt"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatContextSnapshot(t *testing.T) {
	cached := 8200
	s := agent.ContextSnapshot{
		Model:    ai.Model{Provider: "openai", ID: "glm-5.3-flash", ContextWindow: 200000},
		System:   "system prompt...",
		Messages: make([]agent.ContextMessage, 23),
		Tools: []ai.ToolDefinition{
			{Name: "bash"}, {Name: "read"}, {Name: "edit"}, {Name: "write"},
			{Name: "grep"}, {Name: "find"}, {Name: "ls"}, {Name: "multiedit"},
			{Name: "web_fetch"}, {Name: "web_search"}, {Name: "memory"}, {Name: "todo"},
		},
		Usage: agent.ContextUsage{
			EstimatedTokens: 48230,
			ContextWindow:   200000,
			WindowKnown:     true,
			Messages:        31200,
			System:          6100,
			Tools:           10930,
			LastRequest:     ai.Usage{InputTokens: 45100, OutputTokens: 1204, CachedInputTokens: &cached},
		},
		Compactions: []session.CompactionRecord{},
	}
	out := formatContextSnapshot(s)
	fmt.Println(out) // 人工检查渲染效果

	// 内容断言
	assert.Contains(t, out, "glm-5.3-flash")
	assert.Contains(t, out, "24.1%") // 48230/200000
	assert.Contains(t, out, "48.2k")
	assert.Contains(t, out, "200.0k")
	assert.Contains(t, out, "23 entries")
	assert.Contains(t, out, "31.2k")
	assert.Contains(t, out, "45.1k in / 1.2k out")
	assert.Contains(t, out, "8.2k cached")
	assert.Contains(t, out, "█")
	assert.Contains(t, out, "bash, read, edit")
}

func TestFormatContextSnapshot_UnknownWindow(t *testing.T) {
	s := agent.ContextSnapshot{
		Model:    ai.Model{Provider: "openai", ID: "mystery-model"},
		Usage:    agent.ContextUsage{EstimatedTokens: 999},
		Messages: []agent.ContextMessage{},
	}
	out := formatContextSnapshot(s)
	assert.Contains(t, out, "window size unknown")
	assert.NotContains(t, out, "NaN")
}

func TestContextCommandUsesFormatter(t *testing.T) {
	// mockSession 不实现 ContextSnapshot → 走 fallback 分支，验证未被破坏
	reg := newRegistry()
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test", provider: "openai", modelID: "gpt-4o", toolNames: []string{"bash"}},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/context")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "test")
	assert.Contains(t, result.Output, "bash")
}
