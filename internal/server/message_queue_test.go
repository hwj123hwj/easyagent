package server

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestQueueEditsPersistAndRunsExactlyOnce(t *testing.T) {
	s, _, gateway := newRunTestServer(t)
	run, _, err := s.startRun("", "first", "first-request")
	require.NoError(t, err)
	q, err := s.changeQueue(run.SessionID, queueMutation{Action: "add", ID: "next", Prompt: "old"})
	require.NoError(t, err)
	require.Len(t, q.Items, 1)
	_, err = s.changeQueue(run.SessionID, queueMutation{Action: "edit", ID: "next", Prompt: "edited"})
	require.NoError(t, err)
	// An exact retry of admission must not append or revert the edited item.
	q, err = s.changeQueue(run.SessionID, queueMutation{Action: "add", ID: "next", Prompt: "old"})
	require.NoError(t, err)
	require.Len(t, q.Items, 1)
	require.Equal(t, "edited", q.Items[0].Prompt)
	close(gateway.finish)
	require.Eventually(t, func() bool { return gateway.calls.Load() == 2 }, 3*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { q, _ := s.readQueue(run.SessionID); return len(q.Items) == 0 }, 3*time.Second, time.Millisecond)
	q, err = s.changeQueue(run.SessionID, queueMutation{Action: "add", ID: "next", Prompt: "old"})
	require.NoError(t, err)
	require.Empty(t, q.Items)
	require.Equal(t, int32(2), gateway.calls.Load())
}

func TestQueueRestorePausesAndProtectsOtherSession(t *testing.T) {
	s, _, gateway := newRunTestServer(t)
	run, _, err := s.startRun("", "first", "first-request")
	require.NoError(t, err)
	_, err = s.changeQueue(run.SessionID, queueMutation{Action: "add", ID: "next", Prompt: "later"})
	require.NoError(t, err)
	// A different process needs explicit resume; reconnecting the client does not.
	fresh := New(s.app, nil)
	defer fresh.cancel()
	q, err := fresh.readQueue(run.SessionID)
	require.NoError(t, err)
	require.True(t, q.Paused)
	require.Len(t, q.Items, 1)
	_, err = fresh.changeQueue("../outside", queueMutation{Action: "add", ID: "unsafe", Prompt: "later"})
	require.Error(t, err)
	_, err = s.changeQueue(run.SessionID, queueMutation{Action: "remove", ID: "next"})
	require.NoError(t, err)
	close(gateway.finish)
}

func TestQueuedRunPublishesBoundaryBeforeDeltas(t *testing.T) {
	s, server, gateway := newRunTestServer(t)
	conn := dialRunWS(t, server)
	require.NoError(t, conn.WriteJSON(wsClientMessage{Type: "prompt", Prompt: "first", RequestID: "one"}))
	accepted := readRunMessage(t, conn, "accepted")
	id := accepted["session_id"].(string)
	readRunMessage(t, conn, "snapshot")
	_, err := s.changeQueue(id, queueMutation{Action: "add", ID: "second", Prompt: "follow-up"})
	require.NoError(t, err)
	close(gateway.finish)
	next := readRunMessage(t, conn, "accepted")
	require.Equal(t, queueRequestID(id, "second"), next["request_id"])
	require.Equal(t, "follow-up", next["prompt"])
	require.NotEqual(t, accepted["run_id"], next["run_id"])
	event := readRunMessage(t, conn, "event")
	require.Equal(t, next["run_id"], event["run_id"])
}
