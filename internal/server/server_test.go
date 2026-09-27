package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestApp(t *testing.T) *app.App {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIBaseURL = "http://localhost:4001"
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { application.Close() })
	return application
}

func TestServer_Health(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	req := localReq(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp["status"])
}

func TestServer_Chat(t *testing.T) {
	t.Skip("skipping: requires a running gateway with valid API key")
}

func TestServer_Chat_EmptyPrompt(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	body := bytes.NewReader([]byte(`{"prompt":""}`))
	req := localReq(http.MethodPost, "/chat", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_Chat_InvalidJSON(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	body := bytes.NewReader([]byte(`invalid json`))
	req := localReq(http.MethodPost, "/chat", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_Tools(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	req := localReq(http.MethodGet, "/tools", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_Sessions(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	req := localReq(http.MethodGet, "/sessions", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_CreateSession(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	req := localReq(http.MethodPost, "/sessions", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp SessionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp.ID)
}

func TestSessionInfoReportsPathPolicyBehindAuthentication(t *testing.T) {
	for _, allow := range []bool{false, true} {
		cfg := config.Default()
		cfg.DataDir = t.TempDir()
		cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", "http://127.0.0.1:1"
		cfg.AllowOutsideWorkspace = allow
		application, err := app.New(app.AppOptions{Config: cfg})
		require.NoError(t, err)
		t.Cleanup(func() { application.Close() })
		srv := New(application, nil)
		srv.SetAPIKey("secret-key")
		req := localReq(http.MethodPost, "/sessions", nil)
		req.Header.Set("Authorization", "Bearer secret-key")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var session SessionResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&session))
		path := "/sessions/" + session.ID + "/info"
		req = localReq(http.MethodGet, path, nil)
		w = httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		req.Header.Set("Authorization", "Bearer secret-key")
		w = httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var info map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&info))
		assert.Equal(t, allow, info["allow_outside_workspace"])
	}
}

func TestServer_DeleteSession(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	// Create first
	req := localReq(http.MethodPost, "/sessions", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp SessionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Delete
	req2 := localReq(http.MethodDelete, "/sessions/"+resp.ID, nil)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestServer_SessionMessages_NotFound(t *testing.T) {
	application := newTestApp(t)
	srv := New(application, nil)

	req := localReq(http.MethodGet, "/sessions/nonexistent/messages", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestChatStreamRouteIsAuthenticated(t *testing.T) {
	srv := New(newTestApp(t), nil)
	srv.SetAPIKey("test-key")
	for _, key := range []string{"", "test-key"} {
		req := httptest.NewRequest(http.MethodPost, "/chat/stream", bytes.NewBufferString(`{"prompt":""}`))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		want := http.StatusUnauthorized
		if key != "" {
			want = http.StatusBadRequest
		}
		assert.Equal(t, want, w.Code, w.Body.String())
	}
}
