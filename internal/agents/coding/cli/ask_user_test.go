package cli

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/tools"
)

func newStringReader(s string) *strings.Reader { return strings.NewReader(s) }

// validate mirrors the real pipeline: ExecuteConfirmed validates the encoded
// answers against the raw tool arguments.
func validate(decision agent.ConfirmDecision) ([]tools.AskUserAnswer, error) {
	return tools.ValidateAskUserAnswers(json.RawMessage(reqArgsForTest), decision.Response)
}

const reqArgsForTest = `{"questions":[` +
	`{"question":"接入方式?","header":"接入","options":[{"label":"官方 API"},{"label":"自部署"}]},` +
	`{"question":"启用哪些特性?","header":"特性","multiSelect":true,"options":[{"label":"A"},{"label":"B"}]}` +
	`]}`

func TestParseAnswerChoice(t *testing.T) {
	idxs, text, ok := parseAnswerChoice("1", 3)
	assert.True(t, ok)
	assert.Equal(t, []int{1}, idxs)
	assert.Empty(t, text)

	idxs, text, ok = parseAnswerChoice("1,3", 3)
	assert.True(t, ok)
	assert.Equal(t, []int{1, 3}, idxs)

	idxs, text, ok = parseAnswerChoice("2，3", 3) // 中文逗号
	assert.True(t, ok)
	assert.Equal(t, []int{2, 3}, idxs)

	_, text, ok = parseAnswerChoice("0: 用本地模型", 3)
	assert.True(t, ok)
	assert.Equal(t, "用本地模型", text)

	_, _, ok = parseAnswerChoice("0 自己部署", 3)
	assert.True(t, ok)

	// invalid inputs
	for _, in := range []string{"", "0", "0:", "4", "1,1", "abc", "1,4", "1 2 2"} {
		_, _, ok = parseAnswerChoice(in, 3)
		assert.False(t, ok, "input %q should be invalid", in)
	}
}

func TestPromptAskUserRoundtrip(t *testing.T) {
	req := agent.ConfirmationRequest{
		ToolName: "ask_user_question",
		Args: json.RawMessage(`{"questions":[` +
			`{"question":"接入方式?","header":"接入","options":[{"label":"官方 API"},{"label":"自部署"}]},` +
			`{"question":"启用哪些特性?","header":"特性","multiSelect":true,"options":[{"label":"A"},{"label":"B"}]}` +
			`]}`),
	}

	input := "1\n0: 自定义说明\n"
	decision := promptAskUser(io.Discard, newStringReader(input), req)
	assert.True(t, decision.Approved)
	require.NotNil(t, decision.Response)

	answers, err := validate(decision)
	require.NoError(t, err)
	assert.Equal(t, []string{"官方 API"}, answers[0].Selected)
	assert.Empty(t, answers[0].Text)
	assert.Empty(t, answers[1].Selected)
	assert.Equal(t, "自定义说明", answers[1].Text)
}

func TestPromptAskUserInvalidArgsDeclines(t *testing.T) {
	req := agent.ConfirmationRequest{ToolName: "ask_user_question", Args: json.RawMessage(`{"questions":[]}`)}
	decision := promptAskUser(io.Discard, newStringReader("1\n"), req)
	assert.False(t, decision.Approved)
	assert.Contains(t, decision.Reason, "invalid questions")
}
