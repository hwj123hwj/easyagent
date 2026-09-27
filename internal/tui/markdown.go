package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// MarkdownRenderer wraps glamour.TermRenderer for terminal markdown rendering.
// It caches rendered output to avoid re-rendering identical content.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	cache    map[string]string
	mu       sync.Mutex
	style    string
}

var sharedMarkdown *MarkdownRenderer

// NewMarkdownRenderer creates a glamour renderer with auto dark/light detection.
func NewMarkdownRenderer(width int) *MarkdownRenderer {
	if width < 1 {
		width = 1
	}

	style := "light"
	if lipgloss.HasDarkBackground() {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return &MarkdownRenderer{renderer: nil}
	}

	return &MarkdownRenderer{
		renderer: r,
		cache:    make(map[string]string),
		style:    style,
	}
}

// SharedMarkdown returns a lazily-initialized shared markdown renderer.
func SharedMarkdown() *MarkdownRenderer {
	if sharedMarkdown != nil {
		return sharedMarkdown
	}
	sharedMarkdown = NewMarkdownRenderer(80)
	return sharedMarkdown
}

// SetWidth updates the word-wrap width and resets the cache.
func (mr *MarkdownRenderer) SetWidth(width int) {
	if width < 1 {
		width = 1
	}
	mr.mu.Lock()
	defer mr.mu.Unlock()

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(mr.style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return
	}
	mr.renderer = r
	mr.cache = make(map[string]string)
}

// Render converts markdown text to a glamour-rendered string.
// Results are cached by content key.
func (mr *MarkdownRenderer) Render(markdown string) string {
	if mr == nil || mr.renderer == nil {
		return markdown // fallback to raw text
	}

	mr.mu.Lock()
	defer mr.mu.Unlock()

	// Check cache
	if cached, ok := mr.cache[markdown]; ok {
		return cached
	}

	rendered, err := mr.renderer.Render(markdown)
	if err != nil {
		return markdown
	}

	// Trim trailing whitespace per line for compact display
	rendered = strings.TrimSpace(rendered)

	// Cache (limit to 200 entries to avoid memory growth)
	if len(mr.cache) < 200 {
		mr.cache[markdown] = rendered
	}

	return rendered
}

// RenderInline renders short inline markdown (no block elements).
// Falls back to a simple bold/renderer if glamour fails.
func (mr *MarkdownRenderer) RenderInline(text string) string {
	// For short text without newlines, just render normally
	if !strings.Contains(text, "\n") && len(text) < 200 {
		return text
	}
	return mr.Render(text)
}

// PlainText strips markdown formatting and returns plain text.
// Used for system messages where formatting is not needed.
func PlainText(s string) string {
	// Simple approach: strip common markdown markers
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "~~", "")
	return s
}

// Truncate truncates a string to maxLines, appending an ellipsis if truncated.
func Truncate(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[:maxLines], "\n") + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#8B949E")).Render("…")
}
