package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func sameTool(a, b toolTarget) bool { return a.message == b.message && a.tool == b.tool }

func (m *TuiModel) toolRows() []int {
	rows := make([]int, 0, len(m.viewport.toolTargets))
	for row := range m.viewport.toolTargets {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	return rows
}

func (m *TuiModel) openToolFocus() {
	rows := m.toolRows()
	if len(rows) == 0 {
		m.copyNotice = "No tools yet"
		return
	}
	// Start with the most recent group; expanding exposes its individual tools.
	row := rows[len(rows)-1]
	for i := len(rows) - 1; i >= 0; i-- {
		if m.viewport.toolTargets[rows[i]].tool == -1 {
			row = rows[i]
			break
		}
	}
	m.toolFocus = true
	m.focusedTool = m.viewport.toolTargets[row]
	m.revealTool(row)
}

func (m *TuiModel) revealTool(row int) {
	v := &m.viewport
	if row < v.scrollOffset {
		v.scrollOffset = row
	}
	if row >= v.scrollOffset+v.height {
		v.scrollOffset = max(0, row-v.height+1)
	}
	v.userScrolled = true
}

func (m *TuiModel) handleToolFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlT:
		m.toolFocus = false
	case tea.KeyCtrlC, tea.KeyCtrlD:
		m.toolFocus = false
		return m.handleInputKey(msg)
	case tea.KeyPgUp:
		m.viewport.ScrollUp(m.viewport.height)
	case tea.KeyPgDown:
		m.viewport.ScrollDown(m.viewport.height)
	case tea.KeyUp, tea.KeyDown, tea.KeyEnter:
		rows := m.toolRows()
		for i, row := range rows {
			target := m.viewport.toolTargets[row]
			if !sameTool(target, m.focusedTool) {
				continue
			}
			if msg.Type == tea.KeyEnter {
				m.toggleTool(target, max(0, row-m.viewport.scrollOffset))
				return m, nil
			}
			if msg.Type == tea.KeyUp {
				i = max(0, i-1)
			} else {
				i = min(len(rows)-1, i+1)
			}
			m.focusedTool = m.viewport.toolTargets[rows[i]]
			m.revealTool(rows[i])
			return m, nil
		}
		m.openToolFocus() // automatic collapse can hide a focused child
	}
	return m, nil
}

func (m *TuiModel) focusedViewport() string {
	view := m.viewport.View()
	if !m.toolFocus {
		return view
	}
	lines := strings.Split(view, "\n")
	for row, target := range m.viewport.toolTargets {
		screenRow := row - m.viewport.scrollOffset
		if sameTool(target, m.focusedTool) && screenRow >= 0 && screenRow < len(lines) {
			lines[screenRow] = "\x1b[7m" + ansi.Strip(lines[screenRow]) + "\x1b[0m"
			break
		}
	}
	return strings.Join(lines, "\n")
}

func (m *TuiModel) copyFocusedTool() tea.Cmd {
	target := m.focusedTool
	if target.message < 0 || target.message >= len(m.messages) {
		return nil
	}
	msg := m.messages[target.message]
	tools := msg.Tools
	if target.tool >= 0 {
		if target.tool >= len(tools) {
			return nil
		}
		tools = tools[target.tool : target.tool+1]
	}
	var parts []string
	for _, tool := range tools {
		parts = append(parts, tool.Name+"\n"+tool.Args+"\n\n"+tool.Result)
	}
	return m.copyText(strings.Join(parts, "\n\n"))
}
