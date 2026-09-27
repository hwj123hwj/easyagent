package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// Run starts the Bubble Tea TUI program.
// This is the main entry point for the new TUI interactive mode.
func Run(session *runtime.AgentSession, cmds *slashcmd.Registry, app slashcmd.AppContext, autoApprove bool) error {
	m := New(session, cmds, autoApprove)
	m.app = app

	// Set workspace for display
	if cwd, err := os.Getwd(); err == nil {
		m.SetWorkspace(cwd)
	}

	// Keep wheel events inside the alternate screen. Drag selection is handled
	// by the TUI, so copying never requires releasing terminal mouse capture.
	p := newProgram(m)
	defer m.cancelStream()
	defer tea.SetCursorColumn(0)

	// Wire program reference so callbacks can send messages
	m.SetProgram(p)

	// NOTE: Do NOT print anything to stdout before p.Run().
	// AltScreen mode creates a separate buffer, but anything printed
	// before p.Run() goes into the terminal's normal scrollback,
	// which users can see by scrolling up.

	_, err := p.Run()
	return err
}

// BannerText returns the banner string for the TUI.
func BannerText() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("╔══════════════════════════════════════════╗\n")
	b.WriteString("║           EasyAgent TUI           ║\n")
	b.WriteString("║                                          ║\n")
	b.WriteString("║  Enter: Send  │  Ctrl+J: Newline          ║\n")
	b.WriteString("║  Ctrl+C: Cancel/Exit  │  Ctrl+L: Clear      ║\n")
	b.WriteString("║  ↑↓: History  │  Ctrl+P: Model select      ║\n")
	b.WriteString("║  PgUp/PgDn: Scroll  │  Drag: Copy     ║\n")
	b.WriteString("╚══════════════════════════════════════════╝\n")
	b.WriteString("\n")
	return b.String()
}

// QuitMsg is a message that triggers program exit.
type QuitMsg struct{}

// String returns a human-readable description of the model state (for debugging).
func (m *TuiModel) String() string {
	return fmt.Sprintf("TuiModel{msgs:%d streaming:%v busy:%v input:%q}",
		len(m.messages), m.streaming, m.agentBusy, m.input.Text())
}

func newProgram(m *TuiModel, options ...tea.ProgramOption) *tea.Program {
	options = append(options, tea.WithAltScreen(), tea.WithMouseCellMotion())
	return tea.NewProgram(m, options...)
}
