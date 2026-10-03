package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/require"
)

type runTestGateway struct {
	requests     chan map[string]any
	finish       chan struct{}
	disconnected atomic.Bool
	calls        atomic.Int32
}

func newRunTestServer(t *testing.T) (*Server, *httptest.Server, *runTestGateway) {
	return newFileRunTestServer(t, "")
}

func newFileRunTestServer(t *testing.T, toolFile string, dataInside ...bool) (*Server, *httptest.Server, *runTestGateway) {
	t.Helper()
	gateway := &runTestGateway{finish: make(chan struct{}), requests: make(chan map[string]any, 32)}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		gateway.calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gateway.requests <- body
		w.Header().Set("Content-Type", "text/event-stream")
		if toolFile != "" {
			messages := body["messages"].([]any)
			if messages[len(messages)-1].(map[string]any)["role"] != "tool" {
				arguments, _ := json.Marshal(map[string]string{"path": toolFile, "content": fmt.Sprintf("tool-result-%d", gateway.calls.Load())})
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("write-%d", gateway.calls.Load()), "type": "function", "function": map[string]any{"name": "write", "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
				return
			}
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first \"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-gateway.finish:
		case <-r.Context().Done():
			gateway.disconnected.Store(true)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"last\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	cfg := config.Default()
	cfg.DataDir, cfg.Workspace = t.TempDir(), t.TempDir()
	if len(dataInside) > 0 && dataInside[0] {
		cfg.DataDir = filepath.Join(cfg.Workspace, "service-state")
		require.NoError(t, os.MkdirAll(cfg.DataDir, 0700))
	}
	cfg.AutoApprove = toolFile != ""
	cfg.MCPConfigPath = filepath.Join(cfg.DataDir, "mcp.json")
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	srv := New(application, nil)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		srv.cancel()
		srv.runs.mu.Lock()
		var done []<-chan struct{}
		for _, run := range srv.runs.requests {
			done = append(done, run.done)
		}
		srv.runs.mu.Unlock()
		for _, channel := range done {
			select {
			case <-channel:
			case <-time.After(3 * time.Second):
				t.Error("run did not shut down")
			}
		}
		httpServer.Close()
		application.Close()
		provider.Close()
	})
	return srv, httpServer, gateway
}

func dialRunWS(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readRunMessage(t *testing.T, conn *websocket.Conn, wanted string) map[string]any {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	for i := 0; i < 1000; i++ {
		var message map[string]any
		require.NoError(t, conn.ReadJSON(&message))
		if message["type"] == wanted {
			return message
		}
	}
	t.Fatal("message not received: " + wanted)
	return nil
}

func waitRunText(t *testing.T, srv *Server, run *sessionRun) {
	t.Helper()
	require.Eventually(t, func() bool {
		srv.runs.mu.Lock()
		defer srv.runs.mu.Unlock()
		return len(run.projection) > 0 && run.projection[0].Type == agent.StreamEventTextDelta
	}, 3*time.Second, time.Millisecond)
}

func TestRunWebSocketDisconnectReplaysWithoutRestartOrCancellation(t *testing.T) {
	srv, server, gateway := newRunTestServer(t)
	conn := dialRunWS(t, server)
	require.NoError(t, conn.WriteJSON(wsClientMessage{Type: "prompt", Prompt: "hello", RequestID: "once"}))
	ack := readRunMessage(t, conn, "accepted")
	sessionID, runID := ack["session_id"].(string), ack["run_id"].(string)
	srv.runs.mu.Lock()
	run := srv.runs.requests["once"]
	srv.runs.mu.Unlock()
	waitRunText(t, srv, run)
	conn.Close()
	require.False(t, gateway.disconnected.Load(), "socket lifetime must not own the provider context")
	reconnected := dialRunWS(t, server)
	require.NoError(t, reconnected.WriteJSON(wsClientMessage{Type: "subscribe", SessionID: sessionID, RunID: runID}))
	snapshot := readRunMessage(t, reconnected, "snapshot")
	require.Equal(t, "first ", snapshot["events"].([]any)[0].(map[string]any)["text_delta"])
	require.NoError(t, reconnected.WriteJSON(wsClientMessage{Type: "prompt", SessionID: sessionID, Prompt: "hello", RequestID: "once"}))
	duplicate := readRunMessage(t, reconnected, "accepted")
	require.Equal(t, true, duplicate["duplicate"])
	require.Equal(t, runID, duplicate["run_id"])
	require.EqualValues(t, 1, gateway.calls.Load())
	close(gateway.finish)
	result, err := srv.waitRun(context.Background(), run)
	require.NoError(t, err)
	require.Equal(t, "first last", result.Text)
	require.False(t, gateway.disconnected.Load())
	final := readRunMessage(t, reconnected, "status")
	for final["state"] == "running" {
		final = readRunMessage(t, reconnected, "status")
	}
	require.Equal(t, "completed", final["state"])
	// Reconnection at a known cursor receives only subsequent envelopes.
	require.NoError(t, reconnected.WriteJSON(wsClientMessage{Type: "subscribe", SessionID: sessionID, RunID: runID, AfterSeq: uint64(snapshot["seq"].(float64))}))
	replay := readRunMessage(t, reconnected, "replay")
	require.NotEmpty(t, replay["events"])
}

func TestRunSessionBusyAndStaleCancelCannotAffectNewTask(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	first, _, err := srv.startRun("", "first", "first-id")
	require.NoError(t, err)
	waitRunText(t, srv, first)
	_, _, err = srv.startRun(first.SessionID, "overlap", "overlap-id")
	require.ErrorContains(t, err, "正在处理")
	_, _, err = srv.startRun(first.SessionID, "different payload", "first-id")
	require.ErrorContains(t, err, "request_id")
	require.Error(t, srv.cancelRun(first.SessionID, ""))
	require.Error(t, srv.cancelRun(first.SessionID, "other-run"))
	require.False(t, gateway.disconnected.Load())
	require.NoError(t, srv.cancelRun(first.SessionID, first.ID))
	_, err = srv.waitRun(context.Background(), first)
	require.ErrorIs(t, err, context.Canceled)
	second, _, err := srv.startRun(first.SessionID, "second", "second-id")
	require.NoError(t, err)
	require.Error(t, srv.cancelRun(first.SessionID, first.ID))
	srv.runs.mu.Lock()
	require.Equal(t, second, srv.runs.sessions[first.SessionID].run)
	srv.runs.mu.Unlock()
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), second)
	require.NoError(t, err)
}

func TestRunAdmissionFailureLeavesRequestIDRetryable(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	sess, err := srv.app.NewSession(context.Background())
	require.NoError(t, err)
	release, err := srv.app.BeginMCPEdit()
	require.NoError(t, err)
	rejected, duplicate, err := srv.startRun(sess.SessionID(), "retry unchanged", "retry-id")
	require.Error(t, err)
	require.Nil(t, rejected)
	require.False(t, duplicate)
	release()
	run, duplicate, err := srv.startRun(sess.SessionID(), "retry unchanged", "retry-id")
	require.NoError(t, err)
	require.False(t, duplicate, "a rejected, unexecuted prompt must not become a settled duplicate")
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
	require.EqualValues(t, 1, gateway.calls.Load())
}

func TestRunMultipleSessionsAndRESTCannotCancelWebSocketOwner(t *testing.T) {
	srv, server, gateway := newRunTestServer(t)
	first, _, err := srv.startRun("", "first", "one")
	require.NoError(t, err)
	second, _, err := srv.startRun("", "second", "two")
	require.NoError(t, err)
	waitRunText(t, srv, first)
	waitRunText(t, srv, second)
	body, _ := json.Marshal(ChatRequest{SessionID: first.SessionID, Prompt: "overlap"})
	response, err := http.Post(server.URL+"/chat/stream", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	response.Body.Close()
	require.False(t, gateway.disconnected.Load())
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), first)
	require.NoError(t, err)
	_, err = srv.waitRun(context.Background(), second)
	require.NoError(t, err)
}

func TestRunSSEDisconnectOnlyUnsubscribes(t *testing.T) {
	srv, server, gateway := newRunTestServer(t)
	response, err := http.Post(server.URL+"/chat/stream", "application/json", strings.NewReader(`{"prompt":"sse","request_id":"sse-once"}`))
	require.NoError(t, err)
	response.Body.Close()
	require.Eventually(t, func() bool {
		srv.runs.mu.Lock()
		defer srv.runs.mu.Unlock()
		return srv.runs.requests["sse-once"] != nil
	}, time.Second, time.Millisecond)
	srv.runs.mu.Lock()
	run := srv.runs.requests["sse-once"]
	srv.runs.mu.Unlock()
	waitRunText(t, srv, run)
	require.False(t, gateway.disconnected.Load())
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunReplayWindowGapReturnsCompleteProjection(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "stream", "window")
	require.NoError(t, err)
	waitRunText(t, srv, run)
	srv.runs.mu.Lock()
	state := srv.runs.sessions[run.SessionID]
	for i := 0; i < runReplayLimit+10; i++ {
		event := agent.AgentStreamEvent{Type: agent.StreamEventTextDelta, TextDelta: "x"}
		appendRunProjection(run, event)
		srv.runs.publishLocked(state, wsServerMessage{Type: "event", SessionID: run.SessionID, RunID: run.ID, Event: event})
	}
	require.Len(t, state.replay, runReplayLimit)
	srv.runs.mu.Unlock()
	snapshot, err := srv.runSnapshot(run.SessionID, 1, run.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "snapshot", snapshot.Type)
	events := snapshot.Events.([]agent.AgentStreamEvent)
	require.Equal(t, "first "+strings.Repeat("x", runReplayLimit+10), events[0].TextDelta)
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunConfirmationWaitsRejectsMissingChannelAndSurvivesSubscriberLoss(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	require.False(t, srv.confirmRunTool(context.Background(), agent.ConfirmationRequest{}).Approved)
	run, _, err := srv.startRun("", "approval", "approval")
	require.NoError(t, err)
	waitRunText(t, srv, run)
	decisions := make(chan agent.ConfirmDecision, 1)
	ctx := context.WithValue(context.Background(), runContextKey{}, run)
	go func() {
		decisions <- srv.confirmRunTool(ctx, agent.ConfirmationRequest{ToolCallID: "call", ToolName: "mcp.test", Args: json.RawMessage(`{}`), Description: "write external data"})
	}()
	require.Eventually(t, func() bool { srv.runs.mu.Lock(); defer srv.runs.mu.Unlock(); return len(run.pending) == 1 }, time.Second, time.Millisecond)
	snapshot, err := srv.runSnapshot(run.SessionID, 0, "", nil)
	require.NoError(t, err)
	require.Equal(t, "waiting_confirmation", snapshot.Run.State)
	require.Len(t, snapshot.PendingConfirmations, 1)
	select {
	case <-decisions:
		t.Fatal("must wait for explicit approval")
	default:
	}
	id := snapshot.PendingConfirmations[0].ID
	require.Error(t, srv.confirmRun(run.SessionID, "old-run", id, true, ""))
	require.NoError(t, srv.confirmRun(run.SessionID, run.ID, id, false, "declined"))
	require.False(t, (<-decisions).Approved)
	require.Error(t, srv.confirmRun(run.SessionID, run.ID, id, true, ""))
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunConfirmationCancellationReturnsDenied(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "approval", "cancel-approval")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), runContextKey{}, run))
	decisions := make(chan agent.ConfirmDecision, 1)
	go func() { decisions <- srv.confirmRunTool(ctx, agent.ConfirmationRequest{ToolCallID: "call"}) }()
	require.Eventually(t, func() bool { srv.runs.mu.Lock(); defer srv.runs.mu.Unlock(); return len(run.pending) == 1 }, time.Second, time.Millisecond)
	cancel()
	require.False(t, (<-decisions).Approved)
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunConfirmationTimeoutDeniesAndClearsWaitingState(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "approval", "timeout-approval")
	require.NoError(t, err)
	srv.runs.mu.Lock()
	srv.runs.confirmationTimeout = 10 * time.Millisecond
	srv.runs.mu.Unlock()
	ctx := context.WithValue(context.Background(), runContextKey{}, run)
	decision := srv.confirmRunTool(ctx, agent.ConfirmationRequest{ToolCallID: "call"})
	require.False(t, decision.Approved)
	require.Contains(t, decision.Reason, "超时")
	snapshot, err := srv.runSnapshot(run.SessionID, 0, "", nil)
	require.NoError(t, err)
	require.Empty(t, snapshot.PendingConfirmations)
	require.Equal(t, "running", snapshot.Run.State)
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunReplayByteBudgetCannotPretendOversizeEventWasReplayed(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "big tool", "byte-budget")
	require.NoError(t, err)
	waitRunText(t, srv, run)
	srv.runs.mu.Lock()
	state := srv.runs.sessions[run.SessionID]
	event := agent.AgentStreamEvent{Type: agent.StreamEventToolEnd, ToolCallID: "large", ToolResult: strings.Repeat("x", runReplayBytesLimit+1)}
	appendRunProjection(run, event)
	srv.runs.publishLocked(state, wsServerMessage{Type: "event", SessionID: run.SessionID, RunID: run.ID, Event: event})
	require.LessOrEqual(t, state.replayBytes, runReplayBytesLimit)
	require.Empty(t, state.replay)
	srv.runs.mu.Unlock()
	snapshot, err := srv.runSnapshot(run.SessionID, 1, run.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "snapshot", snapshot.Type)
	require.Len(t, snapshot.Events.([]agent.AgentStreamEvent), 2)
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunDeleteCannotRemoveActiveSession(t *testing.T) {
	srv, server, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "keep active", "active-delete")
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodDelete, server.URL+"/sessions/"+run.SessionID, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	response.Body.Close()
	require.True(t, srv.app.SessionManager().Exists(run.SessionID))
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}

func TestRunDeleteCannotRemoveSDKOwnedActiveSession(t *testing.T) {
	srv, server, gateway := newRunTestServer(t)
	sess, err := srv.app.NewSession(context.Background())
	require.NoError(t, err)
	stream, err := sess.PromptStream(context.Background(), "SDK-owned task")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		for range stream {
		}
		close(done)
	}()
	request, err := http.NewRequest(http.MethodDelete, server.URL+"/sessions/"+sess.SessionID(), nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	response.Body.Close()
	require.True(t, srv.app.SessionManager().Exists(sess.SessionID()))
	close(gateway.finish)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SDK stream did not finish")
	}
}

func TestRunUntrustedMCPStillWaitsForApprovalWithConfirmOff(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "mcp approval", "mcp-off")
	require.NoError(t, err)
	waitRunText(t, srv, run)
	sess, err := srv.resolveSession(context.Background(), run.SessionID)
	require.NoError(t, err)
	sess.SetConfirmEnabled(false)
	confirm := sess.ConfirmationCallback()
	ctx := context.WithValue(context.Background(), runContextKey{}, run)
	require.True(t, confirm(ctx, agent.ConfirmationRequest{ToolName: "bash"}).Approved, "ordinary tool confirmation keeps the explicit off behavior")
	decisions := make(chan agent.ConfirmDecision, 1)
	go func() {
		decisions <- confirm(ctx, agent.ConfirmationRequest{ToolName: "mcp_untrusted_write", ToolCallID: "mcp-call", RequiresApproval: true})
	}()
	require.Eventually(t, func() bool { srv.runs.mu.Lock(); defer srv.runs.mu.Unlock(); return len(run.pending) == 1 }, time.Second, time.Millisecond)
	snapshot, err := srv.runSnapshot(run.SessionID, 0, "", nil)
	require.NoError(t, err)
	require.Equal(t, "waiting_confirmation", snapshot.Run.State)
	select {
	case <-decisions:
		t.Fatal("untrusted MCP must not execute automatically")
	default:
	}
	require.NoError(t, srv.confirmRun(run.SessionID, run.ID, snapshot.PendingConfirmations[0].ID, false, "not trusted"))
	require.False(t, (<-decisions).Approved)
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
}
