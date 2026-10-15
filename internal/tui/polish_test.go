package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/stretchr/testify/require"
)

func TestModelSearchKeepsDraftAndEmptyResultsOpen(t *testing.T) {
	m := newSelectorTestModel(t)
	m.input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("我的草稿")})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlP})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("MODEL-19")})
	require.Len(t, m.completion.Items(), 1)
	require.Equal(t, "openai/model-19", m.completion.SelectedItem().Label)
	require.Contains(t, ansi.Strip(m.View()), "Search: MODEL-19")
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("不存在")})
	require.True(t, m.completion.IsActive())
	require.Nil(t, m.completion.SelectedItem())
	require.Contains(t, ansi.Strip(m.View()), "No models match")
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.True(t, m.modelSelect)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyBackspace})
	require.Equal(t, "MODEL-19不存", m.completion.query)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlU})
	require.Len(t, m.completion.Items(), 21)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "我的草稿", m.input.Text())
}

func TestKeyboardToolsExpandCopyAndKeepDraft(t *testing.T) {
	m := toolTestModel()
	m.Update(ToolStartMsg{ID: "1", Name: "bash", Args: "first"})
	m.Update(ToolEndMsg{ID: "1", Name: "bash", Result: strings.Repeat("full result\n", 50)})
	m.Update(ToolStartMsg{ID: "2", Name: "bash", Args: "second"})
	m.Update(ToolEndMsg{ID: "2", Name: "bash", Result: "second result"})
	m.Update(StreamDoneMsg{})
	m.input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlT})
	require.True(t, m.toolFocus)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.True(t, m.messages[0].ToolsExpanded)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.False(t, m.messages[0].Tools[0].Collapsed)
	var copied string
	m.clipboardWriter = func(text string) error { copied = text; return nil }
	_, cmd := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyF2})
	require.NotNil(t, cmd)
	m.Update(cmd())
	require.Equal(t, "bash\nfirst\n\n"+strings.Repeat("full result\n", 50), copied)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 1, m.focusedTool.tool)
	// Long output does not prevent navigation to the next command.
	require.Contains(t, ansi.Strip(m.View()), "second")
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "draft", m.input.Text())
}

func TestWaitingSpinnerAndMultiTurnStreaming(t *testing.T) {
	m := newSelectorTestModel(t)
	_, cmd := m.sendMessage("test") // don't run its mock-less request
	require.NotNil(t, cmd)
	require.True(t, m.tickPending)
	require.Equal(t, "thinking", m.runStatus())
	m.Update(TickMsg{Time: time.Now()})
	require.Equal(t, 1, m.spinnerIdx)
	m.Update(StreamTextMsg{Delta: "First turn"})
	require.Contains(t, m.viewport.streaming, "First turn")
	require.Equal(t, "generating", m.runStatus())
	m.Update(ToolStartMsg{ID: "1", Name: "bash"})
	m.Update(ToolStartMsg{ID: "2", Name: "bash"})
	m.Update(ToolEndMsg{ID: "1", Name: "bash", Result: "ok"})
	require.Equal(t, "tools 1", m.runStatus())
	m.Update(ToolEndMsg{ID: "2", Name: "bash", Result: "ok"})
	m.Update(StreamTurnEndMsg{Text: "First turn"})
	require.Equal(t, "thinking", m.runStatus())
	m.Update(StreamTextMsg{Delta: "Second turn"})
	m.Update(StreamTurnEndMsg{Text: "Second turn"})
	m.Update(StreamDoneMsg{})
	require.Len(t, m.messages, 3)
	require.Equal(t, "First turn", m.messages[1].Content)
	require.Equal(t, "Second turn", m.messages[2].Content)
	require.Empty(t, m.viewport.streaming)
	require.Equal(t, "ready", m.runStatus())
}

func TestRestoreHistoryIncludesCompactedToolsAndInterruptedCalls(t *testing.T) {
	m := newSelectorTestModel(t)
	sess := m.session.Session()
	ctx := context.Background()
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("original user text")))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "before tools", ToolCalls: []ai.ToolCall{{ID: "done", Name: "bash", Args: "{}"}, {ID: "failed", Name: "bash", Args: "{}"}}, Usage: ai.Usage{InputTokens: 42}}))
	require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: "done", Content: "complete result"}))
	require.NoError(t, sess.AppendCompaction(ctx, "summarized"))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "after summary"}))
	restored := New(m.session, slashcmd.NewRegistry(), false)
	require.Len(t, restored.messages, 4)
	require.Equal(t, "original user text", restored.messages[0].Content)
	require.Equal(t, "complete result", restored.messages[0].Tools[0].Result)
	require.True(t, restored.messages[0].Tools[1].IsError)
	require.False(t, restored.messages[0].Tools[1].Streaming)
	require.True(t, restored.messages[0].ToolsExpanded)
	require.Equal(t, "before tools", restored.messages[1].Content)
	require.Equal(t, "after summary", restored.messages[3].Content)
	require.Equal(t, 42, restored.inputTokens)
	restored.input.navigateHistory(-1)
	require.Equal(t, "original user text", restored.input.Text())
}

func TestFooterAndWelcomeStayWithinTerminal(t *testing.T) {
	m := newSelectorTestModel(t)
	m.modelID = strings.Repeat("long-model-", 20)
	for _, width := range []int{1, 20, 40, 80, 120} {
		for _, height := range []int{1, 5, 12, 24} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			view := m.View()
			require.LessOrEqual(t, lipgloss.Width(view), width)
			require.LessOrEqual(t, lipgloss.Height(view), height)
			if width >= 40 && height >= 5 {
				require.Contains(t, ansi.Strip(view), "confirm")
			}
		}
	}
	m.input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, m.quitting)
	require.Equal(t, "draft", m.input.Text())
}

func TestRestoreUsesSelectedBranch(t *testing.T) {
	m := newSelectorTestModel(t)
	sess := m.session.Session()
	ctx := context.Background()
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("shared")))
	fork, err := sess.Storage().GetLeaf(ctx)
	require.NoError(t, err)
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("abandoned branch")))
	require.NoError(t, sess.MoveTo(ctx, fork, ""))
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("selected branch")))
	restored := New(m.session, slashcmd.NewRegistry(), false)
	require.Len(t, restored.messages, 2)
	require.Equal(t, "shared", restored.messages[0].Content)
	require.Equal(t, "selected branch", restored.messages[1].Content)
}

func TestToolFocusFallsBackWhenRunCollapsesGroup(t *testing.T) {
	m := toolTestModel()
	m.Update(ToolStartMsg{ID: "1", Name: "bash"})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlT})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 0, m.focusedTool.tool)
	m.Update(ToolEndMsg{ID: "1", Result: "ok"})
	m.Update(StreamDoneMsg{})
	require.Equal(t, -1, m.focusedTool.tool)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.True(t, m.messages[0].ToolsExpanded)
}
