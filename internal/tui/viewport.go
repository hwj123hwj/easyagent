package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MessageViewport is a scrollable viewport that renders conversation messages
// with full styling: Markdown rendering, tool panels, diff highlighting.
type MessageViewport struct {
	width               int
	height              int
	messages            []ChatMessage
	streaming           string   // text being streamed (not yet finalized)
	lines               []string // rendered lines (cached)
	cachedLines         []string // rendered message lines (without streaming) — cached for incremental updates
	scrollOffset        int
	userScrolled        bool // true if user manually scrolled up
	newLinesSinceScroll int  // lines added since user scrolled up
	theme               *Theme
	md                  *MarkdownRenderer
}

// NewMessageViewport creates a new viewport.
func NewMessageViewport(width, height int) MessageViewport {
	return MessageViewport{
		width:  width,
		height: height,
		theme:  DefaultTheme(),
		md:     SharedMarkdown(),
	}
}

// Resize updates the viewport dimensions and markdown renderer width.
func (v *MessageViewport) Resize(width, height int) {
	v.width = width
	v.height = height
	if v.md != nil {
		v.md.SetWidth(width - 2) // account for left padding
	}
	v.rebuildLines()
}

// SetMessages updates the message list and rebuilds the view.
func (v *MessageViewport) SetMessages(msgs []ChatMessage) {
	v.messages = msgs
	// Invalidate message cache — messages changed
	v.cachedLines = nil
	v.rebuildLines()
}

// SetStreaming sets the current streaming text (shown live as agent types).
// This uses incremental rendering: only re-renders the streaming portion,
// not the cached message lines.
func (v *MessageViewport) SetStreaming(text string) {
	v.streaming = text
	v.rebuildLines()
}

// Clear resets the viewport.
func (v *MessageViewport) Clear() {
	v.messages = nil
	v.streaming = ""
	v.lines = nil
	v.cachedLines = nil
	v.scrollOffset = 0
	v.userScrolled = false
}

// ScrollUp moves the viewport up by n lines.
func (v *MessageViewport) ScrollUp(n int) {
	v.scrollOffset -= n
	if v.scrollOffset < 0 {
		v.scrollOffset = 0
	}
	v.userScrolled = true
}

// ScrollDown moves the viewport down by n lines.
func (v *MessageViewport) ScrollDown(n int) {
	maxScroll := len(v.lines) - v.height
	v.scrollOffset += n
	if v.scrollOffset > maxScroll {
		v.scrollOffset = maxScroll
	}
	if v.scrollOffset < 0 {
		v.scrollOffset = 0
	}
	// If user scrolled to bottom, clear the flag
	if v.scrollOffset >= maxScroll {
		v.userScrolled = false
		v.newLinesSinceScroll = 0
	}
}

// GotoBottom scrolls to the bottom of the messages.
func (v *MessageViewport) GotoBottom() {
	maxScroll := len(v.lines) - v.height
	if maxScroll > 0 {
		v.scrollOffset = maxScroll
	} else {
		v.scrollOffset = 0
	}
	v.userScrolled = false
	v.newLinesSinceScroll = 0
}

// NewLinesCount returns how many new lines appeared since user scrolled up.
func (v *MessageViewport) NewLinesCount() int {
	if !v.userScrolled {
		return 0
	}
	return len(v.lines) - v.scrollOffset - v.height
}

// View renders the visible portion of the viewport.
func (v *MessageViewport) View() string {
	if len(v.lines) == 0 {
		// Empty state — show a subtle hint
		hint := v.theme.HelpText.Render("  Type a message and press Enter to start chatting...")
		padLines := v.height - 1
		if padLines < 0 {
			padLines = 0
		}
		return hint + strings.Repeat("\n", padLines)
	}

	start := v.scrollOffset
	end := start + v.height
	if end > len(v.lines) {
		end = len(v.lines)
	}
	if start > end {
		start = end
	}

	// CRITICAL: copy the slice — v.lines[start:end] shares the underlying
	// array, so modifying `visible` would corrupt v.lines permanently.
	// This was causing scroll indicators to bleed into message content.
	tmp := v.lines[start:end]
	visible := make([]string, len(tmp))
	copy(visible, tmp)

	// Pad to fill height
	for len(visible) < v.height {
		visible = append(visible, "")
	}

	// Add scroll indicator if user has scrolled up
	if v.userScrolled && v.NewLinesCount() > 0 {
		newLines := v.NewLinesCount()
		indicator := v.theme.StatusDim.Render(fmt.Sprintf(" ↑ %d new (scroll down to view) ", newLines))
		if len(visible) > 0 {
			visible[len(visible)-1] = indicator
		}
	} else if v.userScrolled {
		totalLines := len(v.lines)
		percent := int(float64(v.scrollOffset) / float64(totalLines) * 100)
		if percent < 0 {
			percent = 0
		}
		indicator := v.theme.StatusDim.Render(fmt.Sprintf(" ↑ %d%% (scroll for more) ", percent))
		if len(visible) > 0 {
			visible[len(visible)-1] = indicator
		}
	}

	return strings.Join(visible, "\n")
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func (v *MessageViewport) rebuildLines() {
	// Cache message lines if not yet cached
	if v.cachedLines == nil {
		v.cachedLines = nil
		for _, msg := range v.messages {
			v.cachedLines = append(v.cachedLines, v.renderMessage(msg)...)
		}
	}

	// Combine cached message lines + streaming lines
	var lines []string
	lines = append(lines, v.cachedLines...)

	if v.streaming != "" {
		lines = append(lines, v.renderStreaming(v.streaming)...)
	}

	v.lines = lines

	// Smart auto-scroll: only jump to bottom if user hasn't scrolled up
	if !v.userScrolled {
		v.GotoBottom()
	}
}

func (v *MessageViewport) renderMessage(msg ChatMessage) []string {
	var lines []string

	// Role label with timestamp
	var label string
	var labelStyle lipgloss.Style
	switch msg.Role {
	case "user":
		label = "You"
		labelStyle = v.theme.UserLabel
	case "assistant":
		label = "EA"
		labelStyle = v.theme.AssistantLabel
	case "system":
		label = "system"
		labelStyle = v.theme.SystemLabel
	default:
		label = msg.Role
		labelStyle = v.theme.SystemLabel
	}

	ts := v.theme.Timestamp.Render(msg.Timestamp.Format("15:04"))

	// Header line: "You 14:23"
	headerLine := fmt.Sprintf("%s %s",
		labelStyle.Render(label),
		ts,
	)
	lines = append(lines, headerLine)

	// Content rendering
	switch msg.Role {
	case "user":
		// User content: plain text with indentation, no markdown rendering
		for _, line := range strings.Split(msg.Content, "\n") {
			// Bugfix: 长行（长句、URL、中英混排）之前不做软换行，超出终端宽度被直接截断。
			// 这里按可视宽度换行（lipgloss 按 cell 宽计算，CJK 占 2 格），保留两格缩进。
			for _, wl := range wrapVisual(line, v.width-2) {
				lines = append(lines, "  "+wl)
			}
		}
	case "assistant":
		// Assistant content: full Markdown rendering via glamour
		rendered := v.md.Render(msg.Content)
		for _, line := range strings.Split(rendered, "\n") {
			lines = append(lines, line)
		}
	case "system":
		// System content: italic dim text
		for _, line := range strings.Split(msg.Content, "\n") {
			for _, wl := range wrapVisual(line, v.width-2) {
				lines = append(lines, v.theme.SystemContent.Render("  "+wl))
			}
		}
	}

	// Tool calls
	for _, tool := range msg.Tools {
		panel := NewToolPanel(tool, v.width-2) // account for left padding
		lines = append(lines, panel.Render()...)
	}

	lines = append(lines, "") // blank line separator

	return lines
}

func (v *MessageViewport) renderStreaming(text string) []string {
	var lines []string

	lines = append(lines, fmt.Sprintf("%s %s",
		v.theme.AssistantLabel.Render("EA"),
		v.theme.StatusDim.Render("typing…"),
	))

	// Render streaming text as plain text (no markdown until done —
	// glamour is too slow for per-keystroke streaming)
	for _, line := range strings.Split(text, "\n") {
		// Bugfix: 流式输出同样按可视宽度软换行，避免长行被终端截断。
		for _, wl := range wrapVisual(line, v.width-2) {
			lines = append(lines, "  "+wl)
		}
	}
	return lines
}

// wrapVisual 把一行文本按终端可视宽度软换行（ANSI 感知、CJK 宽度感知）。
// width <= 0 时原样返回，避免尺寸未初始化时丢内容。
func wrapVisual(line string, width int) []string {
	if width <= 0 || lipgloss.Width(line) <= width {
		return []string{line}
	}
	wrapped := lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
	return strings.Split(wrapped, "\n")
}
