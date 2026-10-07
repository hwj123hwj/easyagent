// Package computer implements the experimental computer-use integration:
// a macOS helper bridge (Swift, backed by trycua/cua's CuaDriverCore) plus a
// lease registry that guarantees one controlling session per target.
//
// PR-1 scope is read-only: screenshot and accessibility-tree snapshots only.
// Mouse/keyboard injection lands in a follow-up behind the same lease gate.
package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
)

// ─── Helper bridge ───────────────────────────────────────────────────────────

// HelperPath resolves the helper binary shipped next to the server executable
// (easyagent-cua-helper), falling back to PATH.
func HelperPath() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "easyagent-cua-helper")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "easyagent-cua-helper"
}

// Available reports whether computer use can run at all: macOS platform plus
// a discoverable helper binary.
func Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	path := HelperPath()
	if path == "easyagent-cua-helper" {
		_, err := exec.LookPath(path)
		return err == nil
	}
	return true
}

// CheckPermissions probes TCC status (accessibility + screen recording) via
// the helper's status command. Missing permissions surface as a friendly
// message for the settings page.
func CheckPermissions(ctx context.Context) (granted bool, missing []string, err error) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, HelperPath(), "status").Output()
	if err != nil {
		return false, []string{"helper 不可用"}, err
	}
	var status struct {
		Accessibility bool `json:"accessibility"`
		ScreenRecord  bool `json:"screen_recording"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false, []string{"helper 输出无法解析"}, err
	}
	if !status.Accessibility {
		missing = append(missing, "辅助功能 (Accessibility)")
	}
	if !status.ScreenRecord {
		missing = append(missing, "屏幕录制 (Screen Recording)")
	}
	return len(missing) == 0, missing, nil
}

// RequestPermissions opens the macOS System Settings panes so the user can
// grant TCC permissions. Cannot be silent by OS design.
func RequestPermissions(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(c, HelperPath(), "request-permissions").Run()
}

// ─── Lease registry ──────────────────────────────────────────────────────────

// ErrLeaseHeld is returned when another session controls the computer.
var ErrLeaseHeld = errors.New("computer use 已被其他会话占用")

type leaseRegistry struct {
	mu    sync.Mutex
	owner string // session id, "" when idle
}

// Acquire grants the lease to sessionID or reports the current holder.
func (r *leaseRegistry) Acquire(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner != "" && r.owner != sessionID {
		return fmt.Errorf("%w（当前持有者会话 %s）", ErrLeaseHeld, r.owner)
	}
	r.owner = sessionID
	return nil
}

// Release drops the lease only if sessionID owns it.
func (r *leaseRegistry) Release(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner == sessionID {
		r.owner = ""
	}
}

// Owner reports the current lease holder ("" when idle) for the settings page.
func (r *leaseRegistry) Owner() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.owner
}

// Registry is the process-wide lease singleton.
var Registry = &leaseRegistry{}

// ─── Model-facing tool ───────────────────────────────────────────────────────

// Tool is the single model-facing computer tool. PR-1 exposes read-only
// actions; the schema gates future control actions behind the same lease.
type Tool struct {
	shotsDir string // where screenshots are persisted
}

// NewTool wires the computer tool to a screenshot dir under the data dir.
func NewTool(dataDir string) *Tool {
	return &Tool{
		shotsDir: filepath.Join(dataDir, "computer", "screenshots"),
	}
}

// Name implements agent.Tool.
func (t *Tool) Name() string { return "computer" }

// Description tells the model when and how to use the tool.
func (t *Tool) Description() string {
	return "macOS 电脑感知工具（实验）：截取屏幕截图或读取当前应用的无障碍树快照。" +
		"actions: screenshot（全屏截图，返回本地文件路径）、get_app_state（当前焦点应用的可读元素列表）。" +
		"只读操作，不能控制键鼠。"
}

// Parameters implements agent.Tool (map-based JSON Schema).
func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"screenshot", "get_app_state"},
				"description": "要执行的感知动作",
			},
		},
		"required": []string{"action"},
	}
}

// Validate normalizes and checks arguments before Execute.
func (t *Tool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("computer 参数解析失败: %w", err)
	}
	switch input.Action {
	case "screenshot", "get_app_state":
		return params, nil
	case "":
		return nil, fmt.Errorf("computer 需要 action 参数")
	default:
		return nil, fmt.Errorf("不支持的 computer action %q（当前仅支持 screenshot / get_app_state）", input.Action)
	}
}

// Execute performs the read-only action. Both actions acquire the process-wide
// lease so the settings page occupancy reflects real usage — the gate is
// exercised from day one, before any control action exists.
func (t *Tool) Execute(ctx context.Context, params json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var input struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	c, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	switch input.Action {
	case "screenshot":
		if err := os.MkdirAll(t.shotsDir, 0o755); err != nil {
			return agent.ToolResult{IsError: true, Content: "创建截图目录失败: " + err.Error()}, nil
		}
		out, err := runHelper(c, "screenshot", map[string]any{"output_dir": t.shotsDir})
		if err != nil {
			return agent.ToolResult{IsError: true, Content: "截屏失败: " + err.Error()}, nil
		}
		var result struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(out, &result); err != nil || result.Path == "" {
			return agent.ToolResult{IsError: true, Content: "截屏结果异常: " + strings.TrimSpace(string(out))}, nil
		}
		return agent.ToolResult{Content: "屏幕截图已保存: " + result.Path}, nil

	case "get_app_state":
		out, err := runHelper(c, "app_state", nil)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: "读取应用状态失败: " + err.Error()}, nil
		}
		return agent.ToolResult{Content: string(out)}, nil
	}
	return agent.ToolResult{IsError: true, Content: fmt.Sprintf("未知 action %q", input.Action)}, nil
}

// runHelper shells out one CLI invocation per action — stateless and simple,
// matching the daemon convention without holding a long-lived pipe in PR-1.
func runHelper(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	cmdArgs := []string{"tool", tool}
	if args != nil {
		payload, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		cmdArgs = append(cmdArgs, "--args", string(payload))
	}
	c, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, HelperPath(), cmdArgs...).Output()
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}
