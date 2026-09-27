package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

func toolTestModel() *TuiModel {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.messages = []ChatMessage{{Role: "user", Content: "inspect"}}
	m.viewport.SetMessages(m.messages)
	return m
}

func clickToolHeader(t *testing.T, m *TuiModel, message, tool int) int {
	t.Helper()
	m.View()
	for y, hit := range m.frameToolTargets {
		if hit.message == message && hit.tool == tool {
			m.Update(tea.MouseMsg{X: 2, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m.View()
			m.Update(tea.MouseMsg{X: 2, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
			return y
		}
	}
	t.Fatalf("header (%d,%d) not visible in %q", message, tool, m.View())
	return -1
}

func TestToolGroupsAutoCollapseAndHistoricalClick(t *testing.T) {
	m := toolTestModel()
	for i := 0; i < 6; i++ {
		id := fmt.Sprint(i)
		m.Update(ToolStartMsg{ID: id, Name: "bash", Args: "command " + id})
		m.Update(ToolEndMsg{ID: id, Name: "bash", Result: "result " + id})
	}
	if !m.messages[0].ToolsExpanded {
		t.Fatal("running turn should show command list")
	}
	m.Update(StreamTextMsg{Delta: "final answer"})
	m.Update(StreamDoneMsg{})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "6 commands") || strings.Contains(view, "command 0") {
		t.Fatalf("completed group not compact: %s", view)
	}
	clickToolHeader(t, m, 0, -1)
	if !strings.Contains(ansi.Strip(m.View()), "command 0") {
		t.Fatal("historical group did not expand after answer")
	}
	clickToolHeader(t, m, 0, 0)
	if !strings.Contains(ansi.Strip(m.View()), "result 0") {
		t.Fatal("historical command did not expand")
	}
	clickToolHeader(t, m, 0, -1)
	if strings.Contains(ansi.Strip(m.View()), "command 0") {
		t.Fatal("group did not collapse")
	}
	// Keyboard fallback must find the group even when the last message is a reply.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !m.messages[0].ToolsExpanded {
		t.Fatal("Ctrl+O failed after final answer")
	}
}

func TestManualChoiceAndErrorRemainVisible(t *testing.T) {
	m := toolTestModel()
	m.Update(ToolStartMsg{ID: "a", Name: "bash"})
	clickToolHeader(t, m, 0, 0)
	m.Update(ToolEndMsg{ID: "a", Name: "bash", Result: "output"})
	m.Update(StreamDoneMsg{})
	if !m.messages[0].ToolsExpanded || m.messages[0].Tools[0].Collapsed {
		t.Fatal("auto collapse overrode manual expansion")
	}
	clickToolHeader(t, m, 0, -1)
	m.Update(ToolStartMsg{ID: "b", Name: "bash"})
	if m.messages[0].ToolsExpanded {
		t.Fatal("new tool overrode manual group collapse")
	}
	n := toolTestModel()
	n.Update(ToolStartMsg{ID: "error", Name: "bash"})
	n.Update(ToolEndMsg{ID: "error", Name: "bash", Result: "permission denied", IsError: true})
	n.Update(StreamDoneMsg{})
	if !n.messages[0].ToolsExpanded || n.messages[0].Tools[0].Collapsed {
		t.Fatal("error hidden by auto collapse")
	}
}

func TestExpandedOutputIsCompleteAndAnchored(t *testing.T) {
	m := toolTestModel()
	output := strings.Repeat("line 中文 output\n", 80) + "TAIL_MARKER"
	args := "command " + strings.Repeat("long 中文 ", 30)
	m.Update(ToolStartMsg{ID: "a", Name: "bash", Args: args})
	m.Update(ToolEndMsg{ID: "a", Name: "bash", Result: output})
	m.Update(StreamDoneMsg{})
	clickToolHeader(t, m, 0, -1)
	row := clickToolHeader(t, m, 0, 0)
	m.View()
	if hit, ok := m.frameToolTargets[row]; !ok || hit.tool != 0 {
		t.Fatal("expansion jumped away from clicked header")
	}
	all := ansi.Strip(strings.Join(m.viewport.lines, "\n"))
	if !strings.Contains(all, "TAIL_MARKER") || strings.Contains(all, "more lines)") {
		t.Fatal("expanded output truncated")
	}
	for _, line := range m.viewport.lines {
		if ansi.StringWidth(line) > 80 {
			t.Fatalf("line overflow: %q", line)
		}
	}
	end := m.messages[0].Tools[0].EndTime
	if end.IsZero() {
		t.Fatal("duration not frozen at completion")
	}
	m.viewport.ScrollDown(1000)
	if !strings.Contains(ansi.Strip(m.View()), "TAIL_MARKER") {
		t.Fatal("output tail not scrollable")
	}
}

func TestDragOnToolHeaderCopiesWithoutToggling(t *testing.T) {
	m := toolTestModel()
	m.Update(ToolStartMsg{ID: "a", Name: "bash"})
	m.Update(ToolEndMsg{ID: "a", Name: "bash"})
	m.Update(StreamDoneMsg{})
	m.View()
	y := -1
	for row, hit := range m.frameToolTargets {
		if hit.tool == -1 {
			y = row
		}
	}
	if y < 0 {
		t.Fatal("missing group header")
	}
	copied := ""
	m.clipboardWriter = func(s string) error { copied = s; return nil }
	m.Update(tea.MouseMsg{X: 0, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 12, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	_, cmd := m.Update(tea.MouseMsg{X: 12, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if cmd == nil {
		t.Fatal("drag did not copy")
	}
	m.Update(cmd())
	if copied == "" || m.messages[0].ToolsExpanded {
		t.Fatal("drag toggled group instead of copying")
	}
}

func TestCollapsedToolHeaderFitsNarrowWidth(t *testing.T) {
	tool := ToolCallInfo{Name: "bash", Args: strings.Repeat("中文", 40), Collapsed: true, StartTime: time.Unix(10, 0), EndTime: time.Unix(12, 0)}
	for _, width := range []int{20, 40, 99} {
		lines := NewToolPanel(tool, width).Render()
		if len(lines) != 1 || ansi.StringWidth(lines[0]) > width {
			t.Fatalf("width=%d header=%q", width, lines)
		}
	}
}

func TestToolClickAfterResizeAndScroll(t *testing.T) {
	m := toolTestModel()
	m.messages = []ChatMessage{
		{Role: "user", Content: strings.Repeat("earlier 中文 line\n", 20)},
		{Role: "user", Content: "tool turn", Tools: []ToolCallInfo{{Name: "bash", Args: "old command", Result: "old output", Collapsed: true}}},
		{Role: "assistant", Content: strings.Repeat("later answer\n", 20)},
	}
	m.viewport.SetMessages(m.messages)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	header := -1
	for row, hit := range m.viewport.toolTargets {
		if hit.message == 1 && hit.tool == -1 {
			header = row
		}
	}
	if header < 0 {
		t.Fatal("missing historical header")
	}
	m.viewport.userScrolled = true
	m.viewport.scrollOffset = header - 2
	clickToolHeader(t, m, 1, -1)
	clickToolHeader(t, m, 1, 0)
	if m.messages[1].Tools[0].Collapsed {
		t.Fatal("scrolled click did not open command")
	}
	m.viewport.ScrollDown(2)
	if !strings.Contains(ansi.Strip(m.View()), "old output") {
		t.Fatal("scrolled hit map targeted wrong command")
	}
}

func TestToolsStayGroupedAcrossProgressMessages(t *testing.T) {
	m := toolTestModel()
	m.Update(ToolStartMsg{ID: "first", Name: "bash"})
	m.Update(ToolEndMsg{ID: "first", Name: "bash", Result: "first output"})
	m.Update(CompactionMsg{Summary: "context compacted"})
	m.Update(ToolStartMsg{ID: "second", Name: "read"})
	if len(m.messages[0].Tools) != 2 || len(m.messages[1].Tools) != 0 {
		t.Fatal("status message split one turn into different groups")
	}
	if !strings.Contains(ansi.Strip(m.View()), "2 commands") {
		t.Fatal("group cache not updated")
	}
}
