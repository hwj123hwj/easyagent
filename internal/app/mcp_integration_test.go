package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/mcp"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func appMCPFixture(t *testing.T, name string) string {
	t.Helper()
	server := protocol.NewServer(&protocol.Implementation{Name: name, Version: "1"}, nil)
	server.AddTool(&protocol.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: name}}}, nil
	})
	httpServer := httptest.NewServer(protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

func appMCPConfig(t *testing.T, path string, servers map[string]mcp.MCPServerConfig) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
}

func appMCPTestApplication(t *testing.T, workspace, userPath, dataDir string) *App {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[]}`) }))
	t.Cleanup(provider.Close)
	cfg := config.Default()
	cfg.DataDir, cfg.Workspace, cfg.MCPConfigPath = dataDir, workspace, userPath
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
	a, err := New(AppOptions{Config: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { a.Close() })
	return a
}

func appMCPReady(t *testing.T, manager *mcp.Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, manager.WaitReady(ctx))
	for _, server := range manager.Servers() {
		require.Equal(t, mcp.StatusConnected, server.State, server)
	}
}

func TestApplicationMCPProjectTrustScopesAndRestoredWorkspace(t *testing.T) {
	userURL, projectURL := appMCPFixture(t, "user_tool"), appMCPFixture(t, "project_tool")
	dir, one, two := t.TempDir(), t.TempDir(), t.TempDir()
	userPath := filepath.Join(dir, "mcp.json")
	appMCPConfig(t, userPath, map[string]mcp.MCPServerConfig{"shared": {URL: userURL, Trust: true}})
	appMCPConfig(t, filepath.Join(one, ".easyagent", "mcp.json"), map[string]mcp.MCPServerConfig{"shared": {URL: projectURL, Trust: false}})
	a := appMCPTestApplication(t, two, userPath, filepath.Join(dir, "data"))
	first, second := a.MCP(one), a.MCP(two)
	appMCPReady(t, first)
	appMCPReady(t, second)
	require.Equal(t, "user", first.Servers()[0].Scope, "opening a workspace does not execute project configuration")
	require.Equal(t, "mcp__shared__user_tool", first.AllTools()[0].Name())
	require.NoError(t, a.SetMCPProjectTrust(context.Background(), one, true))
	appMCPReady(t, first)
	require.Equal(t, "project", first.Servers()[0].Scope)
	require.Equal(t, "mcp__shared__project_tool", first.AllTools()[0].Name())
	require.Equal(t, "mcp__shared__user_tool", second.AllTools()[0].Name(), "project shadowing cannot leak to another workspace")
	cfg := a.Config()
	cfg.Workspace = one
	sess, err := a.SessionStore().Create(context.Background(), runtime.AgentSessionOptions{Config: cfg}, a.SessionDeps())
	require.NoError(t, err)
	require.NoError(t, a.SessionManager().SaveMeta(sess.SessionID(), one, "coding"))
	require.NoError(t, sess.RefreshTools(context.Background()))
	require.Contains(t, sess.ToolNames(), "mcp__shared__project_tool")
	require.NotContains(t, sess.ToolNames(), "mcp__shared__user_tool")
	id := sess.SessionID()
	require.NoError(t, a.SessionStore().Delete(id))
	restored, err := a.LoadSession(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, one, restored.Workspace(), "session metadata owns its workspace even when app default differs")
	require.Contains(t, restored.ToolNames(), "mcp__shared__project_tool")
	require.NoError(t, a.SetMCPProjectTrust(context.Background(), one, false))
	appMCPReady(t, first)
	require.NoError(t, restored.RefreshTools(context.Background()))
	require.Contains(t, restored.ToolNames(), "mcp__shared__user_tool")
	require.NotContains(t, restored.ToolNames(), "mcp__shared__project_tool")
	// A new App restores the persisted project trust decision, rather than trusting by pathname alone.
	require.NoError(t, a.SetMCPProjectTrust(context.Background(), one, true))
	b := appMCPTestApplication(t, two, userPath, filepath.Join(dir, "another-data"))
	require.True(t, b.MCPProjectTrusted(one))
	require.False(t, b.MCPProjectTrusted(two))
	appMCPReady(t, b.MCP(one))
	require.Equal(t, "project", b.MCP(one).Servers()[0].Scope)
}

func TestApplicationMCPCloseDisposesEveryWorkspaceManager(t *testing.T) {
	url := appMCPFixture(t, "cleanup")
	dir, one, two := t.TempDir(), t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	appMCPConfig(t, path, map[string]mcp.MCPServerConfig{"cleanup": {URL: url, Trust: true}})
	a := appMCPTestApplication(t, one, path, filepath.Join(dir, "data"))
	first, second := a.MCP(one), a.MCP(two)
	appMCPReady(t, first)
	appMCPReady(t, second)
	require.NoError(t, a.Close())
	require.Empty(t, first.AllTools())
	require.Empty(t, second.AllTools())
}

func TestApplicationMCPLeaseCoversPreparationAndReleasesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	a := appMCPTestApplication(t, dir, filepath.Join(dir, "mcp.json"), filepath.Join(dir, "data"))
	deps := a.SessionDeps()
	entered, finish := make(chan struct{}), make(chan struct{})
	deps.PrepareTools = func(context.Context, string) error {
		close(entered)
		<-finish
		return errors.New("fixture preparation failed")
	}
	sess, err := a.SessionStore().Create(context.Background(), runtime.AgentSessionOptions{Config: a.Config()}, deps)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { _, err := sess.PromptStream(context.Background(), "no execution"); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("preparation was not entered")
	}
	release, err := a.BeginMCPEdit()
	require.Error(t, err, "preparation must already protect MCP sessions from teardown")
	require.Nil(t, release)
	close(finish)
	require.ErrorContains(t, <-result, "fixture preparation failed")
	release, err = a.BeginMCPEdit()
	require.NoError(t, err, "preparation failure must release the lease")
	_, err = sess.PromptStream(context.Background(), "blocked by mutation")
	require.ErrorContains(t, err, "配置正在更新")
	release()
	release, err = a.BeginMCPEdit()
	require.NoError(t, err, "failed prompt admission must not leak a read lease")
	release()
}

func TestApplicationMCPLeaseCoversActualStreamUntilCancellation(t *testing.T) {
	started := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	cfg := config.Default()
	dir := t.TempDir()
	cfg.DataDir, cfg.Workspace, cfg.MCPConfigPath = filepath.Join(dir, "data"), dir, filepath.Join(dir, "mcp.json")
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
	a, err := New(AppOptions{Config: cfg})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.Close(); provider.Close() })
	sess, err := a.NewSession(context.Background())
	require.NoError(t, err)
	stream, err := sess.PromptStream(ctx, "hold stream")
	require.NoError(t, err)
	drained := make(chan struct{})
	go func() {
		for range stream {
		}
		close(drained)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	release, err := a.BeginMCPEdit()
	require.Error(t, err)
	require.Nil(t, release)
	require.Error(t, a.SetMCPProjectTrust(context.Background(), dir, true), "project authorization must use the same exclusive gate")
	require.False(t, a.MCPProjectTrusted(dir))
	cancel()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled stream did not drain")
	}
	release, err = a.BeginMCPEdit()
	require.NoError(t, err, "finishing a stream must release its integration lease")
	release()
	require.NoError(t, a.SetMCPProjectTrust(context.Background(), dir, true))
}
