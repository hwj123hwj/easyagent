package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const workspaceSessionTestToken = "workspace-session-test-token"

type workspaceSessionFixture struct {
	app                          *app.App
	handler                      http.Handler
	global, project, outside, id string
}

func newWorkspaceSessionFixture(t *testing.T) workspaceSessionFixture {
	t.Helper()
	base := realPath(t, t.TempDir())
	f := workspaceSessionFixture{
		global:  filepath.Join(base, "default"),
		project: filepath.Join(base, "project"),
		outside: filepath.Join(base, "outside"),
	}
	for _, dir := range []string{f.global, filepath.Join(f.project, "sub"), f.outside} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	for path, content := range map[string]string{
		filepath.Join(f.global, "default.txt"):     "default workspace",
		filepath.Join(f.project, "hello.txt"):      "selected project",
		filepath.Join(f.project, "sub/nested.txt"): "nested project file",
		filepath.Join(f.outside, "secret.txt"):     "outside content",
	} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	f.app = newTestAppWithWorkspace(t, f.global)
	srv := New(f.app, nil)
	srv.SetAPIKey(workspaceSessionTestToken)
	f.handler = srv.Handler()

	// Use the public creation endpoint: the native picker can select a project
	// outside the core process's default workspace.
	body, err := json.Marshal(CreateSessionRequest{Cwd: f.project})
	require.NoError(t, err)
	req := localReq(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+workspaceSessionTestToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var session SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))
	require.NotEmpty(t, session.ID)
	f.id = session.ID
	return f
}

func (f workspaceSessionFixture) request(method, endpoint, path string, sessionID *string, token string) *httptest.ResponseRecorder {
	query := url.Values{}
	if path != "" {
		query.Set("path", path)
	}
	if sessionID != nil {
		query.Set("session_id", *sessionID)
	}
	req := localReq(method, "/workspace/"+endpoint+"?"+query.Encode(), strings.NewReader(`{"content":"written project file"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

func TestWorkspaceSession_SelectedProject(t *testing.T) {
	f := newWorkspaceSessionFixture(t)

	w := f.request(http.MethodGet, "list-dir", "", &f.id, workspaceSessionTestToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var entries []DirEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &entries))
	require.Len(t, entries, 2)
	for _, entry := range entries {
		assert.True(t, within(entry.Path, f.project))
	}
	assert.Equal(t, "hello.txt", entries[0].Name)
	assert.Equal(t, "sub", entries[1].Name)
	assert.True(t, entries[1].IsDir)

	w = f.request(http.MethodGet, "search-files", "", &f.id, workspaceSessionTestToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var files []string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &files))
	assert.ElementsMatch(t, []string{"hello.txt", filepath.Join("sub", "nested.txt")}, files)

	for _, path := range []string{"hello.txt", filepath.Join(f.project, "hello.txt")} {
		w = f.request(http.MethodGet, "read-file", path, &f.id, workspaceSessionTestToken)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var payload map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		assert.Equal(t, "selected project", payload["content"])
	}

	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0, 0xff}
	require.NoError(t, os.WriteFile(filepath.Join(f.project, "image.png"), imageBytes, 0o644))
	w = f.request(http.MethodGet, "read-file-base64", "image.png", &f.id, workspaceSessionTestToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var image map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &image))
	decoded, err := base64.StdEncoding.DecodeString(image["data"])
	require.NoError(t, err)
	assert.Equal(t, imageBytes, decoded)
	assert.Equal(t, "image/png", image["mimeType"])

	w = f.request(http.MethodPut, "write-file", "created/new.txt", &f.id, workspaceSessionTestToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	content, err := os.ReadFile(filepath.Join(f.project, "created/new.txt"))
	require.NoError(t, err)
	assert.Equal(t, "written project file", string(content))
	_, err = os.Stat(filepath.Join(f.global, "created/new.txt"))
	assert.True(t, os.IsNotExist(err))

	// Evict the runtime cache to verify the saved session workspace is restored.
	require.NoError(t, f.app.SessionStore().Delete(f.id))
	w = f.request(http.MethodGet, "read-file", "hello.txt", &f.id, workspaceSessionTestToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "selected project")
}

var workspaceSessionEndpoints = []struct {
	method, endpoint string
	directory        bool
}{
	{http.MethodGet, "list-dir", true},
	{http.MethodGet, "search-files", true},
	{http.MethodGet, "read-file", false},
	{http.MethodGet, "read-file-base64", false},
	{http.MethodPut, "write-file", false},
}

func TestWorkspaceSession_RejectsEscapes(t *testing.T) {
	f := newWorkspaceSessionFixture(t)
	require.NoError(t, os.Symlink(f.outside, filepath.Join(f.project, "escape")))
	for _, endpoint := range workspaceSessionEndpoints {
		t.Run(endpoint.endpoint, func(t *testing.T) {
			paths := []string{f.global, "../default", "escape"}
			if !endpoint.directory {
				paths = []string{
					filepath.Join(f.global, "default.txt"),
					"../default/default.txt",
					"escape/secret.txt",
					"escape/new/created.txt",
				}
			}
			for _, path := range paths {
				w := f.request(endpoint.method, endpoint.endpoint, path, &f.id, workspaceSessionTestToken)
				assert.Equal(t, http.StatusBadRequest, w.Code, "path %q: %s", path, w.Body.String())
			}
		})
	}
	for path, expected := range map[string]string{
		filepath.Join(f.global, "default.txt"): "default workspace",
		filepath.Join(f.outside, "secret.txt"): "outside content",
	} {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, expected, string(content))
	}
	_, err := os.Stat(filepath.Join(f.outside, "new/created.txt"))
	assert.True(t, os.IsNotExist(err))
}

func TestWorkspaceSession_NoSessionKeepsGlobalBoundary(t *testing.T) {
	f := newWorkspaceSessionFixture(t)
	for _, endpoint := range workspaceSessionEndpoints {
		t.Run(endpoint.endpoint, func(t *testing.T) {
			globalPath, projectPath := f.global, f.project
			if !endpoint.directory {
				globalPath = filepath.Join(globalPath, "default.txt")
				projectPath = filepath.Join(projectPath, "hello.txt")
			}
			w := f.request(endpoint.method, endpoint.endpoint, globalPath, nil, workspaceSessionTestToken)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			w = f.request(endpoint.method, endpoint.endpoint, projectPath, nil, workspaceSessionTestToken)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
	for _, endpoint := range []string{"list-dir", "search-files"} {
		w := f.request(http.MethodGet, endpoint, "", nil, workspaceSessionTestToken)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "default.txt")
		assert.NotContains(t, w.Body.String(), "hello.txt")
	}
}

func TestWorkspaceSession_InvalidSessionHasNoFallback(t *testing.T) {
	f := newWorkspaceSessionFixture(t)
	// A session ID must not load metadata from a directory symlink outside the
	// session store, even if that directory resembles persisted session data.
	metadata, err := json.Marshal(map[string]string{"workspace": f.outside})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.outside, "meta.json"), metadata, 0o644))
	require.NoError(t, os.Symlink(f.outside, filepath.Join(f.app.SessionManager().SessionsDir(), "linked-session")))
	invalidIDs := []string{"", "missing-session", ".", "..", "../outside", f.outside, `..\outside`, "linked-session"}
	for _, endpoint := range workspaceSessionEndpoints {
		t.Run(endpoint.endpoint, func(t *testing.T) {
			path := f.global
			if !endpoint.directory {
				path = filepath.Join(path, "default.txt")
			}
			for _, id := range invalidIDs {
				w := f.request(endpoint.method, endpoint.endpoint, path, &id, workspaceSessionTestToken)
				assert.Equal(t, http.StatusNotFound, w.Code, "session %q: %s", id, w.Body.String())
			}
		})
	}
	assert.Equal(t, []string{f.id}, f.app.SessionStore().List(), "invalid IDs must not create runtime sessions")
	_, err = os.Stat(filepath.Join(f.outside, "session.jsonl"))
	assert.True(t, os.IsNotExist(err), "invalid IDs must not create session files outside the store")
	content, err := os.ReadFile(filepath.Join(f.global, "default.txt"))
	require.NoError(t, err)
	assert.Equal(t, "default workspace", string(content), "invalid sessions must not fall back to a global write")
}

func TestWorkspaceSession_RequiresAuthentication(t *testing.T) {
	f := newWorkspaceSessionFixture(t)
	for _, endpoint := range workspaceSessionEndpoints {
		t.Run(endpoint.endpoint, func(t *testing.T) {
			path := "hello.txt"
			if endpoint.directory {
				path = f.project
			}
			w := f.request(endpoint.method, endpoint.endpoint, path, &f.id, "wrong-token")
			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
}
