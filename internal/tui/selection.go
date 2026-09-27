package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

type screenPoint struct{ x, y int }

// A selection owns a snapshot: streaming continues in the model without moving
// the text under the mouse. Screen coordinates are terminal cells, not runes.
type textSelection struct {
	lines       []string
	anchor, end screenPoint
	dragging    bool
	moved       bool
	toolHit     *toolTarget
}

func (s *textSelection) bounds() (screenPoint, screenPoint) {
	a, b := s.anchor, s.end
	if a.y > b.y || (a.y == b.y && a.x > b.x) {
		a, b = b, a
	}
	return a, b
}

// cellRange expands partial wide/combined characters to whole graphemes.
func cellRange(line string, left, right int) (before, selected, after string) {
	g := uniseg.NewGraphemes(line)
	col, start, end := 0, -1, -1
	for g.Next() {
		from, to := g.Positions()
		width := ansi.StringWidth(g.Str())
		if col < right && col+width > left {
			if start < 0 {
				start = from
			}
			end = to
		}
		col += width
	}
	if start < 0 {
		return line, "", ""
	}
	return line[:start], line[start:end], line[end:]
}

func (s *textSelection) selectedText() string {
	if len(s.lines) == 0 || s.anchor == s.end {
		return ""
	}
	a, b := s.bounds()
	var lines []string
	for y := a.y; y <= b.y; y++ {
		left, right := 0, ansi.StringWidth(s.lines[y])
		if y == a.y {
			left = a.x
		}
		if y == b.y {
			right = b.x + 1
		}
		_, text, _ := cellRange(s.lines[y], left, right)
		lines = append(lines, strings.TrimRight(text, " \t"))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func (s *textSelection) view() string {
	lines := append([]string(nil), s.lines...)
	a, b := s.bounds()
	if a != b {
		for y := a.y; y <= b.y; y++ {
			left, right := 0, ansi.StringWidth(lines[y])
			if y == a.y {
				left = a.x
			}
			if y == b.y {
				right = b.x + 1
			}
			before, selected, after := cellRange(lines[y], left, right)
			// Explicit reverse video works even on terminals without true color.
			lines[y] = before + "\x1b[7m" + selected + "\x1b[0m" + after
		}
	}
	return strings.Join(lines, "\n")
}

func (m *TuiModel) clearSelection() { m.selection = textSelection{} }

func (m *TuiModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		m.clearSelection()
		if m.confirmation.IsActive() {
			return m, nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			if m.completion.IsActive() {
				m.completion.Prev()
			} else {
				m.viewport.ScrollUp(3)
			}
		} else {
			if m.completion.IsActive() {
				m.completion.Next()
			} else {
				m.viewport.ScrollDown(3)
			}
		}
		return m, nil
	}
	if len(m.frame) == 0 {
		return m, nil
	}
	point := screenPoint{max(0, min(msg.X, m.width-1)), max(0, min(msg.Y, len(m.frame)-1))}
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.selection = textSelection{lines: append([]string(nil), m.frame...), anchor: point, end: point, dragging: true}
		if hit, ok := m.frameToolTargets[point.y]; ok && !m.confirmation.IsActive() {
			m.selection.toolHit = &hit
		}
		m.copyNotice = ""
	case msg.Action == tea.MouseActionMotion && m.selection.dragging:
		if point != m.selection.anchor {
			m.selection.moved = true
		}
		m.selection.end = point
	case msg.Action == tea.MouseActionRelease && m.selection.dragging:
		m.selection.end = point
		m.selection.dragging = false
		if text := m.selection.selectedText(); text != "" {
			return m, m.copyText(text)
		}
		hit, clicked := m.selection.toolHit, !m.selection.moved && point == m.selection.anchor
		m.clearSelection()
		if hit != nil && clicked {
			m.toggleTool(*hit, point.y)
		}
	}
	return m, nil
}
