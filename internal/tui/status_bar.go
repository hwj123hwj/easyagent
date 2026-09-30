package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// StatusBar renders the bottom status bar showing agent state, model info,
// token count, and workspace.
//
// ┌─────────────────────────────────────────────────────────────────┐
// │ ● ready │ model: gpt-4o │ tokens: 12.3k/128k │ workspace: easyagent │
// └─────────────────────────────────────────────────────────────────┘
type StatusBar struct {
	theme *Theme
}

// NewStatusBar creates a styled status bar.
func NewStatusBar() *StatusBar {
	return &StatusBar{theme: DefaultTheme()}
}

// Render produces the status bar string.
func (sb *StatusBar) Render(
	width int,
	status string,
	spinnerIdx int,
	provider, modelID, workspace string,
	streaming bool,
	inputTokens, outputTokens int,
	detail ...string,
) string {
	sep := sb.theme.StatusDim.Render(" │ ")

	// Status indicator
	var statusPart string
	switch status {
	case "generating":
		statusPart = sb.theme.StatusBusy.Render(spinnerChars[spinnerIdx%len(spinnerChars)] + " writing")
	case "busy":
		spinner := spinnerChars[spinnerIdx%len(spinnerChars)]
		statusPart = sb.theme.StatusBusy.Render(spinner + " working")
	case "error":
		statusPart = sb.theme.StatusError.Render("● error")
	case "thinking":
		spinner := spinnerChars[spinnerIdx%len(spinnerChars)]
		statusPart = sb.theme.StatusBusy.Render(spinner + " thinking")
	default:
		if strings.HasPrefix(status, "tools ") {
			statusPart = sb.theme.StatusBusy.Render(spinnerChars[spinnerIdx%len(spinnerChars)] + " " + status)
		} else {
			statusPart = sb.theme.StatusReady.Render("● ready")
		}
	}

	// Reserve room for phase and permission mode before fitting metadata.
	content := ansi.Truncate(statusPart, width, "")
	if len(detail) > 0 && detail[0] != "" {
		mode := sb.theme.StatusDim.Render(detail[0])
		if lipgloss.Width(content+sep+mode) <= width {
			content += sep + mode
		}
	}
	remaining := width - lipgloss.Width(content+sep)
	if remaining > 3 {
		label := provider + "/" + modelID
		if remaining < 28 {
			label = modelID
		}
		content += sep + sb.theme.StatusAccent.Render(ansi.Truncate(terminalText(label), remaining, "…"))
	}
	var metadata []string
	if workspace != "" {
		metadata = append(metadata, terminalText(filepath.Base(workspace)))
	}
	if inputTokens+outputTokens > 0 {
		metadata = append(metadata, fmt.Sprintf("%s ↑ %s ↓", formatTokenCount(inputTokens), formatTokenCount(outputTokens)))
	}
	for _, part := range metadata {
		if lipgloss.Width(content+sep+part) <= width {
			content += sep + sb.theme.StatusDim.Render(part)
		}
	}
	content += strings.Repeat(" ", max(0, width-lipgloss.Width(content)))

	return content
}

// HelpHint renders a one-line help hint above the status bar.
func (sb *StatusBar) HelpHint(agentBusy bool) string {
	if agentBusy {
		return sb.theme.HelpText.Render("Ctrl+C: cancel | Ctrl+L: clear | Ctrl+D: exit")
	}
	return sb.theme.HelpText.Render("Enter: send | Ctrl+J: newline | Ctrl+L: clear | Ctrl+D: exit | /help: commands")
}

// formatTokenCount converts a raw token number to a human-readable string.
func formatTokenCount(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
