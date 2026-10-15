package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hwj123hwj/easyagent/sdk/tools"
)

func sampleQuestions() []tools.AskUserQuestion {
	return []tools.AskUserQuestion{
		{
			Question: "采用哪种接入方式?",
			Header:   "接入",
			Options: []tools.AskUserOption{
				{Label: "官方 API", Description: "使用云服务"},
				{Label: "自部署", Description: "本地部署"},
			},
		},
		{
			Question:   "启用哪些特性?",
			Header:     "特性",
			MultiSelect: true,
			Options: []tools.AskUserOption{
				{Label: "A"},
				{Label: "B"},
			},
		},
	}
}

func keyRunes(r string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)}
}

func TestQuestionState_ShowBuildsRows(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())
	assert.True(t, qs.IsActive())
	// 2 questions: 1 title + 2 options + 1 custom each
	assert.Equal(t, 8, len(qs.rows))
	assert.False(t, qs.complete())
}

func TestQuestionState_SingleSelectRadio(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())

	// focus: 0=title q0, 1=opt0 q0, 2=opt1 q0, 3=custom q0, ...
	qs.moveFocus(1)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	assert.True(t, qs.selected[0][0])

	// choosing opt1 clears opt0 (single-select)
	qs.moveFocus(1)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, qs.selected[0][0])
	assert.True(t, qs.selected[0][1])

	answers := qs.Answers()
	assert.Equal(t, []string{"自部署"}, answers[0].Selected)
}

func TestQuestionState_CustomText(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())

	// move to custom row of q0 (rows: 0 title,1 opt,2 opt,3 custom)
	qs.moveFocus(3)
	_, _ = qs.HandleKey(keyRunes("hello "))
	_, _ = qs.HandleKey(keyRunes("world"))
	assert.Equal(t, "hello world", qs.custom[0])
	assert.True(t, qs.useCustom[0])

	// selecting a radio option disables the custom toggle (row3 → row1 = -2)
	qs.moveFocus(-2)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	assert.False(t, qs.useCustom[0])

	answers := qs.Answers()
	assert.Equal(t, []string{"官方 API"}, answers[0].Selected)
	assert.Empty(t, answers[0].Text)
}

func TestQuestionState_MultiSelect(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())

	// q1 rows: 4 title, 5 opt A, 6 opt B, 7 custom. focus wraps: 7+1=0
	qs.moveFocus(5)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	qs.moveFocus(1)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace})

	assert.True(t, qs.selected[1][0])
	assert.True(t, qs.selected[1][1])
	// q0 still unanswered → incomplete
	assert.False(t, qs.complete())

	qs.moveFocus(-5) // q1 opt B (6) → q0 opt0 (1)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	assert.True(t, qs.complete())

	answers := qs.Answers()
	assert.ElementsMatch(t, []string{"A", "B"}, answers[1].Selected)
}

func TestQuestionState_SubmitRequiresComplete(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())

	// nothing answered: Y must not submit
	done, submitted := qs.HandleKey(keyRunes("y"))
	assert.False(t, done)
	assert.False(t, submitted)

	qs.moveFocus(1)
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace}) // q0 opt0
	done, submitted = qs.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.False(t, done) // q1 still missing

	qs.moveFocus(1) // q0 opt1
	qs.moveFocus(1) // q0 custom
	qs.moveFocus(1) // q1 title
	qs.moveFocus(1) // q1 opt A
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	done, submitted = qs.HandleKey(keyRunes("y"))
	assert.True(t, done)
	assert.True(t, submitted)
}

func TestQuestionState_EscSkips(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())
	done, submitted := qs.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, done)
	assert.False(t, submitted)
}

func TestQuestionState_BackspaceEditsCustom(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())
	qs.moveFocus(3)
	_, _ = qs.HandleKey(keyRunes("abc"))
	_, _ = qs.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "ab", qs.custom[0])
}

func TestQuestionState_RenderContainsContent(t *testing.T) {
	qs := NewQuestionState()
	qs.Show(sampleQuestions())
	out := qs.Render(DefaultTheme(), 100)
	assert.Contains(t, out, "采用哪种接入方式?")
	assert.Contains(t, out, "官方 API")
	assert.Contains(t, out, "自部署")
	assert.Contains(t, out, "启用哪些特性?")
}

func TestTuiModel_QuestionDialogRoundtrip(t *testing.T) {
	m := &TuiModel{
		input:        NewInputModel(),
		viewport:     NewMessageViewport(80, 20),
		statusBar:    *NewStatusBar(),
		messages:     []ChatMessage{},
		theme:        DefaultTheme(),
		completion:   NewCompletionState(),
		confirmation: NewConfirmationState(),
		questions:    NewQuestionState(),
	}
	ch := make(chan QuestionResultMsg, 1)

	m.Update(QuestionDialogMsg{Questions: sampleQuestions(), Result: ch})
	require.True(t, m.questions.IsActive())

	// answer q0 via first option, q1 via custom text
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // focus opt0 q0
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // opt1 q0
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // custom q0
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // title q1
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // opt A q1
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // opt B q1
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(keyRunes("y"))

	require.Len(t, ch, 1)
	result := <-ch
	assert.True(t, result.Submitted)
	assert.Equal(t, []string{"官方 API"}, result.Answers[0].Selected)
	assert.Equal(t, []string{"A", "B"}, result.Answers[1].Selected)
	assert.False(t, m.questions.IsActive())
}
