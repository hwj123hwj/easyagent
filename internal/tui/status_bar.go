package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
) string {
	sep := sb.theme.StatusDim.Render(" │ ")

	// Status indicator
	var statusPart string
	switch status {
	case "busy":
		spinner := spinnerChars[spinnerIdx%len(spinnerChars)]
		statusPart = sb.theme.StatusBusy.Render(spinner + " working")
	case "error":
		statusPart = sb.theme.StatusError.Render("● error")
	case "thinking":
		spinner := spinnerChars[spinnerIdx%len(spinnerChars)]
		statusPart = sb.theme.StatusBusy.Render(spinner + " thinking")
	default:
		statusPart = sb.theme.StatusReady.Render("● ready")
	}

	// Model info
	modelPart := sb.theme.StatusDim.Render("model: ") +
		sb.theme.StatusAccent.Render(provider+"/"+modelID)

	// Token usage
	tokenPart := ""
	if inputTokens > 0 || outputTokens > 0 {
		tokenPart = sb.theme.StatusDim.Render("tokens: ") +
			sb.theme.StatusAccent.Render(fmt.Sprintf("%s ↑ %s ↓",
				formatTokenCount(inputTokens),
				formatTokenCount(outputTokens)))
	}

	// Workspace
	wsPart := ""
	if workspace != "" {
		// Shorten workspace to just the last directory component
		shortWs := workspace
		if idx := strings.LastIndex(workspace, "/"); idx >= 0 {
			shortWs = workspace[idx+1:]
		}
		wsPart = sb.theme.StatusDim.Render("workspace: ") +
			sb.theme.StatusAccent.Render(shortWs)
	}

	// Assemble
	parts := []string{statusPart, modelPart}
	if tokenPart != "" {
		parts = append(parts, tokenPart)
	}
	if wsPart != "" {
		parts = append(parts, wsPart)
	}

	content := strings.Join(parts, " "+sep+" ")

	// Pad to fill width
	contentWidth := lipgloss.Width(content)
	if contentWidth < width {
		content += strings.Repeat(" ", width-contentWidth)
	}

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
