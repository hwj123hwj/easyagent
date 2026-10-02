package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/require"
)

type acceptedHookWriter struct {
	*httptest.ResponseRecorder
	afterAccepted func()
	called        bool
}

func (w *acceptedHookWriter) Flush() {
	w.ResponseRecorder.Flush()
	if !w.called && strings.Contains(w.Body.String(), "event: accepted") {
		w.called = true
		w.afterAccepted()
	}
}

func TestRunInitialSSESnapshotCannotReturnLaterRunReply(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		text := "reply-for-A"
		if calls.Add(1) > 1 {
			text = "reply-for-B"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
	}))
	cfg := config.Default()
	cfg.DataDir, cfg.Workspace = t.TempDir(), t.TempDir()
	cfg.MCPConfigPath = filepath.Join(cfg.DataDir, "mcp.json")
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	srv := New(application, nil)
	t.Cleanup(func() { srv.cancel(); application.Close(); provider.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	writer := &acceptedHookWriter{ResponseRecorder: httptest.NewRecorder()}
	writer.afterAccepted = func() {
		// Freeze A's observer before its initial snapshot, and let another
		// entry finish B in the same session. Admission itself was not a retry.
		srv.runs.mu.Lock()
		first := srv.runs.requests["initial-A"]
		srv.runs.mu.Unlock()
		require.NotNil(t, first)
		result, err := srv.waitRun(ctx, first)
		require.NoError(t, err)
		require.Equal(t, "reply-for-A", result.Text)
		second, duplicate, err := srv.startRun(first.SessionID, "B", "later-B")
		require.NoError(t, err)
		require.False(t, duplicate)
		result, err = srv.waitRun(ctx, second)
		require.NoError(t, err)
		require.Equal(t, "reply-for-B", result.Text)
	}
	body, err := json.Marshal(ChatRequest{Prompt: "A", RequestID: "initial-A"})
	require.NoError(t, err)
	srv.Handler().ServeHTTP(writer, localReq(http.MethodPost, "/chat/stream", strings.NewReader(string(body))))
	require.True(t, writer.called)
	require.Equal(t, http.StatusOK, writer.Code)
	require.Contains(t, writer.Body.String(), `"text":"reply-for-A"`)
	require.NotContains(t, writer.Body.String(), "reply-for-B", "SSE is bound to its accepted run rather than the session's newest run")
	require.EqualValues(t, 2, calls.Load())
}
