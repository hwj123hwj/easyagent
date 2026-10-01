package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// CompletionPopup renders the autocomplete dropdown as a floating panel.
// It sits just above the input area.
//
//	┌──────────────────────────┐
//	│ /help    Show this help  │ ← highlighted (selected)
//	│ /history  View history   │
//	└──────────────────────────┘
type CompletionPopup struct {
	theme *Theme
}

// NewCompletionPopup creates a new popup renderer.
func NewCompletionPopup() *CompletionPopup {
	return &CompletionPopup{theme: DefaultTheme()}
}

// RenderHeight keeps the selected item visible even when borders will not fit.
func (cp *CompletionPopup) RenderHeight(cm *CompletionState, width, height int) string {
	if !cm.IsActive() || height < 1 {
		return ""
	}
	overhead := 2
	if cm.searchable() {
		overhead++
		if item := cm.SelectedItem(); item != nil && item.Preview != "" {
			overhead += len(pickerPreview(item.Preview, width))
		}
	}
	if len(cm.Items()) == 0 {
		notice := "No " + cm.searchName() + " match: " + terminalText(cm.query) + " · Backspace / Esc"
		if cm.loading {
			notice = "Loading " + cm.searchName() + "… · Esc: cancel"
		}
		return cp.theme.StatusDim.Render(ansi.Truncate(notice, width, "…"))
	}
	if height <= overhead {
		return "\x1b[7m" + ansi.Truncate("› "+terminalText(cm.SelectedItem().Label), width, "…") + "\x1b[0m"
	}
	return cp.Render(cm, width, min(8, height-overhead))
}

// Render produces the popup string from a CompletionState.
func (cp *CompletionPopup) Render(cm *CompletionState, width int, maxRows ...int) string {
	if !cm.IsActive() {
		return ""
	}

	items := cm.Items()
	selected := cm.SelectedIndex()
	rows := 8
	if len(maxRows) > 0 {
		rows = maxInt(1, maxRows[0])
	}
	start := maxInt(0, selected-rows+1)
	end := start + rows
	if end > len(items) {
		end = len(items)
	}

	// Calculate column widths
	maxLabel := 0
	for _, item := range items {
		w := lipgloss.Width(item.Label)
		if w > maxLabel {
			maxLabel = w
		}
	}

	// Build popup lines
	var lines []string
	for i := start; i < end; i++ {
		item := items[i]
		label := terminalText(item.Label)
		desc := terminalText(item.Description)

		// Pad label to align descriptions
		labelWidth := min(maxLabel, max(1, width-2))
		if cm.searchable() {
			labelWidth = min(labelWidth, max(1, (width-2)*2/3))
		}
		label = ansi.Truncate(label, labelWidth, "…")
		padded := label + strings.Repeat(" ", max(0, labelWidth-lipgloss.Width(label))+2)
		padded = ansi.Truncate(padded, max(1, width-2), "")

		// Truncate description to fit width
		availDesc := max(0, width-2-lipgloss.Width(padded))
		desc = ansi.Truncate(desc, availDesc, "…")

		var line string
		if i == selected {
			// Highlighted row — reverse video / accent background
			line = cp.theme.StatusAccent.Render(padded) +
				cp.theme.StatusDim.Render(desc)
			// Full-width highlight
			fullContent := padded + desc
			padW := width - 2 - lipgloss.Width(fullContent)
			if padW > 0 {
				line = line + strings.Repeat(" ", padW)
			}
			line = "\x1b[7m" + line + "\x1b[0m"
		} else {
			line = cp.theme.ToolHeader.Render(padded) +
				cp.theme.StatusDim.Render(desc)
		}

		lines = append(lines, line)
	}

	if cm.searchable() {
		if item := cm.SelectedItem(); item != nil && item.Preview != "" {
			for _, line := range pickerPreview(item.Preview, width) {
				lines = append(lines, cp.theme.StatusDim.Render(line))
			}
		}
		search := "Type to search " + cm.searchName()
		if cm.query != "" {
			search = "Search: " + terminalText(cm.query)
		}
		if cm.loading {
			search = "Opening session…"
		}
		lines = append(lines, ansi.Truncate(fmt.Sprintf("%s · %d/%d · ↑↓ Enter Esc", search, selected+1, len(items)), max(1, width-2), "…"))
	}

	// Wrap in border
	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#087F78", Dark: "#82D5CA"}).
		Padding(0, 0).
		Width(maxInt(1, width-2)).MaxWidth(width)

	content := strings.Join(lines, "\n")
	return popupStyle.Render(content)
}

func pickerPreview(text string, width int) []string {
	width = max(1, width-2)
	lines := strings.Split(ansi.Hardwrap(terminalText(text), width, true), "\n")
	if len(lines) > 2 {
		lines[1] = ansi.Truncate(lines[1], max(0, width-1), "") + "…"
		lines = lines[:2]
	}
	return lines
}

// ConfirmationPopup renders a yes/no confirmation dialog.
//
//	┌─ ⚠️ Confirm ──────────────────────────────────┐
//	│ Run: rm -rf /tmp/cache                         │
//	│                                                │
//	│   [Y] Yes    [N] No    [Esc] Cancel           │
//	└────────────────────────────────────────────────┘
type ConfirmationPopup struct {
	theme *Theme
}

// NewConfirmationPopup creates a new confirmation dialog renderer.
func NewConfirmationPopup() *ConfirmationPopup {
	return &ConfirmationPopup{theme: DefaultTheme()}
}

// Render produces the confirmation dialog.
// `selected` is 0=Yes, 1=No.
func (cp *ConfirmationPopup) Render(description string, selected int, width int) string {
	title := cp.theme.WarnText.Render("⚠️  Confirm")

	// Wrap description to fit width
	descWidth := width - 6
	descLines := wrapText(description, descWidth)

	var bodyLines []string
	bodyLines = append(bodyLines, title)
	bodyLines = append(bodyLines, "")
	for _, dl := range descLines {
		bodyLines = append(bodyLines, cp.theme.ToolBody.Render(dl))
	}
	bodyLines = append(bodyLines, "")

	// Buttons
	yesLabel := "  Y  Yes  "
	noLabel := "  N  No  "
	escLabel := cp.theme.StatusDim.Render("  Esc  Cancel  ")

	if selected == 0 {
		yesLabel = "\x1b[7m" + cp.theme.SuccessText.Render("▶ Y  Yes ") + "\x1b[0m"
		noLabel = cp.theme.StatusDim.Render("  N  No  ")
	} else {
		yesLabel = cp.theme.StatusDim.Render("  Y  Yes  ")
		noLabel = "\x1b[7m" + cp.theme.ErrorText.Render("▶ N  No ") + "\x1b[0m"
	}

	buttons := yesLabel + "   " + noLabel + "   " + escLabel
	bodyLines = append(bodyLines, buttons)

	content := strings.Join(bodyLines, "\n")

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#D29922", Dark: "#D29922"}).
		Padding(0, 1).
		Width(width - 2)

	return dialogStyle.Render(content)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// wrapText wraps text to fit within maxWidth runes per line.
func wrapText(text string, maxWidth int) []string {
	if maxWidth < 10 {
		maxWidth = 10
	}
	var result []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if lipgloss.Width(line)+1+lipgloss.Width(word) <= maxWidth {
				line += " " + word
			} else {
				result = append(result, line)
				line = word
			}
		}
		result = append(result, line)
	}
	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// RenderModelPopup renders the Ctrl+P model selector popup.
func (cp *CompletionPopup) RenderModelPopup(cm *CompletionState, width int) string {
	if !cm.IsActive() || cm.Kind() != CompletionModel {
		return ""
	}
	return cp.Render(cm, width)
}

// RenderConfirmationPopup renders a confirmation dialog overlay.
func RenderConfirmationPopup(desc string, selected int, width int) string {
	cp := NewConfirmationPopup()
	return cp.Render(desc, selected, width)
}

// fmtImportGuard ensures fmt is used (for future debugging).
var _ = fmt.Sprintf
