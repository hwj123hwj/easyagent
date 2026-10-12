package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

type ReadTool struct {
	pathPolicy   PathPolicy
	workspace    string // 工作目录，用于解析相对路径
	maxOutputLen int    // 最大输出长度，0 表示使用 DefaultMaxOutputLen
	ops          operations.FileOperations
	readTracker  *ReadTracker
}

type ReadParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ReadToolOption configures a ReadTool during construction.
type ReadToolOption func(*ReadTool)

// WithReadTracker sets the ReadTracker to record read files.
func WithReadTracker(tracker *ReadTracker) ReadToolOption {
	return func(t *ReadTool) { t.readTracker = tracker }
}

// WithReadPathPolicy explicitly controls access outside the workspace.
func WithReadPathPolicy(policy PathPolicy) ReadToolOption {
	return func(t *ReadTool) { t.pathPolicy = policy }
}

// WithReadWorkspace sets the workspace for path resolution.
func WithReadWorkspace(ws string) ReadToolOption {
	return func(t *ReadTool) { t.workspace = ws }
}

// WithReadMaxOutputLen sets the max output truncation length.
func WithReadMaxOutputLen(n int) ReadToolOption {
	return func(t *ReadTool) { t.maxOutputLen = n }
}

// WithReadOperations sets the FileOperations backend.
func WithReadOperations(ops operations.FileOperations) ReadToolOption {
	return func(t *ReadTool) { t.ops = ops }
}

func NewReadTool(opts ...ReadToolOption) *ReadTool {
	t := &ReadTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}
func (t *ReadTool) Name() string        { return "read" }
func (t *ReadTool) Description() string { return "Read a file from disk." }
func (t *ReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Absolute or relative path to the file."},
			"offset": map[string]any{"type": "integer", "description": "Starting line number (0-indexed, default 0)."},
			"limit":  map[string]any{"type": "integer", "description": "Maximum number of lines to read (default 2000)."},
		},
		"required": []string{"path"},
	}
}
func (t *ReadTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params ReadParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if params.Limit <= 0 {
		params.Limit = 2000
	}
	return json.Marshal(params)
}
func (t *ReadTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params ReadParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	// Resolve path against workspace
	cleanPath := ResolvePath(t.workspace, params.Path)

	// Check path safety if workspace is set
	if !t.pathPolicy.Allows(t.workspace, cleanPath) {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("path %s is outside workspace %s", params.Path, t.workspace),
		}, fmt.Errorf("path escapes workspace")
	}

	data, err := t.ops.ReadFile(ctx, cleanPath)
	if err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	if t.readTracker != nil {
		var modTime time.Time
		if stat, statErr := t.ops.Stat(ctx, cleanPath); statErr == nil {
			modTime = stat.ModTime
		}
		t.readTracker.Record(cleanPath, data, modTime)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	start := params.Offset
	if start < 0 {
		start = 0
	}
	if start > totalLines {
		start = totalLines
	}
	end := start + params.Limit
	if end > totalLines {
		end = totalLines
	}

	// Output with line numbers, capped at the output limit on line boundaries
	// so the model never sees a partial line and the continuation hint is exact.
	selectedLines := lines[start:end]
	maxLen := t.maxOutputLen
	if maxLen <= 0 {
		maxLen = DefaultMaxOutputLen
	}
	var b strings.Builder
	kept := 0
	for i, line := range selectedLines {
		formatted := fmt.Sprintf("%6d\t%s\n", start+i+1, line)
		if len(formatted) > maxLen {
			// 单行超限（minified 文件等）：rune 安全硬切该行，停在行尾。
			formatted = truncateByteCap(formatted, maxLen)
		} else if kept > 0 && b.Len()+len(formatted) > maxLen {
			break // 剩余行放不下，在行边界收尾，续读提示在循环外补
		}
		b.WriteString(formatted)
		kept++
	}
	if kept < len(selectedLines) {
		nextOffset := start + kept
		b.WriteString(fmt.Sprintf(
			"\n... output capped at %d characters: showing lines %d-%d of %d; continue with offset=%d (and a smaller limit when lines are very long)\n",
			maxLen, start+1, nextOffset, totalLines, nextOffset))
	} else {
		// Hint about remaining lines for large files
		remaining := totalLines - end
		if remaining > 0 {
			b.WriteString(fmt.Sprintf("\n... %d more lines (use offset=%d to read further)\n", remaining, end))
		}
	}

	return agent.ToolResult{Content: b.String()}, nil
}

// IsConcurrencySafe implements agent.ConcurrencySafeChecker.
// ReadTool is always safe to execute concurrently.
func (t *ReadTool) IsConcurrencySafe(params json.RawMessage) bool {
	return true
}

// Format is used by prompt_info to give snippet
func lineCount(s string) string {
	return strconv.Itoa(len(strings.Split(s, "\n")))
}
