package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func testServer() *protocol.Server {
	server := protocol.NewServer(&protocol.Implementation{Name: "test-mcp", Version: "1"}, nil)
	server.AddTool(&protocol.Tool{Name: "echo", Description: "Echo", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: string(req.Params.Arguments)}, &protocol.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"}}, StructuredContent: map[string]any{"ok": true}}, nil
	})
	return server
}
func configFile(t *testing.T, path string, servers map[string]MCPServerConfig) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"mcpServers": servers, "unrelated": "keep"})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, raw, 0600))
}
func managerFor(t *testing.T, cfg MCPServerConfig) *Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	configFile(t, path, map[string]MCPServerConfig{"test": cfg})
	m := NewManager(Options{UserConfigPath: path, Workspace: dir})
	require.Empty(t, m.Load())
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func waitConnected(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, m.WaitReady(ctx))
	require.Equal(t, StatusConnected, m.Servers()[0].State, m.Servers())
}

func TestStreamableHTTPConsecutiveRequestsProgressCancellationAndRefresh(t *testing.T) {
	server := testServer()
	started, canceled := make(chan struct{}, 1), make(chan struct{}, 1)
	server.AddTool(&protocol.Tool{Name: "wait", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		if err := req.Session.NotifyProgress(ctx, &protocol.ProgressNotificationParams{ProgressToken: req.Params.GetProgressToken(), Progress: 1, Total: 2, Message: "Working"}); err != nil {
			t.Errorf("progress failed: %v token=%v", err, req.Params.GetProgressToken())
		}
		started <- struct{}{}
		<-ctx.Done()
		canceled <- struct{}{}
		return nil, ctx.Err()
	})
	httpServer := httptest.NewServer(protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	m := managerFor(t, MCPServerConfig{URL: httpServer.URL, Trust: true, Timeout: 2000})
	m.Start(context.Background())
	waitConnected(t, m)
	tools := m.AllTools()
	require.Len(t, tools, 2)
	echo := tools[0]
	require.Contains(t, echo.Name(), "echo")
	for i := 0; i < 5; i++ {
		result, err := echo.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)), nil)
		require.NoError(t, err)
		require.False(t, result.IsError, result)
		require.Contains(t, result.Content, `"ok":true`)
		require.Contains(t, result.Content, "image/png")
		details := result.Details.(*protocol.CallToolResult)
		require.Len(t, details.Content, 2)
	}
	update := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan agent.ToolResult, 1)
	go func() {
		result, _ := tools[1].Execute(ctx, json.RawMessage(`{}`), func(partial agent.PartialResult) { update <- partial.Content })
		done <- result
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	select {
	case text := <-update:
		require.Equal(t, "Working", text)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("no progress received")
	}
	cancel()
	select {
	case result := <-done:
		require.True(t, result.IsError)
	case <-time.After(time.Second):
		t.Fatal("cancel did not settle tool")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("cancel notification did not reach server")
	}
	server.AddTool(&protocol.Tool{Name: "added", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: "new"}}}, nil
	})
	require.Eventually(t, func() bool { return len(m.AllTools()) == 3 }, 2*time.Second, 10*time.Millisecond)
	server.RemoveTools("added")
	require.Eventually(t, func() bool { return len(m.AllTools()) == 2 }, 2*time.Second, 10*time.Millisecond)
}

func TestStreamableJSONAndLegacySSE(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			server := testServer()
			var handler http.Handler
			if transport == "sse" {
				handler = protocol.NewSSEHandler(func(*http.Request) *protocol.Server { return server }, nil)
			} else {
				handler = protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, &protocol.StreamableHTTPOptions{JSONResponse: true})
			}
			hs := httptest.NewServer(handler)
			t.Cleanup(hs.Close)
			m := managerFor(t, MCPServerConfig{Type: transport, URL: hs.URL, Trust: true})
			m.Start(context.Background())
			waitConnected(t, m)
			for i := 0; i < 3; i++ {
				result, err := m.AllTools()[0].Execute(context.Background(), json.RawMessage(`{}`), nil)
				require.NoError(t, err)
				require.False(t, result.IsError, result)
			}
		})
	}
}

func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("EA_MCP_TEST_HELPER") != "1" {
		return
	}
	server := testServer()
	server.AddTool(&protocol.Tool{Name: "pid", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: strconv.Itoa(os.Getpid())}}}, nil
	})
	server.AddTool(&protocol.Tool{Name: "exit", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		go func() { time.Sleep(30 * time.Millisecond); os.Exit(0) }()
		return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: "bye"}}}, nil
	})
	if server.Run(context.Background(), &protocol.StdioTransport{}) != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
func stdioConfig(t *testing.T) MCPServerConfig {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return MCPServerConfig{Command: exe, Args: []string{"-test.run=^TestMCPStdioHelperProcess$"}, Env: map[string]string{"EA_MCP_TEST_HELPER": "1"}, Trust: true}
}
func findTool(t *testing.T, m *Manager, name string) agent.Tool {
	t.Helper()
	for _, tool := range m.AllTools() {
		if tool.(*MCPToolAdapter).ToolName() == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}
func TestStdioDisconnectReconnectAndClose(t *testing.T) {
	m := managerFor(t, stdioConfig(t))
	m.Start(context.Background())
	waitConnected(t, m)
	pidTool := findTool(t, m, "pid")
	first, err := pidTool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.False(t, first.IsError)
	_, err = findTool(t, m, "exit").Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return m.Servers()[0].State == StatusDisconnected }, 3*time.Second, 10*time.Millisecond)
	second, err := pidTool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.False(t, second.IsError, second)
	require.NotEqual(t, first.Content, second.Content)
	require.NoError(t, m.Close())
	require.Empty(t, m.AllTools())
	require.Empty(t, m.Servers()[0].Tools)
	require.Equal(t, StatusDisconnected, m.Servers()[0].State)
	require.Error(t, m.WaitReady(context.Background()))
	result, err := pidTool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.True(t, result.IsError)
}

func TestBackgroundFailureIsolationAndFilters(t *testing.T) {
	server := testServer()
	hs := httptest.NewServer(protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil))
	t.Cleanup(hs.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	configFile(t, path, map[string]MCPServerConfig{"good": {URL: hs.URL, IncludeTools: []string{"e*"}, Trust: false}, "bad": {Command: filepath.Join(dir, "missing")}, "invalid": {}})
	m := NewManager(Options{UserConfigPath: path})
	require.Len(t, m.Load(), 1)
	t.Cleanup(func() { _ = m.Close() })
	start := time.Now()
	m.Start(context.Background())
	require.Less(t, time.Since(start), 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, m.WaitReady(ctx))
	require.Len(t, m.AllTools(), 1)
	adapter := m.AllTools()[0].(*MCPToolAdapter)
	require.False(t, adapter.IsConcurrencySafe(nil))
	require.True(t, adapter.RequiresConfirmationAvailable())
	_, needs := adapter.RequiresConfirmation(nil)
	require.True(t, needs)
	cfg, ok := m.ServerConfig("good", "user")
	require.True(t, ok)
	cfg.IncludeTools = []string{"not-echo"}
	require.NoError(t, m.PutServer(ctx, "good", cfg, "user"))
	require.NoError(t, m.WaitReady(ctx))
	require.Empty(t, m.AllTools())
}

func TestConfigTrustScopeAndSecretIsolation(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user", "mcp.json")
	workspace := filepath.Join(dir, "workspace")
	project := filepath.Join(workspace, ".easyagent", "mcp.json")
	configFile(t, user, map[string]MCPServerConfig{"same": {Command: "user-server", Env: map[string]string{"SECRET": "user-secret"}}, "http": {URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer secret"}}})
	configFile(t, project, map[string]MCPServerConfig{"same": {Command: "project-server"}, "local": {Command: "never-executed"}})
	m := NewManager(Options{UserConfigPath: user, Workspace: workspace})
	require.Empty(t, m.Load())
	require.Len(t, m.Servers(), 2)
	cfg, ok := m.ServerConfig("same", "user")
	require.True(t, ok)
	require.Equal(t, "user-server", cfg.Command)
	raw, _ := json.Marshal(m.Servers())
	require.NotContains(t, string(raw), "user-secret")
	require.NotContains(t, string(raw), "Bearer secret")
	require.Error(t, m.PutServer(context.Background(), "local", MCPServerConfig{Command: "anything"}, "project"))
	require.NoError(t, m.SetProjectTrusted(context.Background(), true))
	cfg, ok = m.ServerConfig("same", "project")
	require.True(t, ok)
	require.Equal(t, "project-server", cfg.Command)
	shadowed, ok := m.ServerConfig("same", "user")
	require.True(t, ok)
	require.Equal(t, "user-secret", shadowed.Env["SECRET"])
	shadowed.Command = "updated-user-server"
	require.NoError(t, m.PutServer(context.Background(), "same", shadowed, "user"))
	active, _ := m.ServerConfig("same", "project")
	require.Equal(t, "project-server", active.Command)
	require.NoError(t, m.RemoveServer(context.Background(), "same", "project"))
	cfg, ok = m.ServerConfig("same", "user")
	require.True(t, ok)
	require.Equal(t, "updated-user-server", cfg.Command)
	require.NoError(t, m.SetEnabled(context.Background(), "http", false))
	cfg, _ = m.ServerConfig("http", "user")
	require.Equal(t, "Bearer secret", cfg.Headers["Authorization"])
	require.False(t, *cfg.Enabled)
	data, err := os.ReadFile(user)
	require.NoError(t, err)
	require.Contains(t, string(data), "unrelated")
	info, err := os.Stat(user)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	copyCfg, _ := m.ServerConfig("http", "user")
	copyCfg.Headers["Authorization"] = "changed"
	again, _ := m.ServerConfig("http", "user")
	require.Equal(t, "Bearer secret", again.Headers["Authorization"])
}

func TestMalformedConfigAndNames(t *testing.T) {
	for _, cfg := range []MCPServerConfig{{}, {Command: "x", URL: "https://example.com"}, {URL: "file:///tmp/x"}, {URL: "http://user:password@host/x"}, {URL: "https://example.com", Type: "unknown"}, {URL: "https://example.com", Timeout: -1}, {URL: "https://example.com", Headers: map[string]string{"X": "a\r\nb"}}} {
		require.Error(t, cfg.Validate())
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{"good":{"command":"x"},"bad":{"command":1}}}`), 0600))
	entries, issues := LoadMergedConfig(path, "", false)
	require.Len(t, entries, 1)
	require.Len(t, issues, 1)
	m := NewManager(Options{UserConfigPath: path})
	m.Load()
	a := newToolAdapter(m, "test", &protocol.Tool{Name: "a.b"}, MCPServerConfig{})
	b := newToolAdapter(m, "test", &protocol.Tool{Name: "a_b"}, MCPServerConfig{})
	require.NotEqual(t, a.Name(), b.Name())
	require.NotContains(t, a.Name(), ".")
	long := newToolAdapter(m, strings.Repeat("server", 10), &protocol.Tool{Name: strings.Repeat("tool", 30)}, MCPServerConfig{})
	require.LessOrEqual(t, len(long.Name()), 64, "provider tool names must fit their 64-byte limit")
	crafted := strings.TrimPrefix(a.Name(), "mcp__test__")
	require.Error(t, validateToolNames("test", []*protocol.Tool{{Name: "a.b"}, {Name: crafted}}))
	require.Error(t, func() error { _, err := a.Validate(json.RawMessage(`[]`)); return err }())
	require.True(t, isToolEnabled("read_file", MCPServerConfig{IncludeTools: []string{"read_*"}}))
	require.False(t, isToolEnabled("read_file", MCPServerConfig{IncludeTools: []string{"read_*"}, ExcludeTools: []string{"*file"}}))
}

func TestToolRequestNotAutomaticallyRetried(t *testing.T) {
	var calls atomic.Int32
	server := testServer()
	server.AddTool(&protocol.Tool{Name: "fail", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		calls.Add(1)
		return nil, fmt.Errorf("uncertain operation result")
	})
	hs := httptest.NewServer(protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil))
	t.Cleanup(hs.Close)
	m := managerFor(t, MCPServerConfig{URL: hs.URL, Trust: true})
	m.Start(context.Background())
	waitConnected(t, m)
	result, err := findTool(t, m, "fail").Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Equal(t, int32(1), calls.Load())
	require.False(t, strings.Contains(result.Content, "Bearer"))
}
