package tui

import (
	"context"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// tea.Exec releases raw mode, input capture and the renderer together. Running
// a terminal wizard in a tea.Cmd would race the renderer and consume its keys.
func (m *TuiModel) runTerminalCommand(input string) (tea.Model, tea.Cmd) {
	m.streamID++
	id := m.streamID
	ctx, cancel := context.WithCancel(context.Background())
	m.registerSlashCmdCancel(cancel)
	name, _ := slashcmd.ParseSlashCommand(input)
	m.slashCmdBusy, m.slashCmdName = true, name
	m.messages = append(m.messages, ChatMessage{Role: "user", Content: input, Timestamp: time.Now()})
	m.viewport.SetMessages(m.messages)
	m.viewport.GotoBottom()
	command := &terminalCommand{
		ctx:      slashcmd.Context{Ctx: ctx, Session: m.session, App: m.app},
		registry: m.slashCmds, input: input, cancel: cancel,
	}
	return m, tea.Exec(command, func(err error) tea.Msg {
		cancel()
		return SlashCmdDoneMsg{Input: input, Output: command.result.Output, Err: err, StreamID: id}
	})
}

type terminalCommand struct {
	ctx      slashcmd.Context
	registry *slashcmd.Registry
	input    string
	cancel   context.CancelFunc
	result   slashcmd.CommandResult
}

func (c *terminalCommand) SetStdin(in io.Reader)   { c.ctx.Stdin = in }
func (c *terminalCommand) SetStdout(out io.Writer) { c.ctx.Stdout = out }
func (c *terminalCommand) SetStderr(io.Writer)     {}
func (c *terminalCommand) Run() error {
	defer c.cancel()
	var err error
	c.result, err = c.registry.Execute(c.ctx, c.input)
	return err
}
