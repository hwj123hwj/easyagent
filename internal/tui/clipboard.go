package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type clipboardResultMsg struct{ err error }

// writeClipboard uses stdin, never a shell or interpolation of selected text.
// A timeout keeps a missing desktop clipboard service from hanging a command.
func writeClipboard(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "[Console]::InputEncoding = [System.Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"}}
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		candidates = append(candidates, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	var lastErr error
	for _, args := range candidates {
		path, err := exec.LookPath(args[0])
		if err != nil {
			lastErr = err
			continue
		}
		cmd := exec.CommandContext(ctx, path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("system clipboard unavailable: %w", lastErr)
}

func (m *TuiModel) copyText(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		m.copyNotice = "Nothing to copy"
		return nil
	}
	text = ansi.Strip(text)
	m.copyNotice = "Copying..."
	write := m.clipboardWriter
	return func() tea.Msg { return clipboardResultMsg{err: write(text)} }
}

func (m *TuiModel) copyLastReply() tea.Cmd {
	if m.streamBuf != "" {
		return m.copyText(m.streamBuf)
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" && m.messages[i].Content != "" {
			return m.copyText(m.messages[i].Content)
		}
	}
	return m.copyText("")
}

func (m *TuiModel) copyConversation() tea.Cmd {
	var b strings.Builder
	for _, msg := range m.messages {
		fmt.Fprintf(&b, "%s:\n%s\n\n", msg.Role, msg.Content)
		for _, tool := range msg.Tools {
			fmt.Fprintf(&b, "[%s]\n%s\n%s\n\n", tool.Name, tool.Args, tool.Result)
		}
	}
	if m.streamBuf != "" {
		fmt.Fprintf(&b, "assistant:\n%s", m.streamBuf)
	}
	return m.copyText(strings.TrimSpace(b.String()))
}
