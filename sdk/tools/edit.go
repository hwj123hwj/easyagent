package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

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
	readTracker   *ReadTracker   // 可选：检测读后外部修改保护
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

// WithEditReadTracker sets the ReadTracker for stale-read detection.
func WithEditReadTracker(tracker *ReadTracker) EditToolOption {
	return func(t *EditTool) { t.readTracker = tracker }
}

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

	// Check stale read if readTracker is set
	if t.readTracker != nil {
		var modTime time.Time
		if stat, statErr := t.ops.Stat(ctx, cleanPath); statErr == nil {
			modTime = stat.ModTime
		}
		if isStale, recordedHash, currentHash := t.readTracker.CheckStale(cleanPath, data, modTime); isStale {
			errMsg := fmt.Sprintf("file %s was modified externally since last read (recorded hash: %.8s..., current hash: %.8s...); re-read the file before editing",
				cleanPath, recordedHash, currentHash)
			return agent.ToolResult{
				IsError: true,
				Content: errMsg,
			}, fmt.Errorf("%s", errMsg)
		}
	}

	// 批量编辑模式
	if len(params.Edits) > 0 {
		newContent, err := applyEdits(content, params.Edits)
		if err != nil {
			return agent.ToolResult{IsError: true, Content: err.Error()}, err
		}
		if err := t.ops.WriteFile(ctx, cleanPath, []byte(newContent), 0o644); err != nil {
			return agent.ToolResult{IsError: true}, err
		}
		if t.readTracker != nil {
			t.readTracker.Invalidate(cleanPath)
		}
		return agent.ToolResult{
			Content: fmt.Sprintf("edited %s (%d edits applied)", cleanPath, len(params.Edits)),
		}, nil
	}

	matchIdx, count := resolveOldStringMatch(content, params.OldString)
	if matchIdx < 0 {
		diagnostic := buildNotFoundDiagnostic(content, params.OldString)
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("%s in %s", diagnostic, cleanPath),
		}, fmt.Errorf("%s in %s", diagnostic, cleanPath)
	}

	matchedOldString := params.OldString
	if !strings.Contains(content, params.OldString) {
		// Use the matched substring (e.g. trimmed or stripped line numbers)
		if strings.Contains(content, strings.Trim(params.OldString, "\r\n")) {
			matchedOldString = strings.Trim(params.OldString, "\r\n")
		} else if strings.Contains(content, stripLineNumbers(params.OldString)) {
			matchedOldString = stripLineNumbers(params.OldString)
		} else {
			// normalized line endings
			matchedOldString = strings.ReplaceAll(params.OldString, "\r\n", "\n")
		}
	}

	if params.ReplaceAll {
		// Replace all occurrences
		newContent := strings.ReplaceAll(content, matchedOldString, params.NewString)
		if err := t.ops.WriteFile(ctx, cleanPath, []byte(newContent), 0o644); err != nil {
			return agent.ToolResult{IsError: true}, err
		}
		if t.readTracker != nil {
			t.readTracker.Invalidate(cleanPath)
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

	newContent := strings.Replace(content, matchedOldString, params.NewString, 1)
	if err := t.ops.WriteFile(ctx, cleanPath, []byte(newContent), 0o644); err != nil {
		return agent.ToolResult{IsError: true}, err
	}
	if t.readTracker != nil {
		t.readTracker.Invalidate(cleanPath)
	}

	// Show diff context
	oldLines := strings.Count(matchedOldString, "\n") + 1
	newLines := strings.Count(params.NewString, "\n") + 1
	before := content[:strings.Index(content, matchedOldString)]
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
		idx, count := resolveOldStringMatch(content, e.OldString)
		if idx < 0 {
			return "", fmt.Errorf("edits[%d]: %s", i, buildNotFoundDiagnostic(content, e.OldString))
		}
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

var lineNumPrefixRe = regexp.MustCompile(`(?m)^\s*\d+[\t:|\s]\s*`)

// stripLineNumbers removes leading line numbers like "  12 | " or "12\t" that LLMs sometimes copy from tool output.
func stripLineNumbers(s string) string {
	if !lineNumPrefixRe.MatchString(s) {
		return s
	}
	return lineNumPrefixRe.ReplaceAllString(s, "")
}

// resolveOldStringMatch performs exact matching first, followed by safe fallbacks
// (trimming extra newline/spaces, stripping accidental line numbers, CRLF/LF normalization).
// It returns the match index and count in content.
// Crucially, it adheres to the safety invariant: ambiguous matches (count > 1) are never guessed.
func resolveOldStringMatch(content, target string) (matchIndex int, matchCount int) {
	if target == "" {
		return -1, 0
	}

	// 1. Exact match
	if strings.Contains(content, target) {
		return strings.Index(content, target), strings.Count(content, target)
	}

	// 2. Normalizing CRLF vs LF
	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	normalizedTarget := strings.ReplaceAll(target, "\r\n", "\n")
	if normalizedContent != content || normalizedTarget != target {
		if strings.Contains(content, normalizedTarget) {
			return strings.Index(content, normalizedTarget), strings.Count(content, normalizedTarget)
		}
	}

	// 3. Trim leading/trailing newlines
	trimmedTarget := strings.Trim(target, "\r\n")
	if trimmedTarget != "" && trimmedTarget != target {
		if strings.Contains(content, trimmedTarget) {
			count := strings.Count(content, trimmedTarget)
			return strings.Index(content, trimmedTarget), count
		}
	}

	// 4. Strip line numbers accidentally copied from read tool
	strippedTarget := stripLineNumbers(target)
	if strippedTarget != "" && strippedTarget != target {
		if strings.Contains(content, strippedTarget) {
			count := strings.Count(content, strippedTarget)
			return strings.Index(content, strippedTarget), count
		}
	}

	return -1, 0
}

// buildNotFoundDiagnostic generates an informative diagnostic message when old_string is not found.
func buildNotFoundDiagnostic(content, oldString string) string {
	var b strings.Builder
	b.WriteString("old_string not found in file")

	// Check if stripping line numbers or trimming would match
	stripped := stripLineNumbers(oldString)
	trimmed := strings.Trim(oldString, "\r\n")
	if (stripped != oldString && strings.Contains(content, stripped)) ||
		(trimmed != oldString && strings.Contains(content, trimmed)) {
		b.WriteString(" (hint: old_string may contain extraneous line numbers or leading/trailing newlines)")
		return b.String()
	}

	// Find the closest line in content to oldString's first non-empty line
	lines := strings.Split(content, "\n")
	oldLines := strings.Split(strings.TrimSpace(oldString), "\n")
	if len(oldLines) > 0 && len(lines) > 0 {
		firstOldLine := strings.TrimSpace(oldLines[0])
		if len(firstOldLine) > 0 {
			firstWords := strings.Fields(firstOldLine)
			var candidateLines []string
			for idx, line := range lines {
				trimmedLine := strings.TrimSpace(line)
				if trimmedLine == "" {
					continue
				}
				matched := false
				if trimmedLine == firstOldLine || strings.Contains(trimmedLine, firstOldLine) || strings.Contains(firstOldLine, trimmedLine) {
					matched = true
				} else if len(firstWords) > 0 {
					// Check if any significant word/prefix matches
					for _, w := range firstWords {
						if len(w) >= 3 && strings.Contains(trimmedLine, w) {
							matched = true
							break
						}
					}
				}

				if matched {
					candidateLines = append(candidateLines, fmt.Sprintf("line %d: %s", idx+1, strings.TrimSpace(line)))
					if len(candidateLines) >= 3 {
						break
					}
				}
			}
			if len(candidateLines) > 0 {
				b.WriteString(fmt.Sprintf(". Nearest line match:\n  %s", strings.Join(candidateLines, "\n  ")))
			}
		}
	}

	return b.String()
}
