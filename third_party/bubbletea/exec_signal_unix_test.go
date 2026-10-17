//go:build !windows

package tea

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

type cancelSignalCommand struct{}

func (cancelSignalCommand) SetStdin(io.Reader)  {}
func (cancelSignalCommand) SetStdout(io.Writer) {}
func (cancelSignalCommand) SetStderr(io.Writer) {}
func (cancelSignalCommand) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		return err
	}
	// Return as soon as the wizard receives Ctrl+C. The program's independent
	// signal consumer may not have been scheduled yet.
	<-ctx.Done()
	return nil
}

type signalExecModel struct{ remaining int }

func (m *signalExecModel) Init() Cmd {
	return Exec(cancelSignalCommand{}, func(err error) Msg { return execFinishedMsg{err} })
}
func (m *signalExecModel) View() string { return "" }
func (m *signalExecModel) Update(msg Msg) (Model, Cmd) {
	if done, ok := msg.(execFinishedMsg); ok {
		if done.err != nil {
			panic(done.err)
		}
		m.remaining--
		if m.remaining > 0 {
			return m, m.Init()
		}
		// Once restored, a new interrupt must still reach the program.
		return m, func() Msg { _ = syscall.Kill(os.Getpid(), syscall.SIGINT); return nil }
	}
	return m, nil
}

func TestExecSignalOwnership(t *testing.T) {
	if os.Getenv("BUBBLETEA_EXEC_SIGNAL_HELPER") == "1" {
		m := &signalExecModel{remaining: 100}
		p := NewProgram(m, WithInput(strings.NewReader("")), WithOutput(io.Discard), WithoutRenderer())
		_, err := p.Run()
		if m.remaining != 0 {
			t.Fatalf("wizard interrupt killed TUI with %d executions left: %v", m.remaining, err)
		}
		if !errors.Is(err, ErrInterrupted) {
			t.Fatalf("fresh interrupt after restore was lost: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecSignalOwnership$")
	cmd.Env = append(os.Environ(), "BUBBLETEA_EXEC_SIGNAL_HELPER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("signal ownership: %v\n%s", err, output)
	}
}
