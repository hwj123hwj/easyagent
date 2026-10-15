package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hwj123hwj/easyagent/sdk/tools"
)

// QuestionDialogMsg opens the interactive ask_user_question overlay.
type QuestionDialogMsg struct {
	ToolCallID string
	Questions  []tools.AskUserQuestion
	Result     chan QuestionResultMsg
}

// QuestionResultMsg carries the submitted answers (or skip) from the overlay.
type QuestionResultMsg struct {
	ToolCallID string
	Submitted  bool
	Answers    []tools.AskUserAnswer
}

type questionCancelledMsg struct{ result chan QuestionResultMsg }

// questionRow is a focusable line inside the question overlay.
type questionRow struct {
	kind      rowKind
	question  int // index into QuestionState.questions
	option    int // index into question.Options (kindRowOption only)
}

type rowKind uint8

const (
	rowQuestionTitle rowKind = iota
	rowOption
	rowCustom
)

// QuestionState drives the interactive ask_user_question overlay. It mirrors
// the desktop QuestionPrompt: per-question option selection (single/multi),
// an optional custom-text answer, submit and skip.
type QuestionState struct {
	active    bool
	questions []tools.AskUserQuestion
	selected  [][]bool // per question, which options are checked
	custom    []string // per question, custom text (empty = unused unless customFocus)
	useCustom []bool   // per question, whether the custom answer is enabled
	rows      []questionRow
	focus     int // index into rows
}

// NewQuestionState creates an inactive question state.
func NewQuestionState() *QuestionState {
	return &QuestionState{}
}

// IsActive reports whether the question overlay is showing.
func (qs *QuestionState) IsActive() bool { return qs.active }

// Show activates the overlay with the validated questions.
func (qs *QuestionState) Show(questions []tools.AskUserQuestion) {
	qs.active = true
	qs.questions = questions
	qs.selected = make([][]bool, len(questions))
	qs.custom = make([]string, len(questions))
	qs.useCustom = make([]bool, len(questions))
	qs.rows = qs.rows[:0]
	for i, q := range questions {
		qs.selected[i] = make([]bool, len(q.Options))
		qs.rows = append(qs.rows, questionRow{kind: rowQuestionTitle, question: i})
		for j := range q.Options {
			qs.rows = append(qs.rows, questionRow{kind: rowOption, question: i, option: j})
		}
		qs.rows = append(qs.rows, questionRow{kind: rowCustom, question: i})
	}
	qs.focus = 0
}

// Hide deactivates the overlay.
func (qs *QuestionState) Hide() { qs.active = false }

// complete reports whether every question has a valid answer set.
func (qs *QuestionState) complete() bool {
	for i, q := range qs.questions {
		count := 0
		for _, on := range qs.selected[i] {
			if on {
				count++
			}
		}
		if text := strings.TrimSpace(qs.custom[i]); qs.useCustom[i] && text != "" {
			count++
		}
		if count == 0 || (!q.MultiSelect && count != 1) {
			return false
		}
	}
	return true
}

// Answers converts the current selection into the wire format expected by
// ValidateAskUserAnswers.
func (qs *QuestionState) Answers() []tools.AskUserAnswer {
	out := make([]tools.AskUserAnswer, len(qs.questions))
	for i := range qs.questions {
		for j, on := range qs.selected[i] {
			if on {
				out[i].Selected = append(out[i].Selected, qs.questions[i].Options[j].Label)
			}
		}
		if qs.useCustom[i] {
			out[i].Text = strings.TrimSpace(qs.custom[i])
		}
	}
	return out
}

// HandleKey processes input while the overlay is visible. Returns
// (done, submitted): submitted=true means answers are ready; done=true with
// submitted=false means the user skipped.
func (qs *QuestionState) HandleKey(msg tea.KeyMsg) (done, submitted bool) {
	if !qs.active {
		return false, false
	}
	switch msg.Type {
	case tea.KeyUp:
		qs.moveFocus(-1)
		return false, false
	case tea.KeyDown:
		qs.moveFocus(1)
		return false, false
	case tea.KeyEnter, tea.KeySpace:
		qs.toggleFocused()
		return false, false
	case tea.KeyEsc:
		return true, false
	case tea.KeyTab:
		if qs.complete() {
			return true, true
		}
		return false, false
	case tea.KeyBackspace:
		qs.editFocusedCustom(true)
		return false, false
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			switch msg.Runes[0] {
			case 'y', 'Y':
				if qs.complete() {
					return true, true
				}
				return false, false
			case 'n', 'N':
				return true, false
			default:
				qs.typeFocusedCustom(string(msg.Runes))
			}
		}
		return false, false
	}
}

func (qs *QuestionState) moveFocus(delta int) {
	if len(qs.rows) == 0 {
		return
	}
	qs.focus = (qs.focus + delta + len(qs.rows)) % len(qs.rows)
}

func (qs *QuestionState) toggleFocused() {
	row := qs.rows[qs.focus]
	switch row.kind {
	case rowOption:
		qs.selected[row.question][row.option] = !qs.selected[row.question][row.option]
		if !qs.questions[row.question].MultiSelect && qs.selected[row.question][row.option] {
			// Radio behaviour: picking one clears the custom toggle and siblings.
			qs.useCustom[row.question] = false
			for j := range qs.selected[row.question] {
				qs.selected[row.question][j] = j == row.option
			}
		}
	case rowCustom:
		qs.useCustom[row.question] = !qs.useCustom[row.question]
		if qs.useCustom[row.question] && !qs.questions[row.question].MultiSelect {
			for j := range qs.selected[row.question] {
				qs.selected[row.question][j] = false
			}
		}
	}
}

func (qs *QuestionState) typeFocusedCustom(text string) {
	row := qs.rows[qs.focus]
	if row.kind != rowCustom {
		return
	}
	qs.useCustom[row.question] = true
	qs.custom[row.question] += text
	if !qs.questions[row.question].MultiSelect {
		for j := range qs.selected[row.question] {
			qs.selected[row.question][j] = false
		}
	}
}

func (qs *QuestionState) editFocusedCustom(backspace bool) {
	row := qs.rows[qs.focus]
	if row.kind != rowCustom || !backspace {
		return
	}
	runes := []rune(qs.custom[row.question])
	if len(runes) > 0 {
		qs.custom[row.question] = string(runes[:len(runes)-1])
	}
}

// focusedRowText returns the plain (unstyled) text of the focused row, used
// for ansi-safe re-styling.
func (qs *QuestionState) focusedCustomText(q int) string {
	if !qs.useCustom[q] && qs.custom[q] == "" {
		return ""
	}
	return qs.custom[q]
}

// Render produces the question overlay panel.
func (qs *QuestionState) Render(theme *Theme, width int) string {
	if !qs.active {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.WarnText.Render("❓ 请回答以下问题"))
	b.WriteString("\n\n")
	for ri, row := range qs.rows {
		cursor := "  "
		if ri == qs.focus {
			cursor = "▸ "
		}
		switch row.kind {
		case rowQuestionTitle:
			q := qs.questions[row.question]
			head := q.Question
			if q.MultiSelect {
				head += " （可多选）"
			}
			b.WriteString(theme.ToolBody.Render(cursor + head))
			b.WriteString("\n")
		case rowOption:
			q := qs.questions[row.question]
			opt := q.Options[row.option]
			mark := "[ ]"
			if qs.selected[row.question][row.option] {
				mark = "[x]"
			}
			line := fmt.Sprintf("%s%s %s", cursor, mark, opt.Label)
			if opt.Description != "" {
				line += " — " + opt.Description
			}
			if ri == qs.focus {
				b.WriteString(theme.SuccessText.Render(line))
			} else {
				b.WriteString(theme.ToolBody.Render(line))
			}
			b.WriteString("\n")
		case rowCustom:
			prefix := "[ ] 其他（自定义输入）"
			if qs.useCustom[row.question] {
				prefix = "[x] 其他（自定义输入）"
			}
			line := cursor + prefix
			if t := qs.focusedCustomText(row.question); t != "" {
				line += ": " + t
			} else if ri == qs.focus {
				line += "（直接输入文字，Backspace 删除）"
			}
			if ri == qs.focus {
				b.WriteString(theme.SuccessText.Render(line))
			} else {
				b.WriteString(theme.StatusDim.Render(line))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	status := "Tab/Y 提交"
	if !qs.complete() {
		status = "还有问题未回答"
	}
	footer := fmt.Sprintf("  ↑↓ 选择 · Space/Enter 勾选 · Esc 跳过 · %s", status)
	b.WriteString(theme.HelpText.Render(footer))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#58A6FF", Dark: "#58A6FF"}).
		Padding(0, 1).
		Width(width - 2)
	return panel.Render(b.String())
}
