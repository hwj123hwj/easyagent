package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

func TestContextEndpointPreservesLogicalMessagesAndCompactionRecords(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)
	defer srv.cancel()
	sess, err := application.NewSession(context.Background())
	require.NoError(t, err)
	history := []ai.Message{ai.NewTextUserMessage("old task"), ai.AssistantMessage{Text: "old reply"}, ai.NewTextUserMessage("retained task"), ai.AssistantMessage{Text: "retained reply"}}
	for _, msg := range history {
		require.NoError(t, sess.Session().AppendMessage(context.Background(), msg))
	}
	require.NoError(t, sess.Session().AppendCompactionKeeping(context.Background(), "完整摘要", history[2:], &session.CompactionInfo{Trigger: "manual", Instructions: "保留测试", MessagesBefore: 4, MessagesAfter: 3}))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/sessions/"+sess.SessionID()+"/context", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var snapshot struct {
		System      string                     `json:"system"`
		Messages    []agent.ContextMessage     `json:"-"`
		Compactions []session.CompactionRecord `json:"compactions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	require.NotEmpty(t, snapshot.System)
	require.Len(t, snapshot.Compactions, 1)
	require.Contains(t, w.Body.String(), "retained task")
	require.Contains(t, w.Body.String(), "retained reply")
	require.Contains(t, w.Body.String(), "保留测试")
	messages, err := sess.Session().BuildContext(context.Background())
	require.NoError(t, err)
	display := serializeSessionContext(sess, messages)
	require.Len(t, display, 3)
	require.Equal(t, "compaction", display[0]["role"])
	require.Equal(t, "retained task", display[1]["content"])
	// Stale run projections are invalidated and restore the same compaction card.
	srv.invalidateRunSnapshot(sess.SessionID())
	run, err := srv.runSnapshot(sess.SessionID(), 0, "", nil)
	require.NoError(t, err)
	require.Equal(t, "compaction", run.Messages[0]["role"])
	bad := httptest.NewRecorder()
	srv.Handler().ServeHTTP(bad, localReq(http.MethodPost, "/sessions/"+sess.SessionID()+"/compact", strings.NewReader("{broken")))
	require.Equal(t, http.StatusBadRequest, bad.Code)
}

func TestMicroCompactionShowsOriginalOutputInHistoryButCleanedModelContext(t *testing.T) {
	ctx := context.Background()
	application := newTestApp(t)
	srv := New(application, nil)
	defer srv.cancel()
	sess, err := application.NewSession(ctx)
	require.NoError(t, err)
	history := []ai.Message{ai.NewTextUserMessage("inspect file"), ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "read", Name: "read"}}}, ai.ToolResultMessage{ToolCallID: "read", Content: "original readable file output"}}
	for _, msg := range history {
		require.NoError(t, sess.Session().AppendMessage(ctx, msg))
	}
	cleaned := append([]ai.Message(nil), history...)
	cleaned[2] = ai.ToolResultMessage{ToolCallID: "read", Content: "cleared model input"}
	require.NoError(t, sess.Session().AppendMicroCompaction(ctx, cleaned))
	for _, endpoint := range []string{"messages", "context"} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/sessions/"+sess.SessionID()+"/"+endpoint, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		if endpoint == "messages" {
			require.Contains(t, w.Body.String(), "original readable file output")
			require.NotContains(t, w.Body.String(), "cleared model input")
		} else {
			require.Contains(t, w.Body.String(), "cleared model input")
			require.NotContains(t, w.Body.String(), "original readable file output")
		}
	}
	// The WebSocket/reconnect projection must use the same original outputs.
	snapshot, err := srv.runSnapshot(sess.SessionID(), 0, "", nil)
	require.NoError(t, err)
	data, err := json.Marshal(snapshot.Messages)
	require.NoError(t, err)
	require.Contains(t, string(data), "original readable file output")
	require.NotContains(t, string(data), "cleared model input")
	srv.invalidateRunSnapshot(sess.SessionID())
	snapshot, err = srv.runSnapshot(sess.SessionID(), 0, "", nil)
	require.NoError(t, err)
	data, err = json.Marshal(snapshot.Messages)
	require.NoError(t, err)
	require.Contains(t, string(data), "original readable file output")
}

func TestContextEndpointRejectsRunningSession(t *testing.T) {
	srv, _, gateway := newRunTestServer(t)
	run, _, err := srv.startRun("", "test", "inspect-busy")
	require.NoError(t, err)
	<-gateway.requests
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/sessions/"+run.SessionID+"/context", nil))
	require.Equal(t, http.StatusConflict, w.Code)
	close(gateway.finish)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/sessions/"+run.SessionID+"/context", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "first last")
}

func TestContextSnapshotUsesConfiguredBearerAuthentication(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir, cfg.Workspace = t.TempDir(), t.TempDir()
	cfg.APIKey = "private-test-token"
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", "http://127.0.0.1:1"
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	defer application.Close()
	sess, err := application.NewSession(context.Background())
	require.NoError(t, err)
	srv := New(application, nil)
	srv.SetAPIKey(cfg.APIKey)
	defer srv.cancel()
	for _, token := range []string{"", "wrong", cfg.APIKey} {
		req := localReq(http.MethodGet, "/sessions/"+sess.SessionID()+"/context", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if token == cfg.APIKey {
			require.Equal(t, http.StatusOK, w.Code)
		} else {
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.NotContains(t, w.Body.String(), "system")
		}
	}
}
