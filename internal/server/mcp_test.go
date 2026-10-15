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
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/mcp"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestCommandCatalogOmitsTerminalOnlyActions(t *testing.T) {
	registry := slashcmd.NewRegistry()
	for _, name := range []string{"clear", "quit", "exit", "workflow", "mcp"} {
		registry.Register(slashcmd.Command{Name: name, Description: name, Subcommands: []slashcmd.Subcommand{{Name: "list", Description: "list"}}})
	}
	w := httptest.NewRecorder()
	(&Server{slashCmds: registry}).listCommands(w, httptest.NewRequest(http.MethodGet, "/commands", nil))
	var data struct {
		Commands []struct {
			Name        string              `json:"name"`
			Subcommands []map[string]string `json:"subcommands"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &data))
	require.Len(t, data.Commands, 2)
	require.Equal(t, "mcp", data.Commands[0].Name)
	require.Equal(t, "list", data.Commands[0].Subcommands[0]["name"])
	require.Equal(t, "workflow", data.Commands[1].Name)
	require.Contains(t, registry.Names(), "clear", "TUI registry still retains terminal actions")
}

func TestCreatedSessionPersistsDefaultWorkspaceForMCPAndDesktop(t *testing.T) {
	srv, _, _ := serverMCPTestServer(t)
	w := serverMCPRequest(t, srv, http.MethodPost, "/sessions", map[string]any{}, true)
	require.Equal(t, http.StatusOK, w.Code)
	var created SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	workspace, _ := srv.app.SessionManager().Metadata(created.ID)
	require.Equal(t, srv.app.Config().Workspace, workspace)
	w = serverMCPRequest(t, srv, http.MethodGet, "/sessions", nil, true)
	var listed []struct {
		Workspace string `json:"workspace"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed, 1)
	require.Equal(t, workspace, listed[0].Workspace)
}

func serverMCPFixture(t *testing.T) string {
	t.Helper()
	server := protocol.NewServer(&protocol.Implementation{Name: "fixture", Version: "1"}, nil)
	for _, name := range []string{"read_one", "write_one"} {
		server.AddTool(&protocol.Tool{Name: name, Description: name, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
			return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: "ok"}}}, nil
		})
	}
	hs := httptest.NewServer(protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil))
	t.Cleanup(func() {
		hs.CloseClientConnections()
		hs.Close()
	})
	return hs.URL
}

func serverMCPTestServer(t *testing.T) (*Server, string, chan []string) {
	t.Helper()
	seenTools := make(chan []string, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		names := []string{}
		for _, tool := range request.Tools {
			names = append(names, tool.Function.Name)
		}
		seenTools <- names
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(provider.Close)
	cfg := config.Default()
	dir := t.TempDir()
	cfg.DataDir, cfg.Workspace, cfg.MCPConfigPath = filepath.Join(dir, "data"), filepath.Join(dir, "workspace"), filepath.Join(dir, "mcp.json")
	require.NoError(t, os.MkdirAll(cfg.Workspace, 0700))
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	srv := New(application, nil)
	srv.SetAPIKey("test-admin-key")
	t.Cleanup(func() { srv.cancel(); application.Close() })
	return srv, cfg.MCPConfigPath, seenTools
}

func serverMCPRequest(t *testing.T, srv *Server, method, path string, body any, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := localReq(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if authorized {
		req.Header.Set("Authorization", "Bearer test-admin-key")
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestMCPRESTAuthenticationAndPartialUpdatesPreservePrivateConfiguration(t *testing.T) {
	srv, path, _ := serverMCPTestServer(t)
	url := serverMCPFixture(t)
	disabled := false
	body := mcp.MCPServerConfig{URL: url, Headers: map[string]string{"Authorization": "Bearer fake-private-token"}, Env: map[string]string{"PRIVATE_KEY": "fake-env-secret"}, Enabled: &disabled, Trust: false}
	w := serverMCPRequest(t, srv, http.MethodPut, "/mcp/servers/test", body, false)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "failed authentication must not create configuration")
	w = serverMCPRequest(t, srv, http.MethodPut, "/mcp/servers/test", body, true)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serverMCPRequest(t, srv, http.MethodPut, "/mcp/servers/test", map[string]any{"description": "renamed", "includeTools": []string{"read_*"}}, true)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	cfg, exists := srv.app.MCP("").ServerConfig("test", "user")
	require.True(t, exists)
	require.Equal(t, "Bearer fake-private-token", cfg.Headers["Authorization"])
	require.Equal(t, "fake-env-secret", cfg.Env["PRIVATE_KEY"])
	require.Equal(t, "renamed", cfg.Description)
	list := serverMCPRequest(t, srv, http.MethodGet, "/mcp", nil, true)
	require.Equal(t, http.StatusOK, list.Code)
	require.NotContains(t, list.Body.String(), "fake-private-token")
	require.NotContains(t, list.Body.String(), "fake-env-secret")
	w = serverMCPRequest(t, srv, http.MethodPut, "/mcp/servers/test", map[string]any{"headers": map[string]string{}, "env": nil}, true)
	require.Equal(t, http.StatusOK, w.Code)
	cfg, _ = srv.app.MCP("").ServerConfig("test", "user")
	require.Empty(t, cfg.Headers)
	require.Empty(t, cfg.Env)
}

func TestMCPRESTToolPolicyRejectsWildcardOverrideAndInjectsNextPrompt(t *testing.T) {
	srv, _, seenTools := serverMCPTestServer(t)
	url := serverMCPFixture(t)
	w := serverMCPRequest(t, srv, http.MethodPut, "/mcp/servers/test", mcp.MCPServerConfig{URL: url, Trust: true, ExcludeTools: []string{"write_*"}}, true)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, srv.app.MCP("").WaitReady(ctx))
	w = serverMCPRequest(t, srv, http.MethodPost, "/mcp/servers/test/tools/write_one", map[string]bool{"enabled": true}, true)
	require.Equal(t, http.StatusConflict, w.Code)
	w = serverMCPRequest(t, srv, http.MethodPost, "/mcp/servers/test/tools/read_one", map[string]bool{"enabled": false}, true)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, srv.app.MCP("").WaitReady(ctx))
	first, _, err := srv.startRun("", "first tools", "tools-first")
	require.NoError(t, err)
	_, err = srv.waitRun(ctx, first)
	require.NoError(t, err)
	names := <-seenTools
	require.NotContains(t, names, "mcp__test__read_one")
	require.NotContains(t, names, "mcp__test__write_one")
	w = serverMCPRequest(t, srv, http.MethodPost, "/mcp/servers/test/tools/read_one", map[string]bool{"enabled": true}, true)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, srv.app.MCP("").WaitReady(ctx))
	second, _, err := srv.startRun(first.SessionID, "second tools", "tools-second")
	require.NoError(t, err)
	_, err = srv.waitRun(ctx, second)
	require.NoError(t, err)
	names = <-seenTools
	require.Contains(t, names, "mcp__test__read_one", "existing sessions discover changes at the next prompt")
	require.NotContains(t, names, "mcp__test__write_one")
}

func TestMCPRESTUntrustedProjectConfigurationIsNotLoadedOrEditable(t *testing.T) {
	srv, _, _ := serverMCPTestServer(t)
	workspace := srv.app.Config().Workspace
	path := filepath.Join(workspace, ".easyagent", "mcp.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{"project":{"command":"must-not-execute","env":{"PRIVATE_KEY":"hidden-project-secret"}}}}`), 0600))
	w := serverMCPRequest(t, srv, http.MethodGet, "/mcp", nil, true)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "must-not-execute")
	require.NotContains(t, w.Body.String(), "hidden-project-secret")
	w = serverMCPRequest(t, srv, http.MethodPut, "/mcp/servers/project?scope=project", map[string]any{"command": "changed-command"}, true)
	require.Equal(t, http.StatusForbidden, w.Code)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "must-not-execute")
	require.NotContains(t, string(data), "changed-command")
	require.True(t, strings.Contains(w.Body.String(), "授权"))
}
