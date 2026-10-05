package coding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/stretchr/testify/require"
)

func TestReadProtectionSurvivesToolRebuildAndIsolatesSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	require.NoError(t, os.WriteFile(path, []byte("initial"), 0o600))
	app := CodingApplication{}
	ext := app.NewSessionExt()
	opts := runtime.ToolBuildOptions{Workspace: dir, SessionID: "session", SessionExt: ext}
	find := func(list []agent.Tool, name string) agent.Tool {
		for _, tool := range list {
			if tool.Name() == name {
				return tool
			}
		}
		t.Fatalf("missing tool %s", name)
		return nil
	}
	readArgs, err := json.Marshal(map[string]string{"path": path})
	require.NoError(t, err)
	_, err = find(app.BuildTools(opts), "read").Execute(context.Background(), readArgs, nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("external"), 0o600))
	writeArgs, err := json.Marshal(map[string]string{"path": path, "content": "replacement"})
	require.NoError(t, err)
	_, err = find(app.BuildTools(opts), "write").Execute(context.Background(), writeArgs, nil)
	require.ErrorContains(t, err, "modified externally")
	// Even the same persisted ID must not retain a discarded live session's state.
	opts.SessionExt = app.NewSessionExt()
	_, err = find(app.BuildTools(opts), "write").Execute(context.Background(), writeArgs, nil)
	require.NoError(t, err)
}

func TestGatewayCatalogIsAuthoritative(t *testing.T) {
	t.Setenv("EA_MODELS_FILE", filepath.Join(t.TempDir(), "missing.json"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"coding"},{"id":"claude-sonnet-4-6"}]}`))
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Provider, cfg.OpenAIBaseURL, cfg.OpenAIAPIKey = "openai", server.URL+"/v1", "test-key"
	app := NewCodingApplication(cfg)
	require.ElementsMatch(t, []slashcmd.ModelInfo{
		{Provider: "openai", ModelID: "coding"},
		{Provider: "openai", ModelID: "claude-sonnet-4-6"},
	}, app.AvailableModels())
	def, ok := app.modelReg.Get("claude-sonnet-4-6")
	require.True(t, ok)
	require.Equal(t, 200000, def.ContextWindow)
}

func TestGatewayEmptyCatalogDoesNotInventModels(t *testing.T) {
	t.Setenv("EA_MODELS_FILE", filepath.Join(t.TempDir(), "missing.json"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Provider, cfg.OpenAIBaseURL = "openai", server.URL
	require.Empty(t, NewCodingApplication(cfg).AvailableModels())
}
