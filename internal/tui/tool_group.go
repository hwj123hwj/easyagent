package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// toolTarget identifies a header in the rendered frame, including the state the
// user actually clicked. Tool indices remain stable while results stream in.
type toolTarget struct {
	message, tool int
	expanded      bool
}

func (v *MessageViewport) toolGroupHeader(msg ChatMessage) string {
	running, failed, done := 0, 0, 0
	for _, tool := range msg.Tools {
		if tool.Streaming {
			running++
		} else if tool.IsError {
			failed++
		} else {
			done++
		}
	}
	parts := []string{fmt.Sprintf("%d commands", len(msg.Tools))}
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", running))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d done", done))
	}
	arrow, action := "▸", "expand"
	if msg.ToolsExpanded {
		arrow, action = "▾", "collapse"
	}
	text := arrow + " Tools · " + strings.Join(parts, " · ") + " · click to " + action
	style := v.theme.StatusDim
	if failed > 0 {
		style = v.theme.ErrorText
	}
	return style.Render(ansi.Truncate(text, max(1, v.width), "…"))
}

// invalidateFrom includes historical messages, which can change when a tool
// finishes late or the user expands an older group after the final reply.
func (v *MessageViewport) invalidateFrom(index int) {
	v.cachedCount = min(v.cachedCount, index+1)
}

func (m *TuiModel) toggleTool(target toolTarget, screenRow int) {
	if target.message < 0 || target.message >= len(m.messages) {
		return
	}
	msg := &m.messages[target.message]
	if len(msg.Tools) == 0 {
		return
	}
	if target.tool < 0 {
		msg.ToolsExpanded = !target.expanded
		msg.ToolsManual = true
	} else {
		if target.tool >= len(msg.Tools) {
			return
		}
		msg.ToolsExpanded = true
		msg.ToolsManual = true
		msg.Tools[target.tool].Collapsed = target.expanded
		msg.Tools[target.tool].Manual = true
	}
	m.viewport.invalidateFrom(target.message)
	m.viewport.userScrolled = true // do not jump to the end of expanded output
	m.viewport.SetMessages(m.messages)
	// Keep the clicked header at the same screen row as its body opens/closes.
	for row, hit := range m.viewport.toolTargets {
		if hit.message == target.message && hit.tool == target.tool {
			m.viewport.scrollOffset = max(0, min(row-screenRow, max(0, len(m.viewport.lines)-m.viewport.height)))
			break
		}
	}
}

func (m *TuiModel) toggleLatestToolGroup() {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if len(m.messages[i].Tools) == 0 {
			continue
		}
		screenRow := 0
		for row, hit := range m.viewport.toolTargets {
			if hit.message == i && hit.tool == -1 {
				screenRow = max(0, min(row-m.viewport.scrollOffset, m.viewport.height-2))
				break
			}
		}
		m.toggleTool(toolTarget{message: i, tool: -1, expanded: m.messages[i].ToolsExpanded}, screenRow)
		return
	}
}

// Complete successful groups collapse automatically. Explicit user choices and
// failed commands stay open, so finishing a run never hides what is being read.
func (m *TuiModel) finishToolGroups(interrupted bool) {
	for i := range m.messages {
		msg := &m.messages[i]
		if len(msg.Tools) == 0 {
			continue
		}
		failed := false
		for j := range msg.Tools {
			tool := &msg.Tools[j]
			if tool.Streaming && interrupted {
				tool.Streaming = false
				tool.IsError = true
				tool.EndTime = time.Now()
				tool.Result += "\n[interrupted]"
				if !tool.Manual {
					tool.Collapsed = false
				}
			}
			failed = failed || tool.IsError
		}
		if !msg.ToolsManual {
			msg.ToolsExpanded = failed
		}
		m.viewport.invalidateFrom(i)
	}
}
