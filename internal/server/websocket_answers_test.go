package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hwj123hwj/easyagent/sdk/tools"
)

// TestWSClientMessageAnswersParsing verifies the ws confirm message carries
// the answers payload through to confirmRun (ask_user_question fix).
func TestWSClientMessageAnswersParsing(t *testing.T) {
	raw := []byte(`{
		"type": "confirm",
		"session_id": "s1",
		"run_id": "r1",
		"confirmation_id": "c1",
		"approved": true,
		"answers": [{"selected": ["官方 API"], "text": ""}]
	}`)
	var msg wsClientMessage
	require.NoError(t, json.Unmarshal(raw, &msg))
	assert.Equal(t, "confirm", msg.Type)
	require.NotNil(t, msg.Answers)

	var answers []tools.AskUserAnswer
	require.NoError(t, json.Unmarshal(msg.Answers, &answers))
	assert.Equal(t, []string{"官方 API"}, answers[0].Selected)
}

// TestConfirmRunVarargsNilSafe makes sure confirmRun tolerates no-answers
// calls (plain tool approval path).
func TestConfirmRunVarargsNilSafe(t *testing.T) {
	// compile-time: variadic response param accepts zero values
	var _ func(sessionID, runID, confirmationID string, approved bool, reason string, response ...json.RawMessage) error = (*Server)(nil).confirmRun
}
