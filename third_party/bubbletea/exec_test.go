package tea

import (
	"bytes"
	"io"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
)

type execFinishedMsg struct{ err error }

type testExecModel struct {
	cmd string
	err error
}

func (m testExecModel) Init() Cmd {
	c := exec.Command(m.cmd) //nolint:gosec
	return ExecProcess(c, func(err error) Msg {
		return execFinishedMsg{err}
	})
}

func (m *testExecModel) Update(msg Msg) (Model, Cmd) {
	switch msg := msg.(type) {
	case execFinishedMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, Quit
	}

	return m, nil
}

func (m *testExecModel) View() string {
	return "\n"
}

type spyRenderer struct {
	renderer
	calledReset bool
}

func (r *spyRenderer) resetLinesRendered() {
	r.calledReset = true
	r.renderer.resetLinesRendered()
}

func TestTeaExec(t *testing.T) {
	type test struct {
		name      string
		cmd       string
		expectErr bool
	}
	tests := []test{
		{
			name:      "invalid command",
			cmd:       "invalid",
			expectErr: true,
		},
	}

	if runtime.GOOS != "windows" {
		tests = append(tests, []test{
			{
				name:      "true",
				cmd:       "true",
				expectErr: false,
			},
			{
				name:      "false",
				cmd:       "false",
				expectErr: true,
			},
		}...)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			var in bytes.Buffer

			m := &testExecModel{cmd: test.cmd}
			p := NewProgram(m, WithInput(&in), WithOutput(&buf))
			if _, err := p.Run(); err != nil {
				t.Error(err)
			}
			p.renderer = &spyRenderer{renderer: p.renderer}

			if m.err != nil && !test.expectErr {
				t.Errorf("expected no error, got %v", m.err)

				if !p.renderer.(*spyRenderer).calledReset {
					t.Error("expected renderer to be reset")
				}
			}
			if m.err == nil && test.expectErr {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestExecPreservesIgnoredSignals(t *testing.T) {
	m := &signalFreeExecModel{}
	p := NewProgram(m, WithInput(&bytes.Buffer{}), WithOutput(&bytes.Buffer{}), WithoutSignals())
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadUint32(&p.ignoreSignals) != 1 {
		t.Fatal("Exec re-enabled signals disabled by WithoutSignals")
	}
}

type signalFreeCommand struct{}

func (signalFreeCommand) SetStdin(io.Reader)  {}
func (signalFreeCommand) SetStdout(io.Writer) {}
func (signalFreeCommand) SetStderr(io.Writer) {}
func (signalFreeCommand) Run() error          { return nil }

type signalFreeExecModel struct{}

func (*signalFreeExecModel) Init() Cmd {
	return Exec(signalFreeCommand{}, func(err error) Msg { return execFinishedMsg{err} })
}
func (m *signalFreeExecModel) Update(msg Msg) (Model, Cmd) {
	if _, ok := msg.(execFinishedMsg); ok {
		return m, Quit
	}
	return m, nil
}
func (*signalFreeExecModel) View() string { return "" }
