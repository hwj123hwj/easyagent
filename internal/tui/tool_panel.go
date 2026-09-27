package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ToolPanel renders a clickable command header and its optional full details.
type ToolPanel struct {
	info  ToolCallInfo
	width int
	theme *Theme
	md    *MarkdownRenderer
}

// NewToolPanel creates a panel for a tool call.
func NewToolPanel(info ToolCallInfo, width int) *ToolPanel {
	info.Name = terminalText(info.Name)
	info.Args = terminalText(info.Args)
	info.Result = terminalText(info.Result)
	return &ToolPanel{
		info:  info,
		width: width,
		theme: DefaultTheme(),
		md:    SharedMarkdown(),
	}
}

// Render uses one compact header per command. Expanded output is never cut at
// an arbitrary line count; the conversation viewport handles scrolling.
func (tp *ToolPanel) Render() []string {
	arrow, status := "▸", "✓"
	if !tp.info.Collapsed {
		arrow = "▾"
	}
	if tp.info.Streaming {
		status = "●"
	} else if tp.info.IsError {
		status = "✗"
	}
	duration := ""
	if !tp.info.EndTime.IsZero() && !tp.info.StartTime.IsZero() {
		duration = " · " + formatDuration(tp.info.EndTime.Sub(tp.info.StartTime))
	}
	summary := strings.Join(strings.Fields(formatToolArgs(tp.info.Args)), " ")
	header := fmt.Sprintf("  %s %s %s%s  %s", arrow, status, tp.info.Name, duration, summary)
	style := tp.theme.ToolHeader
	if tp.info.IsError {
		style = tp.theme.ErrorText
	}
	lines := []string{style.Render(ansi.Truncate(header, max(1, tp.width), "…"))}
	if tp.info.Collapsed {
		return lines
	}
	appendText := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			for _, segment := range strings.Split(ansi.Hardwrap(line, max(1, tp.width-4), true), "\n") {
				lines = append(lines, "    "+segment)
			}
		}
	}
	appendText("Arguments:")
	appendText(tp.info.Args)
	appendText("Output:")
	if tp.info.Result != "" {
		result := tp.info.Result
		if isEditTool(tp.info.Name) {
			result = strings.Join(RenderDiff(result, tp.theme), "\n")
		}
		appendText(result)
	} else if tp.info.Streaming {
		appendText("Waiting for output…")
	} else {
		appendText("(no output)")
	}
	return lines
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func isEditTool(name string) bool {
	return strings.Contains(name, "edit") || strings.Contains(name, "replace") ||
		strings.Contains(name, "write") || strings.Contains(name, "patch")
}

// formatToolArgs parses the raw args string (which might be JSON, Go fmt "%v", or <nil>)
// and returns a compact human-readable display string.
func formatToolArgs(raw string) string {
	if raw == "" || raw == "<nil>" {
		return ""
	}

	// Try to parse as JSON object
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &m); err == nil {
			return formatJSONArgs(m)
		}
	}

	// Fallback: just show the raw string
	return raw
}

// formatJSONArgs converts a JSON object to a compact "key: value" display.
func formatJSONArgs(m map[string]interface{}) string {
	var parts []string
	for k, v := range m {
		switch val := v.(type) {
		case string:
			s := val
			parts = append(parts, k+": "+strconv.Quote(s))
		default:
			parts = append(parts, k+": "+fmt.Sprintf("%v", v))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// formatDuration renders a duration as a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ToggleCollapsed toggles the collapsed state of a tool panel.
func (tp *ToolPanel) ToggleCollapsed() {
	tp.info.Collapsed = !tp.info.Collapsed
}
