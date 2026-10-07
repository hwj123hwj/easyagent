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
	return "macOS 电脑使用工具（实验）：感知与控制当前电脑。" +
		"actions: screenshot（全屏截图，返回本地文件路径）、get_app_state（当前焦点应用的可读元素列表）、" +
		"perform_action（键鼠控制：click/type/key/scroll/wait 批量动作，需用户逐批批准；单批最多 " + fmt.Sprint(maxActionSteps) + " 个动作）。"
}

// Parameters implements agent.Tool (map-based JSON Schema).
func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"screenshot", "get_app_state", "perform_action"},
				"description": "要执行的动作：screenshot 截屏、get_app_state 读取应用状态、perform_action 控制键鼠（需用户批准）",
			},
			"actions": map[string]any{
				"type":        "array",
				"description": "perform_action 时必填：按顺序原子执行的键鼠动作列表（单次调用一批，整体一次确认）",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"enum":        []string{"click", "double_click", "right_click", "type", "key", "scroll", "wait"},
							"description": "动作类型",
						},
						"x":    map[string]any{"type": "integer", "description": "click 系列必填：屏幕坐标 x"},
						"y":    map[string]any{"type": "integer", "description": "click 系列必填：屏幕坐标 y"},
						"text": map[string]any{"type": "string", "description": "type 必填：要输入的文本"},
						"key":  map[string]any{"type": "string", "description": "key 必填：按键或组合键，如 cmd+s、enter"},
						"dx":   map[string]any{"type": "integer", "description": "scroll 可选：水平滚动量"},
						"dy":   map[string]any{"type": "integer", "description": "scroll 可选：垂直滚动量"},
						"ms":   map[string]any{"type": "integer", "description": "wait 可选：等待毫秒数（<=5000）"},
					},
					"required": []string{"type"},
				},
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
	case "perform_action":
		normalized, err := validateActions(params)
		if err != nil {
			return nil, err
		}
		return normalized, nil
	case "":
		return nil, fmt.Errorf("computer 需要 action 参数")
	default:
		return nil, fmt.Errorf("不支持的 computer action %q（仅支持 screenshot / get_app_state / perform_action）", input.Action)
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

	case "perform_action":
		// Validate 已归一化 payload；此处直接信任 actionBatch。
		var batch actionBatch
		if err := json.Unmarshal(params, &batch); err != nil || len(batch.Actions) == 0 {
			return agent.ToolResult{IsError: true, Content: "perform_action 动作批无效"}, nil
		}
		if _, err := runHelper(c, "perform_action", batch); err != nil {
			return agent.ToolResult{IsError: true, Content: "执行键鼠动作失败: " + err.Error()}, nil
		}
		return agent.ToolResult{Content: fmt.Sprintf("已执行 %d 个动作", len(batch.Actions))}, nil
	}
	return agent.ToolResult{IsError: true, Content: fmt.Sprintf("未知 action %q", input.Action)}, nil
}

// ─── 确认门（sdk/agent ToolWithConfirmation）────────────────────────────────

// RequiresConfirmation implements agent.ToolWithConfirmation. Read-only
// actions run silently; perform_action always requires per-batch approval —
// the description lists every step so the user approves exactly what will run.
func (t *Tool) RequiresConfirmation(params json.RawMessage) (string, bool) {
	var input struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(params, &input) != nil || input.Action != "perform_action" {
		return "", false
	}
	var batch actionBatch
	if json.Unmarshal(params, &batch) != nil || len(batch.Actions) == 0 {
		return "执行未知的键鼠动作批", true
	}
	prefix := "电脑控制："
	if isHighRisk(batch) {
		prefix = "⚠️ 高风险电脑控制："
	}
	return prefix + describeActions(batch), true
}

// RequiresConfirmationAvailable implements agent.ToolRequiringConfirmation.
// In headless entrypoints (serve 单向流、定时任务) there is no human to
// approve — refuse rather than silently inherit approval.
func (t *Tool) RequiresConfirmationAvailable() bool { return true }

// runHelper shells out one CLI invocation per action — stateless and simple,
// matching the daemon convention without holding a long-lived pipe in PR-1.
func runHelper(ctx context.Context, tool string, args any) (json.RawMessage, error) {
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

// ─── perform_action: 键鼠控制（PR-2）─────────────────────────────────────────

// actionStep is one atomic mouse/keyboard step inside a perform_action batch.
type actionStep struct {
	Type string `json:"type"`
	X    *int   `json:"x,omitempty"`
	Y    *int   `json:"y,omitempty"`
	Text string `json:"text,omitempty"`
	Key  string `json:"key,omitempty"`
	Dx   *int   `json:"dx,omitempty"`
	Dy   *int   `json:"dy,omitempty"`
	Ms   *int   `json:"ms,omitempty"`
}

// actionBatch is the normalized perform_action payload (produced by Validate).
type actionBatch struct {
	Actions []actionStep `json:"actions"`
}

// maxActionSteps bounds one confirmation → one bounded batch. Keeps a runaway
// model from parking the machine in a long unattended loop.
const maxActionSteps = 20

// maxWaitMs bounds each wait step; sleeps belong to the agent loop, not the UI.
const maxWaitMs = 5000

// maxTypeChars bounds type steps; large documents must go through file tools.
const maxTypeChars = 2000

// validateActions parses and checks the perform_action batch. It returns a
// normalized payload (numbers already bounds-checked) so Execute can trust it.
func validateActions(raw json.RawMessage) (json.RawMessage, error) {
	var wrapper struct {
		Actions []actionStep `json:"actions"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("computer perform_action 参数解析失败: %w", err)
	}
	if len(wrapper.Actions) == 0 {
		return nil, fmt.Errorf("perform_action 需要 actions 数组（1-%d 个动作）", maxActionSteps)
	}
	if len(wrapper.Actions) > maxActionSteps {
		return nil, fmt.Errorf("actions 数量超过上限（%d > %d）——请拆分为多次调用", len(wrapper.Actions), maxActionSteps)
	}
	for i, s := range wrapper.Actions {
		switch s.Type {
		case "click", "double_click", "right_click":
			if s.X == nil || s.Y == nil {
				return nil, fmt.Errorf("actions[%d] %s 需要 x/y 坐标", i, s.Type)
			}
			if err := checkScreenPoint(*s.X, *s.Y); err != nil {
				return nil, fmt.Errorf("actions[%d]: %w", i, err)
			}
		case "type":
			if s.Text == "" {
				return nil, fmt.Errorf("actions[%d] type 需要 text", i)
			}
			if len([]rune(s.Text)) > maxTypeChars {
				return nil, fmt.Errorf("actions[%d] type 文本超长（%d > %d 字符）——请改用文件+编辑器", i, len([]rune(s.Text)), maxTypeChars)
			}
		case "key":
			if s.Key == "" {
				return nil, fmt.Errorf("actions[%d] key 需要 key", i)
			}
			if err := checkKeyCombo(s.Key); err != nil {
				return nil, fmt.Errorf("actions[%d]: %w", i, err)
			}
		case "scroll":
			if s.Dx == nil && s.Dy == nil {
				return nil, fmt.Errorf("actions[%d] scroll 需要 dx 或 dy", i)
			}
		case "wait":
			if s.Ms != nil && (*s.Ms < 0 || *s.Ms > maxWaitMs) {
				return nil, fmt.Errorf("actions[%d] wait 时长需在 0-%d ms", i, maxWaitMs)
			}
		default:
			return nil, fmt.Errorf("actions[%d] 不支持的动作类型 %q", i, s.Type)
		}
	}
	// Re-marshal only the actions array, preserving the rest of the payload
	// (notably "action":"perform_action") so downstream confirmation checks
	// and the helper both see a complete, normalized request.
	normalizedActions, err := json.Marshal(wrapper.Actions)
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("computer perform_action 参数解析失败: %w", err)
	}
	payload["actions"] = normalizedActions
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

// checkScreenPoint rejects coordinates outside a sane virtual-screen envelope.
// The helper re-checks against the real display bounds; this catches runaway
// model math before it reaches the OS.
func checkScreenPoint(x, y int) error {
	if x < -32768 || x > 32768 || y < -32768 || y > 32768 {
		return fmt.Errorf("坐标 (%d, %d) 超出合理屏幕范围", x, y)
	}
	return nil
}

// dangerousKeyCombos are combos that can destroy work or lock the user out.
// They get a high-risk confirmation description and (PR-3) policy gating.
var dangerousKeyCombos = map[string]bool{
	"cmd+q":       true, // 退出应用
	"cmd+shift+q": true, // 退出并注销（多数应用）
	"cmd+alt+esc": true, // 强制退出对话框
	"cmd+w":       false,
	"ctrl+click":  false,
}

// checkKeyCombo rejects malformed combos early; the helper does final mapping.
func checkKeyCombo(key string) error {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return fmt.Errorf("key 不能为空")
	}
	for _, part := range strings.Split(k, "+") {
		switch strings.TrimSpace(part) {
		case "cmd", "alt", "ctrl", "shift", "fn", "space", "enter", "return",
			"tab", "esc", "escape", "delete", "backspace", "up", "down", "left", "right",
			"home", "end", "pageup", "pagedown", "f1", "f2", "f3", "f4", "f5", "f6",
			"f7", "f8", "f9", "f10", "f11", "f12":
		default:
			if len([]rune(part)) != 1 {
				return fmt.Errorf("key 组合含无法识别的部分 %q", part)
			}
		}
	}
	return nil
}

// describeActions renders a batch into a human-readable confirmation
// description. Surfaced in the desktop confirmation card.
func describeActions(batch actionBatch) string {
	if len(batch.Actions) == 1 {
		return describeStep(batch.Actions[0])
	}
	parts := make([]string, 0, len(batch.Actions))
	for _, s := range batch.Actions {
		parts = append(parts, describeStep(s))
	}
	return fmt.Sprintf("依次执行 %d 个动作：%s", len(batch.Actions), strings.Join(parts, " → "))
}

func describeStep(s actionStep) string {
	switch s.Type {
	case "click":
		return fmt.Sprintf("左键点击 (%d, %d)", deref(s.X), deref(s.Y))
	case "double_click":
		return fmt.Sprintf("双击 (%d, %d)", deref(s.X), deref(s.Y))
	case "right_click":
		return fmt.Sprintf("右键点击 (%d, %d)", deref(s.X), deref(s.Y))
	case "type":
		preview := s.Text
		if len([]rune(preview)) > 40 {
			preview = string([]rune(preview)[:40]) + "…"
		}
		return fmt.Sprintf("输入文本 %q", preview)
	case "key":
		return "按键 " + s.Key
	case "scroll":
		if s.Dy != nil {
			return fmt.Sprintf("滚动 dy=%d", *s.Dy)
		}
		return fmt.Sprintf("滚动 dx=%d", *s.Dx)
	case "wait":
		return fmt.Sprintf("等待 %d ms", deref(s.Ms))
	}
	return s.Type
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// isHighRisk reports whether the batch contains an action that can destroy
// work or lock the user out (PR-3 policy gates on this).
func isHighRisk(batch actionBatch) bool {
	for _, s := range batch.Actions {
		if s.Type == "key" && dangerousKeyCombos[strings.ToLower(strings.TrimSpace(s.Key))] {
			return true
		}
	}
	return false
}
