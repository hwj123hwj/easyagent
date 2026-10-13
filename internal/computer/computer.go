// Package computer implements the experimental computer-use integration:
// a bundled macOS helper CLI bridge and exclusive per-invocation ownership.
package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/config"
)

// ─── Helper bridge ───────────────────────────────────────────────────────────

// HelperPath prefers the signed helper in the desktop bundle. Standalone cores
// retain the legacy sibling/PATH integration without requiring MCP configuration.
func HelperPath() string {
	if exe, err := os.Executable(); err == nil {
		return helperPathForExecutable(exe)
	}
	return "easyagent-cua-helper"
}
func helperPathForExecutable(exe string) string {
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(exe), "cua-helper", "EasyAgent Computer Use.app", "Contents", "MacOS", "easyagent-cua-helper"),
		filepath.Join(filepath.Dir(exe), "easyagent-cua-helper"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	return "easyagent-cua-helper"
}

// Available reports whether computer use can run at all: macOS platform plus
// a discoverable helper binary.
func Available() bool {
	if !platformSupported() {
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
		return false, []string{"电脑控制组件启动失败（需要 macOS 14+）"}, err
	}
	var status struct {
		Accessibility   bool `json:"accessibility"`
		ScreenRecord    bool `json:"screen_recording"`
		ProtocolVersion int  `json:"protocol_version"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false, []string{"helper 输出无法解析"}, err
	}
	if status.ProtocolVersion != 2 {
		return false, []string{"电脑控制组件版本不兼容，请更新内置 helper"}, fmt.Errorf("需要 helper 协议 2，收到 %d", status.ProtocolVersion)
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
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
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
	if sessionID == "" {
		return errors.New("computer use 缺少会话身份")
	}
	if r.owner != "" {
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

// Tool is the session-scoped model-facing computer tool.
type Tool struct {
	sessionID string
	helper    func(context.Context, string, any) (json.RawMessage, error)
	dataDir   string // settings.json lives here (approved-apps persistence)
	shotsDir  string // where screenshots are persisted

	mu             sync.Mutex
	approvalPolicy string   // "ask" (default) | "auto"
	approvedApps   []string // apps actually controlled in auto mode (session view)
}

// NewTool wires the computer tool to the data dir; policy defaults to "ask".
func NewTool(dataDir string) *Tool {
	return &Tool{
		dataDir:  dataDir,
		helper:   runHelper,
		shotsDir: filepath.Join(dataDir, "computer", "screenshots"),
	}
}

// ConfigurePolicy sets the approval policy and seeds the approved-apps view
// from persisted settings. Called at tool construction (session-scoped).
func (t *Tool) ConfigurePolicy(policy string, approvedApps []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if policy != "auto" {
		policy = "ask"
	}
	t.approvalPolicy = policy
	t.approvedApps = append([]string(nil), approvedApps...)
}

// ConfigureSession binds occupancy to the owning runtime session.
func (t *Tool) ConfigureSession(sessionID string) { t.sessionID = sessionID }

// Name implements agent.Tool.
func (t *Tool) Name() string { return "computer" }

// Description tells the model when and how to use the tool.
func (t *Tool) Description() string {
	return "macOS 后台应用控制（实验）。先 list_apps 获取 bundle_id、pid、window_id；后续动作必须携带这三个目标标识。" +
		"screenshot 只截指定窗口（即使被其他应用遮挡）；get_app_state 只读指定窗口；perform_action 向目标进程发送 click/type/key/scroll/wait，最多 " + fmt.Sprint(maxActionSteps) + " 个动作，遵守审批策略。" +
		"坐标是目标窗口左上角为原点的逻辑像素。先观察再操作；输入前先点击目标窗口的可编辑区域。窗口关闭、身份变化或不支持后台操作时停止并报告，不能激活应用或改用全局键鼠。用户可继续使用其他应用，但不要同时编辑同一目标窗口。"
}

// Parameters implements agent.Tool (map-based JSON Schema).
func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list_apps", "screenshot", "get_app_state", "perform_action"},
				"description": "list_apps 列出应用和窗口；其他动作限定目标窗口（操作按策略审批）",
			},
			"bundle_id": map[string]any{"type": "string", "description": "除 list_apps 外必填：应用 bundle ID（来自 list_apps）"},
			"pid":       map[string]any{"type": "integer", "description": "除 list_apps 外必填：应用进程 ID"},
			"window_id": map[string]any{"type": "integer", "description": "除 list_apps 外必填：目标窗口 ID"},
			"actions": map[string]any{
				"type":        "array",
				"description": "perform_action 时必填：按顺序执行（不支持回滚）的键鼠动作列表（单次调用一批，整体一次确认）",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"enum":        []string{"click", "double_click", "right_click", "type", "key", "scroll", "wait"},
							"description": "动作类型",
						},
						"x":    map[string]any{"type": "integer", "description": "click 系列必填：目标窗口内坐标 x"},
						"y":    map[string]any{"type": "integer", "description": "click 系列必填：目标窗口内坐标 y"},
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
		Action   string `json:"action"`
		BundleID string `json:"bundle_id"`
		PID      int64  `json:"pid"`
		WindowID int64  `json:"window_id"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("computer 参数解析失败: %w", err)
	}
	if (input.Action == "screenshot" || input.Action == "get_app_state" || input.Action == "perform_action") && (strings.TrimSpace(input.BundleID) == "" || input.PID <= 0 || input.PID > 2147483647 || input.WindowID <= 0 || input.WindowID > 4294967295) {
		return nil, fmt.Errorf("先 list_apps，其他动作必须指定有效的 bundle_id、pid、window_id；不会使用前台应用")
	}
	switch input.Action {
	case "list_apps", "screenshot", "get_app_state":
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
		return nil, fmt.Errorf("不支持的 computer action %q（仅支持 list_apps / screenshot / get_app_state / perform_action）", input.Action)
	}
}

// Execute validates arguments and holds exclusive ownership until the helper returns.
func (t *Tool) Execute(ctx context.Context, params json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	normalized, err := t.Validate(params)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}
	params = normalized
	var input struct {
		Action   string `json:"action"`
		BundleID string `json:"bundle_id"`
		PID      int64  `json:"pid"`
		WindowID int64  `json:"window_id"`
	}
	_ = json.Unmarshal(params, &input)
	if err := Registry.Acquire(t.sessionID); err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}
	defer Registry.Release(t.sessionID)

	c, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	target := map[string]any{"bundle_id": input.BundleID, "pid": input.PID, "window_id": input.WindowID}
	switch input.Action {
	case "list_apps":
		out, err := t.helper(c, "list_apps", nil)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: "列出应用失败: " + err.Error()}, nil
		}
		return agent.ToolResult{Content: string(out)}, nil
	case "screenshot":
		if err := os.MkdirAll(t.shotsDir, 0o700); err != nil {
			return agent.ToolResult{IsError: true, Content: "创建截图目录失败: " + err.Error()}, nil
		}
		target["output_dir"] = t.shotsDir
		out, err := t.helper(c, "screenshot", target)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: "截屏失败: " + err.Error()}, nil
		}
		var result struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(out, &result); err != nil || result.Path == "" {
			return agent.ToolResult{IsError: true, Content: "截屏结果异常: " + strings.TrimSpace(string(out))}, nil
		}
		data, err := t.readScreenshot(result.Path)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: "读取截图失败: " + err.Error()}, nil
		}
		return agent.ToolResult{Content: "目标窗口截图（像素对应窗口内逻辑坐标）: " + string(out), Images: []ai.ImageBlock{{MediaType: "image/png", Data: data}}}, nil

	case "get_app_state":
		out, err := t.helper(c, "app_state", target)
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
		out, err := t.helper(c, "perform_action", batch)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: "执行键鼠动作失败: " + err.Error()}, nil
		}
		var result struct {
			Error   string `json:"error"`
			Success *bool  `json:"success"`
		}
		if err := json.Unmarshal(out, &result); err != nil {
			return agent.ToolResult{IsError: true, Content: "键鼠结果无法解析"}, nil
		}
		if result.Error != "" || result.Success == nil || !*result.Success {
			return agent.ToolResult{IsError: true, Content: "键鼠操作未完成: " + string(out)}, nil
		}
		t.recordControlledApp(out)
		return agent.ToolResult{Content: fmt.Sprintf("已执行 %d 个目标窗口动作: %s", len(batch.Actions), out)}, nil
	}
	return agent.ToolResult{IsError: true, Content: fmt.Sprintf("未知 action %q", input.Action)}, nil
}

// ─── 确认门（sdk/agent ToolWithConfirmation）────────────────────────────────

// RequiresConfirmation implements agent.ToolWithConfirmation. Read-only
// actions run silently.
//
// perform_action policy (PR-3):
//   - "ask" (default): every batch needs per-batch approval, description lists
//     every step so the user approves exactly what will run.
//   - "auto": non-high-risk batches run without asking (the user opted in via
//     settings); high-risk batches (quit combos etc.) ALWAYS ask, policy
//     notwithstanding.
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
	highRisk := isHighRisk(batch)
	if !highRisk {
		t.mu.Lock()
		policy := t.approvalPolicy
		t.mu.Unlock()
		if policy == "auto" {
			return "", false
		}
	}
	prefix := "电脑控制："
	if highRisk {
		prefix = "⚠️ 高风险电脑控制："
	}
	return prefix + fmt.Sprintf("%s（进程 %d，窗口 %d）\n", batch.BundleID, batch.PID, batch.WindowID) + describeActions(batch), true
}

// RequiresConfirmationAvailable implements agent.ToolRequiringConfirmation.
// In headless entrypoints (serve 单向流、定时任务) there is no human to
// approve — refuse rather than silently inherit approval.
func (t *Tool) RequiresConfirmationAvailable() bool   { return true }
func (t *Tool) RequiresInteractiveConfirmation() bool { return true }

// recordControlledApp persists the app that received the injected actions
// (helper returns controlled_app only for an input batch, excluding waits). Best-effort: display data,
// never blocks control. Updates the in-session view and settings.json.
func (t *Tool) recordControlledApp(helperOut json.RawMessage) {
	var result struct {
		ControlledApp string `json:"controlled_app"`
	}
	if json.Unmarshal(helperOut, &result) != nil || result.ControlledApp == "" {
		return
	}
	t.mu.Lock()
	seen := false
	for _, existing := range t.approvedApps {
		if existing == result.ControlledApp {
			seen = true
			break
		}
	}
	if !seen {
		t.approvedApps = append(t.approvedApps, result.ControlledApp)
	}
	t.mu.Unlock()
	_ = config.RecordApprovedApp(t.dataDir, result.ControlledApp)
}

// ApprovedApps returns the session's controlled-app list (settings page).
func (t *Tool) ApprovedApps() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.approvedApps))
	copy(out, t.approvedApps)
	return out
}

// runHelper shells out one CLI invocation per action — stateless and simple,
// matching the daemon convention without holding a long-lived pipe in PR-1.
func runHelper(ctx context.Context, tool string, args any) (json.RawMessage, error) {
	return runHelperAtPath(ctx, HelperPath(), tool, args)
}

func runHelperAtPath(ctx context.Context, path, tool string, args any) (json.RawMessage, error) {
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
	// Old helpers ignore target fields and inject globally. Never invoke their tool command.
	versionCtx, versionCancel := context.WithTimeout(c, 5*time.Second)
	version, versionErr := exec.CommandContext(versionCtx, path, "--version").Output()
	versionCancel()
	var protocol struct {
		Version int `json:"protocol_version"`
	}
	if versionErr != nil || json.Unmarshal(version, &protocol) != nil || protocol.Version != 2 {
		return nil, fmt.Errorf("电脑控制组件不支持后台目标协议 2，请更新 helper；已拒绝调用")
	}
	cmd := exec.CommandContext(c, path, cmdArgs...)
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(diagnostic.String()))
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
	BundleID string       `json:"bundle_id"`
	PID      int64        `json:"pid"`
	WindowID int64        `json:"window_id"`
	Actions  []actionStep `json:"actions"`
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
			key, err := normalizeKeyCombo(s.Key)
			if err != nil {
				return nil, fmt.Errorf("actions[%d]: %w", i, err)
			}
			wrapper.Actions[i].Key = key
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

// checkScreenPoint bounds coordinate magnitudes before invoking the helper.
// The helper re-checks against the target window bounds; this catches runaway
// model math before it reaches the OS.
func checkScreenPoint(x, y int) error {
	if x < -32768 || x > 32768 || y < -32768 || y > 32768 {
		return fmt.Errorf("坐标 (%d, %d) 超出合理屏幕范围", x, y)
	}
	return nil
}

// normalizeKeyCombo gives validation, confirmation and the helper one spelling.
func normalizeKeyCombo(key string) (string, error) {
	modifiers := map[string]bool{}
	base := ""
	for _, part := range strings.Split(strings.ToLower(key), "+") {
		part = strings.TrimSpace(part)
		switch part {
		case "cmd", "alt", "ctrl", "shift", "fn":
			if modifiers[part] {
				return "", fmt.Errorf("重复修饰键 %q", part)
			}
			modifiers[part] = true
			continue
		case "escape":
			part = "esc"
		case "return":
			part = "enter"
		}
		switch part {
		case "space", "enter", "tab", "esc", "delete", "backspace", "up", "down", "left", "right", "home", "end", "pageup", "pagedown", "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12":
		default:
			if len([]rune(part)) != 1 {
				return "", fmt.Errorf("key 组合含无法识别的部分 %q", part)
			}
		}
		if base != "" {
			return "", fmt.Errorf("key 组合仅支持一个普通按键")
		}
		base = part
	}
	if base == "" {
		return "", fmt.Errorf("key 需要普通按键")
	}
	parts := []string{}
	for _, modifier := range []string{"cmd", "ctrl", "alt", "shift", "fn"} {
		if modifiers[modifier] {
			parts = append(parts, modifier)
		}
	}
	return strings.Join(append(parts, base), "+"), nil
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
		return fmt.Sprintf("输入文本 %q", s.Text)
	case "key":
		return "按键 " + s.Key
	case "scroll":
		return fmt.Sprintf("滚动 dx=%d dy=%d", deref(s.Dx), deref(s.Dy))
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
	for _, step := range batch.Actions {
		if step.Type != "key" {
			continue
		}
		key, err := normalizeKeyCombo(step.Key)
		if err != nil {
			return true
		}
		parts := strings.Split(key, "+")
		keys := map[string]bool{}
		for _, part := range parts {
			keys[part] = true
		}
		// Extra modifiers must not bypass quit, logout, lock or force-quit approval.
		if keys["cmd"] && (keys["q"] || (keys["alt"] && keys["esc"])) {
			return true
		}
	}
	return false
}

// A helper result must reference a bounded PNG inside this tool's screenshot directory.
func (t *Tool) readScreenshot(path string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(t.shotsDir)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("截图路径超出专用目录")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16*1024*1024 {
		return nil, errors.New("截图文件无效或超过 16MB")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	dimensions, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || dimensions.Width < 1 || dimensions.Height < 1 || dimensions.Width*dimensions.Height > 32*1024*1024 {
		return nil, errors.New("截图不是有效的 PNG")
	}
	return data, nil
}
