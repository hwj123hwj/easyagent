package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// newSlashCmdTestModel builds a TuiModel with a slash registry containing a
// caller-controlled command. The returned channel receives the slashcmd
// Context the handler was invoked with.

// driveCmd executes a tea.Cmd (flattening tea.BatchMsg) in background
// goroutines, like the Bubble Tea runtime would.
func driveCmd(cmd tea.Cmd) <-chan tea.Msg {
	ch := make(chan tea.Msg, 8)
	run := func(c tea.Cmd) {
		if c == nil {
			return
		}
		if msg := c(); msg != nil {
			ch <- msg
		}
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				go run(c)
			}
			return
		}
		run(func() tea.Msg { return msg })
	}()
	return ch
}

func newSlashCmdTestModel(t *testing.T, handler func(slashcmd.Context, string) (slashcmd.CommandResult, error)) (*TuiModel, <-chan slashcmd.Context) {
	t.Helper()
	ctxCh := make(chan slashcmd.Context, 1)
	reg := slashcmd.NewRegistry()
	reg.Register(slashcmd.Command{
		Name: "compact",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			ctxCh <- ctx
			return handler(ctx, args)
		},
	})
	session := &runtime.AgentSession{}
	m := New(session, reg, false)
	m.width, m.height = 80, 24
	return m, ctxCh
}

// TestHandleSlashCommand_SlowCommandDoesNotBlock verifies that a slow slash
// command (/compact) returns control to the Update loop immediately instead
// of blocking it — the bug that froze the whole terminal.
func TestHandleSlashCommand_SlowCommandDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		<-release // simulate a slow LLM summarization
		return slashcmd.CommandResult{Output: "compacted"}, nil
	})

	done := make(chan struct{})
	go func() {
		m.handleSlashCommand("/compact")
		close(done)
	}()

	select {
	case <-done:
		// handleSlashCommand returned while the handler is still blocked:
		// the Update loop is not stalled.
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("handleSlashCommand blocked until command finished; slow commands must run async")
	}
	close(release)
}

// TestHandleSlashCommand_BusyStateAndSpinner checks busy flag + spinner tick
// are set right away, so the UI shows progress while the command runs.
func TestHandleSlashCommand_BusyStateAndSpinner(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		<-release
		return slashcmd.CommandResult{}, nil
	})

	newModel, cmd := m.handleSlashCommand("/compact keep the API notes")
	m = newModel.(*TuiModel)

	if !m.slashCmdBusy {
		t.Fatal("slashCmdBusy should be true while the command runs")
	}
	if m.slashCmdName != "compact" {
		t.Errorf("slashCmdName = %q, want %q", m.slashCmdName, "compact")
	}
	// Echoes the command as a user message so users see what is running.
	if len(m.messages) == 0 || m.messages[0].Content != "/compact keep the API notes" {
		t.Fatalf("command should be echoed to the message list, got %+v", m.messages)
	}
	if cmd == nil {
		t.Fatal("expected spinner tick cmd to keep the spinner animating")
	}
}

// TestSlashCmdDoneMsg_AppendsOutput runs the full async roundtrip: result
// message lands, busy state clears, output appended.
func TestSlashCmdDoneMsg_AppendsOutput(t *testing.T) {
	release := make(chan struct{})
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		<-release
		return slashcmd.CommandResult{Output: "Context compacted: 42 → 5 messages"}, nil
	})
	defer close(release)

	newModel, _ := m.handleSlashCommand("/compact")
	m = newModel.(*TuiModel)
	id := m.streamID

	m.Update(SlashCmdDoneMsg{
		Input:    "/compact",
		Output:   "Context compacted: 42 → 5 messages",
		StreamID: id,
	})

	if m.slashCmdBusy {
		t.Fatal("slashCmdBusy should be cleared after SlashCmdDoneMsg")
	}
	last := m.messages[len(m.messages)-1]
	if last.Content != "Context compacted: 42 → 5 messages" {
		t.Errorf("output not appended, got %q", last.Content)
	}
}

// TestSlashCmdDoneMsg_StaleResultIgnored verifies the streamID guard: a
// result from a cancelled/superseded command must not clear the busy state
// of the currently running one.
func TestSlashCmdDoneMsg_StaleResultIgnored(t *testing.T) {
	release := make(chan struct{})
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		<-release
		return slashcmd.CommandResult{Output: "late"}, nil
	})
	defer close(release)

	newModel, _ := m.handleSlashCommand("/compact")
	m = newModel.(*TuiModel)

	m.Update(SlashCmdDoneMsg{Input: "/compact", Output: "late", StreamID: m.streamID + 999})

	if !m.slashCmdBusy {
		t.Fatal("stale SlashCmdDoneMsg must not clear the current busy state")
	}
}

// TestHandleSlashCommand_CtrlCCancels verifies Ctrl+C aborts the running
// command through its own context (before: cancelStream was a no-op for
// slash commands, so /compact could not be interrupted at all).
func TestHandleSlashCommand_CtrlCCancels(t *testing.T) {
	ctxGot := make(chan context.Context, 1)
	release := make(chan struct{})
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		ctxGot <- ctx.Ctx
		<-release
		return slashcmd.CommandResult{}, nil
	})
	closeOnFinish := func() { close(release) }
	defer closeOnFinish()

	newModel, cmd := m.handleSlashCommand("/compact")
	m = newModel.(*TuiModel)
	if cmd == nil {
		t.Fatal("expected batch cmd")
	}
	// Drive the command goroutines like tea would.
	driveCmd(cmd)

	var runCtx context.Context
	select {
	case runCtx = <-ctxGot:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never invoked")
	}

	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = newModel.(*TuiModel)

	select {
	case <-runCtx.Done():
		// good: the command context was cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("Ctrl+C did not cancel the slash command context")
	}

	// After cancel, the goroutine returns a SlashCmdDoneMsg carrying the
	// cancellation; busy state clears via the normal path.
	m.Update(SlashCmdDoneMsg{Input: "/compact", Err: errors.New("cancelled"), StreamID: m.streamID})
	if m.slashCmdBusy {
		t.Fatal("busy state should clear after cancelled result")
	}
	if last := m.messages[len(m.messages)-1]; !strings.Contains(last.Content, "cancelled") {
		t.Errorf("expected cancellation note, got %q", last.Content)
	}
}

// TestHandleSlashCommand_ErrorSurfaced checks a failing command surfaces its
// error without clearing the transcript.
func TestHandleSlashCommand_ErrorSurfaced(t *testing.T) {
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		return slashcmd.CommandResult{}, errors.New("compact failed: boom")
	})

	newModel, cmd := m.handleSlashCommand("/compact")
	m = newModel.(*TuiModel)
	if cmd == nil {
		t.Fatal("expected batch cmd")
	}
	var done SlashCmdDoneMsg
	timeout := time.After(2 * time.Second)
collect:
	for {
		select {
		case msg := <-driveCmd(cmd):
			if d, ok := msg.(SlashCmdDoneMsg); ok {
				done = d
				break collect
			}
		case <-timeout:
			t.Fatal("timeout waiting for SlashCmdDoneMsg")
		}
	}
	m.Update(done)

	last := m.messages[len(m.messages)-1]
	if !strings.Contains(last.Content, "compact failed: boom") {
		t.Errorf("error not surfaced, got %q", last.Content)
	}
}

// TestHandleSlashCommand_BusyRejectsSecondCommand ensures a second slow
// command is rejected while one is still running (no queue pile-up).
func TestHandleSlashCommand_BusyRejectsSecondCommand(t *testing.T) {
	release := make(chan struct{})
	m, _ := newSlashCmdTestModel(t, func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		<-release
		return slashcmd.CommandResult{}, nil
	})
	defer close(release)

	newModel, _ := m.handleSlashCommand("/compact")
	m = newModel.(*TuiModel)
	before := len(m.messages)

	newModel, _ = m.handleSlashCommand("/compact again")
	m = newModel.(*TuiModel)

	if len(m.messages) != before {
		t.Fatalf("second command must not append messages while busy, %d → %d", before, len(m.messages))
	}
	if !strings.Contains(m.copyNotice, "still running") {
		t.Errorf("expected busy notice, got %q", m.copyNotice)
	}
}
