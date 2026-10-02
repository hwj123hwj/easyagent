package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/mcp"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

type mcpState struct {
	gate      sync.RWMutex
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	managers  map[string]*mcp.Manager
	trusted   map[string]bool
	trustPath string
	userPath  string
	revision  atomic.Uint64
}

func (a *App) initMCP() {
	a.mcpState.ctx, a.mcpState.cancel = context.WithCancel(context.Background())
	a.mcpState.managers = map[string]*mcp.Manager{}
	a.mcpState.trusted = map[string]bool{}
	path := a.cfg.MCPConfigPath
	if path == "" {
		path = filepath.Join(config.HomeDir(), "mcp.json")
	}
	a.mcpState.trustPath = path + ".trust.json"
	a.mcpState.userPath = path
	if b, err := os.ReadFile(a.mcpState.trustPath); err == nil {
		_ = json.Unmarshal(b, &a.mcpState.trusted)
	}
	if a.mcpState.trusted == nil {
		a.mcpState.trusted = map[string]bool{}
	}
}

func (a *App) MCPWorkspace(workspace string) string {
	if workspace == "" {
		workspace = a.cfg.Workspace
	}
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	if real, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = real
	}
	return filepath.Clean(workspace)
}

// MCP returns an independently scoped manager. Opening a project never executes
// its configuration until the user has explicitly trusted that workspace.
func (a *App) MCP(workspace string) *mcp.Manager {
	workspace = a.MCPWorkspace(workspace)
	a.mcpState.mu.Lock()
	defer a.mcpState.mu.Unlock()
	if manager := a.mcpState.managers[workspace]; manager != nil {
		return manager
	}
	manager := mcp.NewManager(mcp.Options{Workspace: workspace, UserConfigPath: a.mcpState.userPath, ProjectTrusted: a.mcpState.trusted[workspace]})
	manager.Subscribe(func() { a.mcpState.revision.Add(1) })
	manager.Load()
	a.mcpState.managers[workspace] = manager
	manager.Start(a.mcpState.ctx)
	return manager
}

func (a *App) mcpRevision(workspace string) uint64 {
	a.MCP(workspace)
	return a.mcpState.revision.Load()
}

func (a *App) prepareMCP(ctx context.Context, workspace string) error {
	ready, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = a.MCP(workspace).WaitReady(ready)
	return ctx.Err()
}

func (a *App) MCPProjectTrusted(workspace string) bool {
	a.mcpState.mu.Lock()
	defer a.mcpState.mu.Unlock()
	return a.mcpState.trusted[a.MCPWorkspace(workspace)]
}

// CheckMCPIdle protects active tool calls from a configuration reload.
// User-scope edits affect every workspace, while project edits affect one.
func (a *App) CheckMCPIdle(workspace, scope string) error {
	workspace = a.MCPWorkspace(workspace)
	for _, id := range a.sessionStore.List() {
		if sess, ok := a.sessionStore.Get(id); ok && (scope == "user" || a.MCPWorkspace(sess.Workspace()) == workspace) && sess.IsBusy() {
			return errors.New("请等当前任务完成，或先停止任务，再修改 MCP 配置")
		}
	}
	return nil
}

func (a *App) SetMCPProjectTrust(ctx context.Context, workspace string, trusted bool) error {
	release, err := a.BeginMCPEdit()
	if err != nil {
		return err
	}
	defer release()
	workspace = a.MCPWorkspace(workspace)
	if err := a.CheckMCPIdle(workspace, "project"); err != nil {
		return err
	}
	a.mcpState.mu.Lock()
	copyTrust := make(map[string]bool, len(a.mcpState.trusted))
	for key, value := range a.mcpState.trusted {
		copyTrust[key] = value
	}
	if trusted {
		copyTrust[workspace] = true
	} else {
		delete(copyTrust, workspace)
	}
	b, _ := json.MarshalIndent(copyTrust, "", "  ")
	path := a.mcpState.trustPath
	err = savePrivate(path, b)
	if err == nil {
		a.mcpState.trusted = copyTrust
	}
	a.mcpState.mu.Unlock()
	if err != nil {
		return errors.New("无法保存项目 MCP 授权，请检查配置目录权限")
	}
	return a.MCP(workspace).SetProjectTrusted(ctx, trusted)
}

func savePrivate(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mcp-trust-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// ReloadMCP updates the other workspace views after a user-level config edit.
func (a *App) ReloadMCP(except *mcp.Manager) {
	a.mcpState.mu.Lock()
	managers := []*mcp.Manager{}
	for _, manager := range a.mcpState.managers {
		if manager != except {
			managers = append(managers, manager)
		}
	}
	a.mcpState.mu.Unlock()
	for _, manager := range managers {
		manager.Load()
	}
}

func (a *App) closeMCP() {
	a.mcpState.cancel()
	a.mcpState.mu.Lock()
	defer a.mcpState.mu.Unlock()
	for _, manager := range a.mcpState.managers {
		_ = manager.Close()
	}
}

// ManageMCP is shared by the TUI and the HTTP slash-command entry.
func (a *App) ManageMCP(ctx context.Context, workspace, args string) (string, error) {
	manager := a.MCP(workspace)
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "list" || fields[0] == "status" {
		var out strings.Builder
		fmt.Fprintf(&out, "MCP · %s\n项目配置授权：%t\n", a.MCPWorkspace(workspace), a.MCPProjectTrusted(workspace))
		for _, server := range manager.Servers() {
			fmt.Fprintf(&out, "%s · %s · %s · %d tools · trusted=%t\n", server.Name, server.Scope, server.State, server.ToolCount, server.Trusted)
			if server.Error != "" {
				fmt.Fprintf(&out, "  %s\n", server.Error)
			}
		}
		for _, issue := range manager.Issues() {
			fmt.Fprintf(&out, "配置：%s\n", issue.Message)
		}
		out.WriteString("\n/mcp reconnect|refresh|enable|disable|trust|untrust <server>\n/mcp project trust|untrust\n在网页或桌面设置中添加服务、选择工具和完成 OAuth 授权。")
		return out.String(), nil
	}
	if len(fields) != 2 {
		return "用法：/mcp [list|reconnect|refresh|enable|disable|trust|untrust <server>|project trust|project untrust]", nil
	}
	if fields[0] == "project" {
		if fields[1] != "trust" && fields[1] != "untrust" {
			return "用法：/mcp project trust|untrust", nil
		}
		if err := a.SetMCPProjectTrust(ctx, workspace, fields[1] == "trust"); err != nil {
			return "", err
		}
		return "项目 MCP 配置授权已更新。", nil
	}
	release, err := a.BeginMCPEdit()
	if err != nil {
		return "", err
	}
	defer release()
	if err := a.CheckMCPIdle(workspace, "user"); err != nil {
		return "", err
	}
	switch fields[0] {
	case "reconnect":
		err = manager.Reconnect(ctx, fields[1])
	case "refresh":
		err = manager.RefreshTools(ctx, fields[1])
	case "enable", "disable":
		err = manager.SetEnabled(ctx, fields[1], fields[0] == "enable")
	case "trust", "untrust":
		err = manager.SetTrust(ctx, fields[1], fields[0] == "trust")
	default:
		return "未知 MCP 操作；输入 /mcp 查看帮助。", nil
	}
	if err != nil {
		return "", err
	}
	a.ReloadMCP(manager)
	return "MCP 服务已更新；下一次发送会使用最新工具列表。", nil
}

// BeginMCPEdit excludes new prompts and rejects edits while any integration is
// in use. This also covers workflow actors and SDK-backed entries.
func (a *App) BeginMCPEdit() (func(), error) {
	if !a.mcpState.gate.TryLock() {
		return nil, errors.New("MCP 正在被任务使用，请先等任务结束或停止任务")
	}
	return a.mcpState.gate.Unlock, nil
}

func (a *App) acquireMCP(ctx context.Context, workspace string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !a.mcpState.gate.TryRLock() {
		return nil, runtime.ErrToolsUpdating
	}
	return a.mcpState.gate.RUnlock, nil
}
