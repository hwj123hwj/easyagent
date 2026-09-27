package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/stretchr/testify/require"
)

func TestFileCompletionPreservesMultilineUnicodeDraft(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.workspace = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(m.workspace, "中文.txt"), nil, 0600))
	m.input.insertString("第一行\n请看 @中 后缀\n第三行")
	m.input.cursorY, m.input.cursorX = 1, utf8.RuneCountInString("请看 @中")
	m.checkTriggerCompletion()
	require.True(t, m.completion.IsActive())
	m.acceptCompletion(m.completion.SelectedItem().InsertText)
	require.Equal(t, "第一行\n请看 @中文.txt 后缀\n第三行", m.input.Text())
	require.Equal(t, 1, m.input.cursorY)
	require.Equal(t, utf8.RuneCountInString("请看 @中文.txt"), m.input.cursorX)
	m.input.undo()
	require.Equal(t, "第一行\n请看 @中 后缀\n第三行", m.input.Text())
}

func TestPopupDoesNotSwallowCancel(t *testing.T) {
	for _, busy := range []bool{false, true} {
		m := newSelectorTestModel(t)
		m.openModelSelector()
		m.agentBusy = busy
		m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlC})
		require.False(t, m.modelSelect)
		require.False(t, m.completion.IsActive())
		require.False(t, m.agentBusy)
		require.False(t, m.quitting, "idle popup cancellation should keep the editor open")
	}
}

func TestCompletionCancelAndExitKeepDraftRules(t *testing.T) {
	m := newSelectorTestModel(t)
	m.input.insertString("/mo")
	m.checkTriggerCompletion()
	require.True(t, m.completion.IsActive())
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, m.completion.IsActive())
	require.False(t, m.quitting)
	require.Equal(t, "/mo", m.input.Text())
	m.openModelSelector()
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlD})
	require.False(t, m.quitting)
	m.input.Reset()
	m.openModelSelector()
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlD})
	require.True(t, m.quitting)
}

func TestModelCannotChangeDuringRun(t *testing.T) {
	m := newSelectorTestModel(t)
	m.agentBusy = true
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlP})
	require.False(t, m.modelSelect)
}

func TestScrollShowsEveryTranscriptRow(t *testing.T) {
	v := NewMessageViewport(80, 3)
	v.lines = []string{"one", "two", "three", "four", "five", "six", "seven"}
	v.userScrolled = true
	require.Equal(t, "one\ntwo\nthree", v.View())
	v.ScrollDown(3)
	require.Equal(t, "four\nfive\nsix", v.View())
}

func TestStreamErrorPreservesPartialAndRejectsLateEvents(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.agentBusy, m.streaming = true, true
	m.streamID = 7
	m.streamBuf = "部分回复"
	m.appliedStreamLen = len(m.streamBuf)
	m.viewport.SetStreaming(m.streamBuf)
	m.Update(streamEventMsg{7, AgentErrorMsg{Err: errors.New("connection lost")}})
	require.Empty(t, m.streamBuf)
	require.Empty(t, m.viewport.streaming)
	require.Zero(t, m.appliedStreamLen)
	require.Len(t, m.messages, 2)
	require.Equal(t, "部分回复", m.messages[0].Content)
	m.Update(streamEventMsg{7, StreamTextMsg{Delta: "late"}})
	m.Update(streamEventMsg{7, StreamDoneMsg{}})
	require.Empty(t, m.streamBuf)
	require.Len(t, m.messages, 2)
}

func TestHistoryRepeatedSubmissionRestoresFreshDraft(t *testing.T) {
	im := NewInputModel()
	im.AddHistory("old")
	im.AddHistory("latest")
	im.navigateHistory(-1)
	im.AddHistory(im.Text())
	im.Reset()
	im.insertString("new draft")
	im.navigateHistory(-1)
	require.Equal(t, "latest", im.Text())
	im.navigateHistory(1)
	require.Equal(t, "new draft", im.Text())
}

func TestSessionSwitchKeepsConfirmationMode(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
		m.session.SetConfirmEnabled(enabled)
		next := &runtime.AgentSession{}
		m.slashCmds.Register(slashcmd.Command{Name: "new", Handler: func(slashcmd.Context, string) (slashcmd.CommandResult, error) {
			return slashcmd.CommandResult{SessionSwitchTo: next}, nil
		}})
		m.handleSlashCommand("/new")
		require.Equal(t, enabled, next.ConfirmEnabled())
		next.SetConfirmEnabled(true)
		require.True(t, next.ConfirmEnabled(), "new session must have the TUI confirmation handler")
	}
}

func TestSmallWindowKeepsSelectedCompletionVisible(t *testing.T) {
	m := newSelectorTestModel(t)
	m.input.insertString(strings.Repeat("draft\n", 12))
	for height := 1; height <= 14; height++ {
		m.Update(tea.WindowSizeMsg{Width: 40, Height: height})
		m.openModelSelector()
		m.completion.selected = 20
		view := m.View()
		require.LessOrEqual(t, lipgloss.Height(view), height)
		require.LessOrEqual(t, lipgloss.Width(view), 40)
		require.Contains(t, view, "openai/model-20", "height %d", height)
	}
}

func TestCompletionLongUnicodeLabelUsesSingleRow(t *testing.T) {
	cm := NewCompletionState()
	cm.visible = true
	cm.items = []CompletionItem{{Label: strings.Repeat("中文文件", 30), Description: "description"}}
	view := NewCompletionPopup().Render(&cm, 24, 1)
	require.Equal(t, 3, lipgloss.Height(view))
	require.LessOrEqual(t, lipgloss.Width(view), 24)
}

func TestClearScreenRedrawsWithoutDiscardingTranscript(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.messages = []ChatMessage{{Role: "user", Content: "keep me"}}
	m.viewport.SetMessages(m.messages)
	_, cmd := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlL})
	require.NotNil(t, cmd)
	require.Contains(t, m.viewport.View(), "keep me")
}
