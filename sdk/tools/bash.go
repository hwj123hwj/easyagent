package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

const DefaultBashTimeoutSeconds = 120

type BashTool struct {
	workspace        string // 工作目录限制，空字符串表示不限制
	maxOutputLen     int    // 最大输出长度，0 表示使用 DefaultMaxOutputLen
	progressInterval time.Duration
	ops              operations.BashOperations
}

type BashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// BashToolOption configures a BashTool during construction.
type BashToolOption func(*BashTool)

// WithBashWorkspace sets the working directory for command execution.
func WithBashWorkspace(ws string) BashToolOption {
	return func(t *BashTool) { t.workspace = ws }
}

// WithBashMaxOutputLen sets the max output truncation length.
func WithBashMaxOutputLen(n int) BashToolOption {
	return func(t *BashTool) { t.maxOutputLen = n }
}

// WithBashOperations sets the BashOperations backend.
func WithBashOperations(ops operations.BashOperations) BashToolOption {
	return func(t *BashTool) { t.ops = ops }
}

// NewBashTool creates BashTool with optional configuration.
// If no BashOperations is provided via WithBashOperations, defaults to LocalBashOperations.
func NewBashTool(opts ...BashToolOption) *BashTool {
	t := &BashTool{progressInterval: 5 * time.Second}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalBashOperations{}
	}
	return t
}

func (t *BashTool) Name() string { return "bash" }
func (t *BashTool) Description() string {
	return "Execute a non-interactive shell command on the server. No terminal or password input is available; use sudo -n when elevated access is needed."
}
func (t *BashTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "The shell command to execute."},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (default 120). Set a longer timeout for builds or tests."},
		},
		"required": []string{"command"},
	}
}
func (t *BashTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params BashParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if params.Timeout <= 0 {
		params.Timeout = DefaultBashTimeoutSeconds
	}
	return json.Marshal(params)
}

// RequiresConfirmation 实现 agent.ToolWithConfirmation。
// 对破坏性命令（rm -rf、覆盖重定向、sudo、远程脚本执行、磁盘操作等）要求用户确认；
// 普通命令（ls、echo、grep 等）直接放行，避免过度打扰。
func (t *BashTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params BashParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "", false // 解析失败交给 Validate 报错，不在此阻断
	}
	if reason := whyDangerous(params.Command); reason != "" {
		return fmt.Sprintf("即将执行 shell 命令（%s）:\n  %s", reason, params.Command), true
	}
	return "", false
}
func (t *BashTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params BashParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	if params.Timeout <= 0 {
		params.Timeout = DefaultBashTimeoutSeconds
	}

	req := operations.RunRequest{
		Command: params.Command,
		Timeout: time.Duration(params.Timeout) * time.Second,
		WorkDir: t.workspace,
	}

	stopProgress := t.reportProgress(ctx, params.Timeout, onUpdate)
	defer stopProgress()
	result, runErr := t.ops.Run(ctx, req)

	output := string(result.Output)

	// Keep output and failure together so the model sees both the cause and progress.
	if isBinaryOutput(output) {
		output = fmt.Sprintf("Command produced binary output (%d bytes). Use file redirection to save output.", len(result.Output))
	} else {
		output = TruncateOutput(stripANSI(output), t.maxOutputLen)
	}
	var failure error
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		if ctx.Err() != nil {
			failure = fmt.Errorf("command interrupted: request deadline exceeded: %w", runErr)
		} else {
			failure = fmt.Errorf("command timed out after %s: %w", req.Timeout, runErr)
		}
	case errors.Is(runErr, context.Canceled):
		failure = fmt.Errorf("command canceled: %w", runErr)
	case runErr != nil:
		failure = runErr
	case result.ExitCode != 0:
		failure = fmt.Errorf("command exited with code %d", result.ExitCode)
	}
	if failure != nil {
		if output != "" {
			output += "\n\n"
		}
		output += failure.Error()
		return agent.ToolResult{Content: output, IsError: true}, failure
	}

	return agent.ToolResult{Content: output}, nil
}

// ansiRegex matches ANSI escape sequences.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// isBinaryOutput checks if the output appears to be binary data.
func isBinaryOutput(s string) bool {
	// Check for null bytes in first 512 bytes
	checkLen := len(s)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}

// Even silent commands report that they are still waiting; this is elapsed time,
// not invented output or a completion estimate. Stop and join before tool_end.
func (t *BashTool) reportProgress(ctx context.Context, timeout int, update func(agent.PartialResult)) func() {
	if update == nil {
		return func() {}
	}
	interval := t.progressInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				message := fmt.Sprintf("命令仍在执行 · 已等待 %ds", int(time.Since(start).Seconds()))
				if timeout > 0 {
					message += fmt.Sprintf(" · 本次超时 %ds", timeout)
				}
				update(agent.PartialResult{Content: message})
			}
		}
	}()
	return func() { close(stop); <-done }
}
