package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

func TestSelectionUnicodeAndDirection(t *testing.T) {
	for _, tc := range []struct {
		text string
		a, b int
		want string
	}{
		{"A中文B", 2, 4, "中文"},
		{"A中文B", 4, 2, "中文"},
		{"e\u0301🙂x", 0, 1, "e\u0301🙂"},
		{"👨‍👩‍👧‍👦 hello", 0, 1, "👨‍👩‍👧‍👦"},
	} {
		s := textSelection{lines: []string{tc.text}, anchor: screenPoint{tc.a, 0}, end: screenPoint{tc.b, 0}}
		if got := s.selectedText(); got != tc.want {
			t.Errorf("%q selected=%q want=%q", tc.text, got, tc.want)
		}
		if ansi.Strip(s.view()) != tc.text {
			t.Fatal("highlight changed text")
		}
	}
	s := textSelection{lines: []string{"abc   ", "  中文", "end"}, anchor: screenPoint{1, 0}, end: screenPoint{1, 2}}
	if got := s.selectedText(); got != "bc\n  中文\nen" {
		t.Fatalf("multiline selection=%q", got)
	}
}

func TestDragCopySnapshotAndScroll(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.messages = []ChatMessage{{Role: "user", Content: "你好 world"}}
	m.viewport.SetMessages(m.messages)
	before := m.View()
	row := -1
	for y, line := range m.frame {
		if strings.Contains(line, "你好 world") {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("message missing")
	}
	copied := ""
	m.clipboardWriter = func(text string) error { copied = text; return nil }
	m.Update(tea.MouseMsg{X: 2, Y: row, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 5, Y: row, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(StreamTextMsg{Delta: "new streamed reply"})
	m.Update(TickMsg{})
	if !strings.Contains(m.View(), "\x1b[7m你好\x1b[0m") {
		t.Fatal("selection not highlighted")
	}
	if strings.Contains(m.View(), "new streamed reply") {
		t.Fatal("stream moved selection snapshot")
	}
	_, cmd := m.Update(tea.MouseMsg{X: 5, Y: row, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if cmd == nil {
		t.Fatal("release should copy")
	}
	m.Update(cmd())
	if copied != "你好" || !strings.Contains(m.View(), "Copied") {
		t.Fatalf("clipboard=%q", copied)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || m.quitting {
		t.Fatal("Ctrl+C on selection should copy, not exit")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.selection.lines) != 0 || m.View() == before || !strings.Contains(m.View(), "new streamed reply") {
		t.Fatal("Esc did not resume live view")
	}
	m.Update(tea.MouseMsg{X: 2, Y: row, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if len(m.selection.lines) != 0 {
		t.Fatal("resize must invalidate selection")
	}
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
}

func TestCopyShortcutsAndFailure(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.messages = []ChatMessage{{Role: "assistant", Content: "**answer**\n代码", Tools: []ToolCallInfo{{Name: "read", Result: "full result", Collapsed: true}}}}
	copied := ""
	m.clipboardWriter = func(s string) error { copied = s; return nil }
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m.Update(cmd())
	if copied != "**answer**\n代码" {
		t.Fatalf("reply=%q", copied)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF3})
	m.Update(cmd())
	if !strings.Contains(copied, "full result") {
		t.Fatal("full copy omitted collapsed tool")
	}
	m.clipboardWriter = func(string) error { return errors.New("test failure") }
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m.Update(cmd())
	if !strings.Contains(m.copyNotice, "Copy failed") || m.quitting {
		t.Fatal("copy failure must be visible and nonfatal")
	}
}

// Run the real program lifecycle: both buffers and mouse modes must be restored
// on exit, and copy shortcuts must never disable mouse reporting mid-session.
func TestProgramOwnsMouseUntilExit(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	var output bytes.Buffer
	p := newProgram(m, tea.WithInput(strings.NewReader("")), tea.WithOutput(&output), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	p.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	p.Send(tea.KeyMsg{Type: tea.KeyF2})
	p.Send(tea.KeyMsg{Type: tea.KeyF3})
	p.Send(tea.Quit())
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		p.Kill()
		t.Fatal("program failed to stop")
	}
	out := output.String()
	for _, seq := range []string{"\x1b[?1049h", "\x1b[?1002h", "\x1b[?1006h", "\x1b[?2004h", "\x1b[?1049l", "\x1b[?1002l", "\x1b[?1006l", "\x1b[?2004l"} {
		if !strings.Contains(out, seq) {
			t.Fatalf("missing terminal lifecycle sequence %q", seq)
		}
	}
	if strings.Count(out, "\x1b[?1002l") != 1 {
		t.Fatal("mouse mode released before shutdown")
	}
}

func TestBusySubmitKeepsDraftAndCancelRejectsLateEvents(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.agentBusy = true
	m.streamID = 7
	m.input.insertString("next draft")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.input.Text() != "next draft" || len(m.messages) != 0 {
		t.Fatal("busy submit started another run or lost draft")
	}
	cancelled := false
	m.registerStreamCancel(func() { cancelled = true })
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m.Update(streamEventMsg{7, StreamTextMsg{Delta: "stale response"}})
	m.Update(streamEventMsg{7, StreamDoneMsg{InputTokens: 999}})
	if !cancelled || m.agentBusy || m.streamBuf != "" || m.inputTokens != 0 {
		t.Fatal("cancelled run changed current state")
	}
}

func TestConfirmationArrowsDoNotResolve(t *testing.T) {
	cs := NewConfirmationState()
	cs.Show("id", "bash", "command")
	_, _, resolved := cs.HandleKey(tea.KeyMsg{Type: tea.KeyRight})
	if resolved || cs.Selected() != 1 {
		t.Fatal("right arrow must only select No")
	}
	approved, _, resolved := cs.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !resolved || approved {
		t.Fatal("Enter must respect selected No")
	}
}

func TestConfirmationInterruptsSelectionAndCancellationIsScoped(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.selection = textSelection{lines: []string{"frozen"}}
	result := make(chan ConfirmationResultMsg, 1)
	m.Update(ConfirmationDialogMsg{ToolCallID: "new", Description: "run command", Result: result})
	if len(m.selection.lines) != 0 || !m.confirmation.IsActive() {
		t.Fatal("confirmation hidden by selection")
	}
	m.Update(confirmationCancelledMsg{result: make(chan ConfirmationResultMsg, 1)})
	if !m.confirmation.IsActive() {
		t.Fatal("stale cancellation hid current dialog")
	}
	m.Update(confirmationCancelledMsg{result: result})
	if m.confirmation.IsActive() {
		t.Fatal("cancel did not close dialog")
	}
}

func TestOutputCannotChangeTerminalModes(t *testing.T) {
	v := NewMessageViewport(80, 24)
	hostile := "before\x1b[?1049l\x1b[?1002l\x1b[2Jafter"
	v.SetMessages([]ChatMessage{{Role: "user", Content: hostile, Tools: []ToolCallInfo{{Name: "read", Result: hostile, Collapsed: false}}}})
	v.SetStreaming(hostile)
	view := v.View()
	for _, seq := range []string{"\x1b[?1049l", "\x1b[?1002l", "\x1b[2J"} {
		if strings.Contains(view, seq) {
			t.Fatalf("external terminal control leaked: %q", seq)
		}
	}
	if !strings.Contains(ansi.Strip(view), "beforeafter") {
		t.Fatal("safe content lost")
	}
}
