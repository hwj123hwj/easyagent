package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/stretchr/testify/require"
)

func TestHistorySearchPreservesDraftAndRestoresMultilineWithoutSending(t *testing.T) {
	m := newSelectorTestModel(t)
	for _, text := range []string{"查看日志", "分析 内存\n保留详细结果", "查看日志"} {
		m.input.AddHistory(text)
	}
	m.input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("我的草稿")})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
	require.Len(t, m.completion.Items(), 2)
	require.Equal(t, "查看日志", m.completion.SelectedItem().InsertText)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("内存 不存在")})
	require.True(t, m.completion.IsActive())
	require.Nil(t, m.completion.SelectedItem())
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, "我的草稿", m.input.Text())
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyBackspace})
	require.Equal(t, "内存 不存", m.completion.query)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "我的草稿", m.input.Text())
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("分析 结果")})
	require.Len(t, m.completion.Items(), 1)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, "分析 内存\n保留详细结果", m.input.Text())
	require.False(t, m.agentBusy)
	require.Empty(t, m.messages)
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlZ})
	require.Equal(t, "我的草稿", m.input.Text(), "accept can be undone")
	m.input.Reset()
	m.input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/models")})
	m.checkTriggerCompletion()
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
	require.Equal(t, CompletionHistory, m.completion.Kind(), "history can open while slash completion is visible")
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "/models", m.input.Text())
}

func TestSessionPickerSearchAndResumeFullTranscript(t *testing.T) {
	m := newSelectorTestModel(t)
	s, err := m.app.CreateSession(context.Background())
	require.NoError(t, err)
	target := s.(*runtime.AgentSession)
	require.NoError(t, target.Session().AppendMessage(context.Background(), ai.NewTextUserMessage("内存分析")))
	require.NoError(t, target.Session().AppendMessage(context.Background(), ai.AssistantMessage{Text: "已分析", Usage: ai.Usage{InputTokens: 42}}))
	m.session.SetConfirmEnabled(false)
	m.input.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("继续草稿")})
	_, list := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlG})
	require.NotNil(t, list)
	m.Update(list())
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("内存")})
	require.Len(t, m.completion.Items(), 1)
	require.Contains(t, m.completion.SelectedItem().Preview, target.SessionID())
	_, open := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, open)
	m.Update(open())
	require.Equal(t, target.SessionID(), m.session.SessionID())
	require.Equal(t, "继续草稿", m.input.Text())
	require.False(t, m.session.ConfirmEnabled())
	require.False(t, m.completion.IsActive())
	require.Len(t, m.messages, 2)
	require.Equal(t, "已分析", m.messages[1].Content)
	require.Equal(t, 42, m.inputTokens)
	require.Equal(t, []string{"内存分析"}, m.input.history)
}

type failingPickerApp struct{ slashcmd.AppContext }

func (a failingPickerApp) SwitchSession(context.Context, string) (slashcmd.SessionContext, error) {
	return nil, errors.New("disk unavailable")
}

func TestPickerIgnoresCancelledLoadsAndKeepsSessionOnFailure(t *testing.T) {
	m := newSelectorTestModel(t)
	original := m.session
	_, list := m.openSessionPicker()
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	m.Update(list())
	require.False(t, m.completion.IsActive())
	_, old := m.openSessionPicker()
	_, current := m.openSessionPicker()
	m.Update(old())
	require.True(t, m.completion.loading)
	m.Update(current())
	m.searchCatalog = []CompletionItem{{Label: "failed", InsertText: "missing"}}
	filterPicker(&m.completion, m.searchCatalog)
	m.app = failingPickerApp{m.app}
	_, open := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(open())
	require.Same(t, original, m.session)
	require.Contains(t, m.copyNotice, "disk unavailable")
	require.True(t, m.completion.IsActive())
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	m.agentBusy = true
	_, cmd := m.openSessionPicker()
	require.Nil(t, cmd)
	require.False(t, m.completion.IsActive())
}

func TestSearchPopupsFitSmallTerminalsIncludingEmptyResults(t *testing.T) {
	m := newSelectorTestModel(t)
	m.input.AddHistory(strings.Repeat("中文历史很长\n", 30))
	for _, size := range []tea.WindowSizeMsg{{Width: 1, Height: 1}, {Width: 20, Height: 5}, {Width: 40, Height: 12}, {Width: 80, Height: 24}} {
		m.Update(size)
		m.openHistoryPicker()
		for _, query := range []string{"", "没有结果"} {
			m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
			view := m.View()
			require.LessOrEqual(t, lipgloss.Width(view), size.Width)
			require.LessOrEqual(t, lipgloss.Height(view), size.Height)
		}
		m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestMarkdownReadingHeadingsTablesAndResize(t *testing.T) {
	text := "# 总览\n\n## 内存情况\n\n### 进程\n\n| 项目 | 数值 |\n| --- | --- |\n| 内存 | 14 GiB |\n| 交换 | 0 GiB |\n\n正文 **重点** 与 `代码`。"
	for _, dark := range []bool{false, true} {
		previous := lipgloss.HasDarkBackground()
		lipgloss.SetHasDarkBackground(dark)
		renderer := NewMarkdownRenderer(160)
		for _, width := range []int{160, 40, 24, 160} {
			renderer.SetWidth(width)
			result := renderer.Render(text)
			plain := ansi.Strip(result)
			require.NotContains(t, plain, "#")
			for _, word := range []string{"总览", "内存情况", "进程", "14 GiB", "交换", "重点", "代码"} {
				require.Contains(t, plain, word)
			}
			for _, line := range strings.Split(result, "\n") {
				require.LessOrEqual(t, ansi.StringWidth(line), min(width, 96))
			}
		}
		lipgloss.SetHasDarkBackground(previous)
	}
}
