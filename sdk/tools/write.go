package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

type WriteTool struct {
	pathPolicy    PathPolicy
	workspace     string // 工作目录，用于解析相对路径
	ops           operations.FileOperations
	mutationQueue MutationQueue  // 可选：per-file 串行化
	backupMgr     *BackupManager // 可选：操作前自动快照
	readTracker   *ReadTracker   // 可选：检测读后外部修改保护
}

type WriteParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteToolOption configures a WriteTool during construction.
type WriteToolOption func(*WriteTool)

// WithWriteReadTracker sets the ReadTracker for stale-read detection.
func WithWriteReadTracker(tracker *ReadTracker) WriteToolOption {
	return func(t *WriteTool) { t.readTracker = tracker }
}

// WithWritePathPolicy explicitly controls access outside the workspace.
func WithWritePathPolicy(policy PathPolicy) WriteToolOption {
	return func(t *WriteTool) { t.pathPolicy = policy }
}

// WithWriteWorkspace sets the workspace for path resolution.
func WithWriteWorkspace(ws string) WriteToolOption {
	return func(t *WriteTool) { t.workspace = ws }
}

// WithWriteOperations sets the FileOperations backend.
func WithWriteOperations(ops operations.FileOperations) WriteToolOption {
	return func(t *WriteTool) { t.ops = ops }
}

// WithWriteMutationQueue sets the per-file mutation queue for serialized writes.
func WithWriteMutationQueue(q MutationQueue) WriteToolOption {
	return func(t *WriteTool) { t.mutationQueue = q }
}

// WithWriteBackupManager sets the backup manager for auto-snapshot before writes.
func WithWriteBackupManager(bm *BackupManager) WriteToolOption {
	return func(t *WriteTool) { t.backupMgr = bm }
}

func NewWriteTool(opts ...WriteToolOption) *WriteTool {
	t := &WriteTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}
func (t *WriteTool) Name() string        { return "write" }
func (t *WriteTool) Description() string { return "Write a file to disk." }
func (t *WriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Absolute path to the file to write."},
			"content": map[string]any{"type": "string", "description": "The content to write."},
		},
		"required": []string{"path", "content"},
	}
}
func (t *WriteTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params WriteParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return json.Marshal(params)
}

// RequiresConfirmation 实现 agent.ToolWithConfirmation。
// 写文件会覆盖目标路径已有内容，无条件要求用户确认。
func (t *WriteTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params WriteParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "即将写入文件（参数解析失败，仍需确认）", true
	}
	cleanPath := ResolvePath(t.workspace, params.Path)
	return fmt.Sprintf("即将写入文件（可能覆盖已有内容）:\n  %s (%s)",
		cleanPath, FormatByteCount(len(params.Content))), true
}
func (t *WriteTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	if t.mutationQueue != nil {
		var params WriteParams
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

func (t *WriteTool) doExecute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params WriteParams
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

	// Check stale read if file exists and readTracker is set
	if t.readTracker != nil {
		if currentData, readErr := t.ops.ReadFile(ctx, cleanPath); readErr == nil {
			var modTime time.Time
			if stat, statErr := t.ops.Stat(ctx, cleanPath); statErr == nil {
				modTime = stat.ModTime
			}
			if isStale, recordedHash, currentHash := t.readTracker.CheckStale(cleanPath, currentData, modTime); isStale {
				errMsg := fmt.Sprintf("file %s was modified externally since last read (recorded hash: %.8s..., current hash: %.8s...); re-read the file before writing",
					cleanPath, recordedHash, currentHash)
				return agent.ToolResult{
					IsError: true,
					Content: errMsg,
				}, fmt.Errorf("%s", errMsg)
			}
		} else if !isNotExist(readErr) || t.readTracker.HasRead(cleanPath) {
			return agent.ToolResult{IsError: true}, fmt.Errorf("cannot verify %s before writing: %w", cleanPath, readErr)
		}
	}

	// Auto-snapshot before modification (if backup manager is set)
	if t.backupMgr != nil {
		if _, err := t.backupMgr.Snapshot(cleanPath); err != nil {
			// Non-fatal: log but continue with the write
			_ = err
		}
	}

	// Ensure parent directory exists
	if err := t.ops.MkdirAll(ctx, parentDir(cleanPath), 0o755); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	content := []byte(params.Content)
	if err := t.ops.WriteFile(ctx, cleanPath, content, 0o644); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	if t.readTracker != nil {
		t.readTracker.Record(cleanPath, content, time.Time{})
	}

	// Count bytes and lines
	byteCount := len(content)
	lineCount := strings.Count(params.Content, "\n")
	if !strings.HasSuffix(params.Content, "\n") && len(params.Content) > 0 {
		lineCount++
	}

	return agent.ToolResult{
		Content: fmt.Sprintf("written %s (%s, %d lines)", cleanPath, FormatByteCount(byteCount), lineCount),
	}, nil
}
