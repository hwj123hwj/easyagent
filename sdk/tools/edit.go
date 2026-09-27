package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// MutationQueue 是 EditTool/WriteTool 的 per-file 串行化抽象。
// 定义在此处以避免循环依赖（agent 包不应依赖 coding 包）。
type MutationQueue interface {
	Execute(ctx context.Context, filePath string, fn func() (agent.ToolResult, error)) (agent.ToolResult, error)
}

// EditTool performs exact string replacements in files.
// Supports single replacement (old_string must be unique) and replace_all mode.
// If file doesn't exist and old_string is empty, creates a new file.
type EditTool struct {
	pathPolicy    PathPolicy
	workspace     string // 工作目录，用于解析相对路径
	ops           operations.FileOperations
	mutationQueue MutationQueue  // 可选：per-file 串行化
	backupMgr     *BackupManager // 可选：操作前自动快照
}

type EditParams struct {
	Path       string      `json:"path"`
	OldString  string      `json:"old_string"`
	NewString  string      `json:"new_string"`
	ReplaceAll bool        `json:"replace_all,omitempty"`
	Edits      []EditEntry `json:"edits,omitempty"` // 多编辑模式：与 old_string/new_string 互斥
}

// EditEntry 表示多编辑模式中的一个替换操作。
type EditEntry struct {
	OldString string `json:"old_string"` // 在原始文件中必须唯一出现
	NewString string `json:"new_string"`
}

// EditToolOption configures an EditTool during construction.
type EditToolOption func(*EditTool)

// WithEditPathPolicy explicitly controls access outside the workspace.
func WithEditPathPolicy(policy PathPolicy) EditToolOption {
	return func(t *EditTool) { t.pathPolicy = policy }
}

// WithEditWorkspace sets the workspace for path resolution.
func WithEditWorkspace(ws string) EditToolOption {
	return func(t *EditTool) { t.workspace = ws }
}

// WithEditOperations sets the FileOperations backend.
func WithEditOperations(ops operations.FileOperations) EditToolOption {
	return func(t *EditTool) { t.ops = ops }
}

// WithEditMutationQueue sets the per-file mutation queue for serialized writes.
func WithEditMutationQueue(q MutationQueue) EditToolOption {
	return func(t *EditTool) { t.mutationQueue = q }
}

// WithEditBackupManager sets the backup manager for auto-snapshot before edits.
func WithEditBackupManager(bm *BackupManager) EditToolOption {
	return func(t *EditTool) { t.backupMgr = bm }
}

func NewEditTool(opts ...EditToolOption) *EditTool {
	t := &EditTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *EditTool) Name() string { return "edit" }

func (t *EditTool) Description() string {
	return "Perform exact string replacements in files. Supports single replacement (old_string must be unique), replace_all mode, and multi-edit mode (edits array for multiple replacements in one call)."
}

func (t *EditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Absolute path to the file to edit."},
			"old_string":  map[string]any{"type": "string", "description": "The text to replace (single-edit mode). Must match exactly, including whitespace and indentation."},
			"new_string":  map[string]any{"type": "string", "description": "The text to replace with (single-edit mode)."},
			"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (single-edit mode, default false)."},
			"edits": map[string]any{
				"type":        "array",
				"description": "Multi-edit mode: array of replacements to apply in one call. Each old_string must be unique in the original file. Mutually exclusive with old_string/new_string.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"old_string": map[string]any{"type": "string", "description": "The text to replace. Must be unique in the file."},
						"new_string": map[string]any{"type": "string", "description": "The replacement text."},
					},
					"required": []string{"old_string", "new_string"},
				},
			},
		},
		"required": []string{"path"},
	}
}

func (t *EditTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params EditParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if len(params.Edits) > 0 {
		// 多编辑模式：不允许同时提供 old_string/new_string
		if params.OldString != "" || params.NewString != "" {
			return nil, fmt.Errorf("cannot provide both edits[] and old_string/new_string; use one or the other")
		}
		for i, e := range params.Edits {
			if e.OldString == "" {
				return nil, fmt.Errorf("edits[%d]: old_string is required", i)
			}
		}
	} else {
		// 单编辑模式：必须有 old_string/new_string。
		// 注意：old_string="" 是合法的（用于创建新文件），
		// 但 new_string 也为空则无意义。
		// 我们允许 old_string=""（创建文件），不做额外限制。
	}
	return json.Marshal(params)
}

// RequiresConfirmation 实现 agent.ToolWithConfirmation。
// 编辑文件会修改已有内容，无条件要求用户确认。
func (t *EditTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params EditParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "即将编辑文件（参数解析失败，仍需确认）", true
	}
	cleanPath := ResolvePath(t.workspace, params.Path)
	if len(params.Edits) > 0 {
		return fmt.Sprintf("即将编辑文件（%d 处替换）:\n  %s", len(params.Edits), cleanPath), true
	}
	return fmt.Sprintf("即将编辑文件:\n  %s", cleanPath), true
}

func (t *EditTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	// 若有 mutation queue，先解析 path 以确定 queue key
	if t.mutationQueue != nil {
		var params EditParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return agent.ToolResult{IsError: true}, err
		}
		cleanPath := ResolvePath(t.workspace, params.Path)
		return t.mutationQueue.Execute(ctx, cleanPath, func() (agent.ToolResult, error) {
			return t.doExecute(ctx, raw, onUpdate)
		})
	}
	return t.doExecute(ctx, raw, onUpdate)
}

func (t *EditTool) doExecute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params EditParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	cleanPath := ResolvePath(t.workspace, params.Path)

	// Check path safety if workspace is set
	if !t.pathPolicy.Allows(t.workspace, cleanPath) {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("path %s is outside workspace %s", params.Path, t.workspace),
		}, fmt.Errorf("path escapes workspace")
	}

	// Auto-snapshot before modification (if backup manager is set)
	if t.backupMgr != nil {
		if _, err := t.backupMgr.Snapshot(cleanPath); err != nil {
			// Non-fatal: log but continue with the edit
			_ = err
		}
	}

	// Read existing file
	data, err := t.ops.ReadFile(ctx, cleanPath)
	if err != nil {
		// File doesn't exist: create new file if old_string is empty
		if isNotExist(err) && params.OldString == "" {
			if err := t.ops.MkdirAll(ctx, parentDir(cleanPath), 0o755); err != nil {
				return agent.ToolResult{IsError: true}, err
			}
			if err := t.ops.WriteFile(ctx, cleanPath, []byte(params.NewString), 0o644); err != nil {
				return agent.ToolResult{IsError: true}, err
			}
			return agent.ToolResult{Content: fmt.Sprintf("created %s", cleanPath)}, nil
		}
		return agent.ToolResult{IsError: true}, err
	}

	content := string(data)

	// 批量编辑模式
	if len(params.Edits) > 0 {
		newContent, err := applyEdits(content, params.Edits)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: err.Error()}, err
		}
		if err := t.ops.WriteFile(ctx, cleanPath, []byte(newContent), 0o644); err != nil {
			return agent.ToolResult{IsError: true}, err
		}
		return agent.ToolResult{
			Content: fmt.Sprintf("edited %s (%d edits applied)", cleanPath, len(params.Edits)),
		}, nil
	}

	// Check old_string exists
	if !strings.Contains(content, params.OldString) {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("old_string not found in %s", cleanPath),
		}, fmt.Errorf("old_string not found in %s", cleanPath)
	}

	count := strings.Count(content, params.OldString)

	if params.ReplaceAll {
		// Replace all occurrences
		newContent := strings.ReplaceAll(content, params.OldString, params.NewString)
		if err := t.ops.WriteFile(ctx, cleanPath, []byte(newContent), 0o644); err != nil {
			return agent.ToolResult{IsError: true}, err
		}

		return agent.ToolResult{
			Content: fmt.Sprintf("edited %s (%d replacements)", cleanPath, count),
		}, nil
	}

	// Single replacement: require uniqueness
	if count > 1 {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("old_string appears %d times in %s; it must be unique. Add more surrounding context to make it unique, or use replace_all.", count, cleanPath),
		}, fmt.Errorf("old_string is not unique (found %d occurrences) in %s", count, cleanPath)
	}

	newContent := strings.Replace(content, params.OldString, params.NewString, 1)
	if err := t.ops.WriteFile(ctx, cleanPath, []byte(newContent), 0o644); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	// Show diff context
	oldLines := strings.Count(params.OldString, "\n") + 1
	newLines := strings.Count(params.NewString, "\n") + 1
	before := content[:strings.Index(content, params.OldString)]
	startLine := strings.Count(before, "\n") + 1
	endLine := startLine + oldLines - 1

	// Collect context lines around the change
	allLines := strings.Split(newContent, "\n")
	ctxStart := startLine - 3
	if ctxStart < 1 {
		ctxStart = 1
	}
	ctxEnd := endLine + 3
	if ctxEnd > len(allLines) {
		ctxEnd = len(allLines)
	}

	var diffCtx strings.Builder
	diffCtx.WriteString(fmt.Sprintf("edited %s (lines %d-%d, %d→%d lines)\n\n", cleanPath, startLine, endLine, oldLines, newLines))
	for i := ctxStart; i <= ctxEnd; i++ {
		marker := "  "
		if i >= startLine && i <= startLine+newLines-1 {
			marker = "> "
		}
		if i <= len(allLines) {
			line := allLines[i-1]
			if len(line) > 120 {
				line = line[:120] + "..."
			}
			diffCtx.WriteString(fmt.Sprintf("%s%4d | %s\n", marker, i, line))
		}
	}

	return agent.ToolResult{
		Content: diffCtx.String(),
	}, nil
}

// isNotExist checks if an error indicates a file does not exist.
// Works for both local errors (os.IsNotExist) and operations-level errors.
func isNotExist(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	// For SSH operations where the error is a string from remote
	msg := err.Error()
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not exist")
}

// applyEdits 对 content 应用多个编辑操作。
// 所有 old_string 匹配原始文件内容（非增量匹配）。
// 匹配失败或重叠时返回错误，成功时返回替换后的完整内容。
func applyEdits(content string, edits []EditEntry) (string, error) {
	type match struct {
		index int // 在 edits 中的下标
		start int // 在 content 中的起始位置
		end   int // 在 content 中的结束位置
	}

	var matches []match

	// 1. 校验所有 old_string 存在且唯一
	for i, e := range edits {
		idx := strings.Index(content, e.OldString)
		if idx < 0 {
			return "", fmt.Errorf("edits[%d]: old_string not found in file", i)
		}
		count := strings.Count(content, e.OldString)
		if count > 1 {
			return "", fmt.Errorf("edits[%d]: old_string appears %d times (must be unique)", i, count)
		}
		matches = append(matches, match{index: i, start: idx, end: idx + len(e.OldString)})
	}

	// 2. 按位置从大到小排序（从后往前替换）
	sort.Slice(matches, func(i, j int) bool { return matches[i].start > matches[j].start })

	// 3. 重叠检测：两个匹配区域不能有交集
	for i := 1; i < len(matches); i++ {
		if matches[i].end > matches[i-1].start {
			return "", fmt.Errorf("edits[%d] and edits[%d] have overlapping matches", matches[i].index, matches[i-1].index)
		}
	}

	// 4. 从后往前替换（后面的替换不影响前面文本的偏移量）
	result := content
	for _, m := range matches {
		result = result[:m.start] + edits[m.index].NewString + result[m.end:]
	}
	return result, nil
}
