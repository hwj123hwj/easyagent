package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

const DefaultTimeout = 60 * time.Second
const ConnectTimeout = 15 * time.Second

type ServerStatus string

const (
	StatusDisconnected ServerStatus = "disconnected"
	StatusConnecting   ServerStatus = "connecting"
	StatusConnected    ServerStatus = "connected"
	StatusFailed       ServerStatus = "failed"
	StatusNeedsAuth    ServerStatus = "needs_auth"
	StatusDisabled     ServerStatus = "disabled"
)

type Options struct {
	Workspace      string
	UserConfigPath string
	AuthStorePath  string
	ProjectTrusted bool
}

// ServerInfo is safe to expose to a presentation: credentials and command
// arguments/environment are intentionally absent.
type ServerInfo struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Source      string       `json:"source"`
	Scope       string       `json:"scope"`
	Transport   string       `json:"transport"`
	State       ServerStatus `json:"state"`
	Error       string       `json:"error,omitempty"`
	ToolCount   int          `json:"tool_count"`
	Trusted     bool         `json:"trusted"`
	Enabled     bool         `json:"enabled"`
	Tools       []ToolInfo   `json:"tools"`
}
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type managedServer struct {
	entry   ConfigEntry
	op      sync.Mutex
	session *protocol.ClientSession
	client  *connection
	state   ServerStatus
	err     string
	tools   []*protocol.Tool
	ready   chan struct{}
	cancel  context.CancelFunc
}

type Manager struct {
	mu           sync.RWMutex
	mutation     sync.Mutex
	options      Options
	servers      map[string]*managedServer
	issues       []ConfigIssue
	listeners    map[uint64]func()
	nextListener uint64
	ctx          context.Context
	cancel       context.CancelFunc
	closed       bool
	auth         *authStore
	logins       map[string]*pendingLogin
}

func NewManager(opts Options) *Manager {
	home, _ := os.UserHomeDir()
	if opts.UserConfigPath == "" {
		opts.UserConfigPath = filepath.Join(home, ".easyagent", "mcp.json")
	}
	if opts.AuthStorePath == "" {
		opts.AuthStorePath = filepath.Join(filepath.Dir(opts.UserConfigPath), "mcp-auth.json")
	}
	return &Manager{options: opts, servers: map[string]*managedServer{}, listeners: map[uint64]func(){}, auth: &authStore{path: opts.AuthStorePath}, logins: map[string]*pendingLogin{}}
}

// Load replaces the configuration and releases connections from the old
// generation. Invalid entries never prevent independent servers from loading.
func (m *Manager) Load() []ConfigIssue {
	m.mutation.Lock()
	defer m.mutation.Unlock()
	return m.load()
}
func (m *Manager) load() []ConfigIssue {
	m.mu.RLock()
	opts := m.options
	m.mu.RUnlock()
	entries, issues := LoadMergedConfig(opts.UserConfigPath, opts.Workspace, opts.ProjectTrusted)
	newServers := map[string]*managedServer{}
	for _, entry := range entries {
		state := StatusDisconnected
		if !entry.Config.enabled() {
			state = StatusDisabled
		}
		newServers[entry.Name] = &managedServer{entry: entry, state: state}
	}
	m.mu.Lock()
	old := m.servers
	m.servers = newServers
	m.issues = issues
	ctx := m.ctx
	closed := m.closed
	m.mu.Unlock()
	for _, server := range old {
		m.stop(server)
	}
	m.changed()
	if ctx != nil && !closed {
		m.startServers(ctx)
	}
	return append([]ConfigIssue(nil), issues...)
}
func (m *Manager) Issues() []ConfigIssue {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]ConfigIssue(nil), m.issues...)
}

// ServerConfig is for authenticated configuration editing. It returns a deep
// copy and must never be serialized directly in a public status response.
func (m *Manager) ServerConfig(name, scope string) (MCPServerConfig, bool) {
	m.mu.RLock()
	path, err := m.configPath(scope)
	m.mu.RUnlock()
	if err != nil {
		return MCPServerConfig{}, false
	}
	return scopedConfig(path, name)
}

func scopedConfig(path, name string) (MCPServerConfig, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return MCPServerConfig{}, false
	}
	var file struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return MCPServerConfig{}, false
	}
	entry, ok := file.Servers[name]
	if !ok {
		return MCPServerConfig{}, false
	}
	var cfg MCPServerConfig
	if json.Unmarshal(entry, &cfg) != nil {
		return MCPServerConfig{}, false
	}
	return cfg, true
}

// Start does not wait for network connections or subprocess initialization.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.closed || m.ctx != nil {
		m.mu.Unlock()
		return
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	runCtx := m.ctx
	m.mu.Unlock()
	m.startServers(runCtx)
	go func() { <-runCtx.Done(); _ = m.Close() }()
}
func (m *Manager) startServers(ctx context.Context) {
	m.mu.RLock()
	names := []string{}
	for name, srv := range m.servers {
		if srv.entry.Config.enabled() {
			names = append(names, name)
		}
	}
	m.mu.RUnlock()
	for _, name := range names {
		go func(name string) { _ = m.ConnectOne(ctx, name) }(name)
	}
}

// WaitReady is bounded by the caller's context and never blocks startup UI.
func (m *Manager) WaitReady(ctx context.Context) error {
	for {
		m.mu.RLock()
		if m.closed {
			m.mu.RUnlock()
			return errors.New("MCP manager is closed")
		}
		pending := false
		for _, srv := range m.servers {
			if srv.entry.Config.enabled() && (srv.state == StatusDisconnected || srv.state == StatusConnecting) {
				pending = true
				break
			}
		}
		m.mu.RUnlock()
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (m *Manager) Subscribe(fn func()) func() {
	m.mu.Lock()
	m.nextListener++
	id := m.nextListener
	m.listeners[id] = fn
	m.mu.Unlock()
	return func() { m.mu.Lock(); delete(m.listeners, id); m.mu.Unlock() }
}
func (m *Manager) changed() {
	m.mu.RLock()
	callbacks := make([]func(), 0, len(m.listeners))
	for _, fn := range m.listeners {
		callbacks = append(callbacks, fn)
	}
	m.mu.RUnlock()
	for _, fn := range callbacks {
		fn()
	}
}

func (m *Manager) Servers() []ServerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ServerInfo, 0, len(m.servers))
	for name, srv := range m.servers {
		cfg := srv.entry.Config
		info := ServerInfo{Name: name, Description: cfg.Description, Source: srv.entry.Source, Scope: srv.entry.Scope, Transport: cfg.Transport(), State: srv.state, Error: srv.err, Trusted: cfg.Trust, Enabled: cfg.enabled(), Tools: []ToolInfo{}}
		for _, tool := range srv.tools {
			enabled := isToolEnabled(tool.Name, cfg)
			if enabled {
				info.ToolCount++
			}
			info.Tools = append(info.Tools, ToolInfo{Name: tool.Name, Description: tool.Description, Enabled: enabled})
		}
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func (m *Manager) server(name string) (*managedServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, errors.New("MCP manager is closed")
	}
	srv := m.servers[name]
	if srv == nil {
		return nil, errors.New("MCP server not found")
	}
	return srv, nil
}

func (m *Manager) ConnectOne(ctx context.Context, name string) error {
	srv, err := m.server(name)
	if err != nil {
		return err
	}
	srv.op.Lock()
	defer srv.op.Unlock()
	m.mu.Lock()
	if m.servers[name] != srv || m.closed {
		m.mu.Unlock()
		return errors.New("MCP configuration changed")
	}
	if !srv.entry.Config.enabled() {
		m.mu.Unlock()
		return errors.New("MCP server is disabled")
	}
	if srv.session != nil && srv.state == StatusConnected {
		m.mu.Unlock()
		return nil
	}
	connectCtx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	srv.cancel = cancel
	srv.state = StatusConnecting
	srv.err = ""
	ready := make(chan struct{})
	srv.ready = ready
	entry := srv.entry
	m.mu.Unlock()
	m.changed()
	defer cancel()
	defer close(ready)
	conn := newConnection(m, entry)
	session, err := conn.connect(connectCtx, m.options.Workspace)
	var tools []*protocol.Tool
	if err == nil {
		tools, err = listTools(connectCtx, session)
		if err == nil {
			err = validateToolNames(name, tools)
		}
	}
	if err != nil {
		if session != nil {
			_ = session.Close()
		}
		m.mu.Lock()
		if m.servers[name] == srv {
			srv.cancel = nil
			srv.client = conn
			srv.state = StatusFailed
			srv.err = conn.safeError(err)
			if conn.needsAuth(err) {
				srv.state = StatusNeedsAuth
			}
		}
		m.mu.Unlock()
		m.changed()
		return errors.New("MCP connection failed")
	}
	m.mu.Lock()
	if m.closed || m.servers[name] != srv {
		m.mu.Unlock()
		_ = session.Close()
		return errors.New("MCP configuration changed")
	}
	srv.session = session
	srv.client = conn
	srv.tools = tools
	srv.state = StatusConnected
	srv.cancel = nil
	m.mu.Unlock()
	m.changed()
	go func() {
		err := session.Wait()
		m.mu.Lock()
		if m.servers[name] == srv && srv.session == session {
			srv.session = nil
			srv.state = StatusDisconnected
			srv.err = conn.safeError(err)
		}
		m.mu.Unlock()
		m.changed()
	}()
	return nil
}
func listTools(ctx context.Context, session *protocol.ClientSession) ([]*protocol.Tool, error) {
	if session.InitializeResult().Capabilities.Tools == nil {
		return nil, nil
	}
	var tools []*protocol.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}
func validateToolNames(server string, tools []*protocol.Tool) error {
	names := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" {
			return errors.New("MCP server advertised an empty tool name")
		}
		name := newToolAdapter(nil, server, tool, MCPServerConfig{}).Name()
		if names[name] {
			return errors.New("MCP server advertised colliding tool names")
		}
		names[name] = true
	}
	return nil
}
func (m *Manager) RefreshTools(ctx context.Context, name string) error {
	srv, err := m.server(name)
	if err != nil {
		return err
	}
	srv.op.Lock()
	defer srv.op.Unlock()
	m.mu.RLock()
	session := srv.session
	m.mu.RUnlock()
	if session == nil {
		return errors.New("MCP server is disconnected")
	}
	ctx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()
	tools, err := listTools(ctx, session)
	if err == nil {
		err = validateToolNames(name, tools)
	}
	if err != nil {
		return errors.New("cannot refresh MCP tools")
	}
	m.mu.Lock()
	if m.servers[name] == srv && srv.session == session {
		srv.tools = tools
	}
	m.mu.Unlock()
	m.changed()
	return nil
}
func (m *Manager) Reconnect(ctx context.Context, name string) error {
	srv, err := m.server(name)
	if err != nil {
		return err
	}
	m.stop(srv)
	return m.ConnectOne(ctx, name)
}
func (m *Manager) stop(srv *managedServer) {
	m.mu.Lock()
	if srv.cancel != nil {
		srv.cancel()
	}
	m.mu.Unlock()
	srv.op.Lock()
	defer srv.op.Unlock()
	m.mu.Lock()
	session := srv.session
	srv.session = nil
	if srv.entry.Config.enabled() {
		srv.state = StatusDisconnected
	} else {
		srv.state = StatusDisabled
	}
	m.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	servers := []*managedServer{}
	for _, srv := range m.servers {
		srv.tools = nil
		servers = append(servers, srv)
	}
	for _, login := range m.logins {
		login.cancel()
	}
	m.logins = map[string]*pendingLogin{}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		go func(srv *managedServer) { defer wg.Done(); m.stop(srv) }(srv)
	}
	wg.Wait()
	m.changed()
	return nil
}

func (m *Manager) AllTools() []agent.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []agent.Tool{}
	if m.closed {
		return result
	}
	for name, srv := range m.servers {
		if !srv.entry.Config.enabled() {
			continue
		}
		for _, tool := range srv.tools {
			if isToolEnabled(tool.Name, srv.entry.Config) {
				result = append(result, newToolAdapter(m, name, tool, srv.entry.Config))
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}
func isToolEnabled(name string, cfg MCPServerConfig) bool {
	for _, pattern := range cfg.ExcludeTools {
		match, _ := filepath.Match(pattern, name)
		if match {
			return false
		}
	}
	if len(cfg.IncludeTools) == 0 {
		return true
	}
	for _, pattern := range cfg.IncludeTools {
		match, _ := filepath.Match(pattern, name)
		if match {
			return true
		}
	}
	return false
}

func (m *Manager) configPath(scope string) (string, error) {
	switch scope {
	case "user":
		return m.options.UserConfigPath, nil
	case "project":
		if !m.options.ProjectTrusted {
			return "", errors.New("project MCP configuration requires explicit project trust")
		}
		if m.options.Workspace == "" {
			return "", errors.New("no project workspace")
		}
		return filepath.Join(m.options.Workspace, ".easyagent", "mcp.json"), nil
	default:
		return "", errors.New("scope must be user or project")
	}
}
func (m *Manager) PutServer(ctx context.Context, name string, cfg MCPServerConfig, scope string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mutation.Lock()
	defer m.mutation.Unlock()
	m.mu.RLock()
	path, err := m.configPath(scope)
	existing := m.servers[name]
	_, definedInScope := scopedConfig(path, name)
	if err == nil && existing != nil && existing.entry.Scope != scope && !definedInScope {
		err = errors.New("server is defined in a different scope; use a distinct name or edit its defining scope")
	}
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	if err = editConfig(path, name, &cfg); err != nil {
		return err
	}
	m.load()
	return nil
}
func (m *Manager) RemoveServer(ctx context.Context, name, scope string) error {
	m.mutation.Lock()
	defer m.mutation.Unlock()
	m.mu.RLock()
	path, err := m.configPath(scope)
	existing := m.servers[name]
	_, definedInScope := scopedConfig(path, name)
	if err == nil && existing != nil && existing.entry.Scope != scope && !definedInScope {
		err = errors.New("server is defined in another scope")
	}
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	if err = editConfig(path, name, nil); err != nil {
		return err
	}
	m.load()
	return nil
}
func (m *Manager) patchServer(ctx context.Context, name string, patch func(*MCPServerConfig)) error {
	srv, err := m.server(name)
	if err != nil {
		return err
	}
	m.mu.RLock()
	cfg := srv.entry.Config
	scope := srv.entry.Scope
	m.mu.RUnlock()
	patch(&cfg)
	return m.PutServer(ctx, name, cfg, scope)
}
func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool) error {
	return m.patchServer(ctx, name, func(c *MCPServerConfig) { c.Enabled = &enabled })
}
func (m *Manager) SetTrust(ctx context.Context, name string, trust bool) error {
	return m.patchServer(ctx, name, func(c *MCPServerConfig) { c.Trust = trust })
}
func (m *Manager) SetProjectTrusted(ctx context.Context, trusted bool) error {
	m.mutation.Lock()
	defer m.mutation.Unlock()
	m.mu.Lock()
	m.options.ProjectTrusted = trusted
	m.mu.Unlock()
	m.load()
	return nil
}

func (m *Manager) callTool(ctx context.Context, name, tool string, args map[string]any, onUpdate func(agent.PartialResult)) (*protocol.CallToolResult, error) {
	srv, err := m.server(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	state := srv.state
	m.mu.RUnlock()
	if state == StatusNeedsAuth {
		return nil, errNeedsAuth
	}
	if err = m.ConnectOne(ctx, name); err != nil {
		return nil, err
	}
	m.mu.RLock()
	session, conn := srv.session, srv.client
	allowed := false
	for _, current := range srv.tools {
		if current.Name == tool && isToolEnabled(tool, srv.entry.Config) {
			allowed = true
			break
		}
	}
	m.mu.RUnlock()
	if session == nil || !allowed {
		return nil, errors.New("MCP tool is no longer available")
	}
	params := &protocol.CallToolParams{Name: tool, Arguments: args}
	cleanup := conn.progress(params, onUpdate)
	defer cleanup()
	// Never replay a tool request after an uncertain transport failure.
	result, err := session.CallTool(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("MCP tool call failed: %s", conn.safeError(err))
	}
	return result, nil
}
