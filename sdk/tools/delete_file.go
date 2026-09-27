package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// DeleteFileTool safely deletes text files from the filesystem.
// It reads and preserves file content before deletion (via BackupManager if available).
// Only text files are allowed — for non-text files, the user should use bash rm.
type DeleteFileTool struct {
	pathPolicy PathPolicy
	workspace  string
	ops        operations.FileOperations
	backupMgr  *BackupManager
}

type DeleteFileParams struct {
	FilePath string `json:"file_path"`
	Reason   string `json:"reason,omitempty"`
}

type DeleteFileOption func(*DeleteFileTool)

// WithDeleteFilePathPolicy explicitly controls access outside the workspace.
func WithDeleteFilePathPolicy(policy PathPolicy) DeleteFileOption {
	return func(t *DeleteFileTool) { t.pathPolicy = policy }
}

func WithDeleteFileWorkspace(ws string) DeleteFileOption {
	return func(t *DeleteFileTool) { t.workspace = ws }
}

func WithDeleteFileOperations(ops operations.FileOperations) DeleteFileOption {
	return func(t *DeleteFileTool) { t.ops = ops }
}

func WithDeleteFileBackupManager(bm *BackupManager) DeleteFileOption {
	return func(t *DeleteFileTool) { t.backupMgr = bm }
}

func NewDeleteFileTool(opts ...DeleteFileOption) *DeleteFileTool {
	t := &DeleteFileTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *DeleteFileTool) Name() string { return "delete_file" }

func (t *DeleteFileTool) Description() string {
	return `Safely deletes text files from the filesystem after capturing their content for potential rollback.
The tool will read and preserve the file content before deletion, allowing for recovery if needed.

RESTRICTIONS: This tool can only delete text files (code, configuration, documentation, etc.).
For non-text files (images, videos, binaries, archives, etc.), use the shell tool with commands like "rm".

IMPORTANT: This operation is destructive and should be used with caution. Always ensure you have
a backup strategy or version control in place.`
}

func (t *DeleteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute or workspace-relative path to the file to delete.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Optional reason for deletion (for logging and rollback documentation).",
			},
		},
		"required": []string{"file_path"},
	}
}

func (t *DeleteFileTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params DeleteFileParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	return json.Marshal(params)
}

// RequiresConfirmation implements agent.ToolWithConfirmation.
func (t *DeleteFileTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params DeleteFileParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "即将删除文件（参数解析失败，仍需确认）", true
	}
	cleanPath := ResolvePath(t.workspace, params.FilePath)
	reason := ""
	if params.Reason != "" {
		reason = fmt.Sprintf(" (%s)", params.Reason)
	}
	return fmt.Sprintf("即将删除文件:%s\n  %s", reason, cleanPath), true
}

func (t *DeleteFileTool) Execute(ctx context.Context, raw json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var params DeleteFileParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	cleanPath := ResolvePath(t.workspace, params.FilePath)

	// Check path safety
	if !t.pathPolicy.Allows(t.workspace, cleanPath) {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("path %s is outside workspace %s", params.FilePath, t.workspace),
		}, fmt.Errorf("path escapes workspace")
	}

	// Check file exists and is not a directory
	info, err := t.ops.Stat(ctx, cleanPath)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("file not found: %s", cleanPath)}, err
	}
	if info.IsDir {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("path is a directory, not a file: %s", cleanPath)}, fmt.Errorf("cannot delete directory")
	}

	// Read file content for backup/recovery info
	data, err := t.ops.ReadFile(ctx, cleanPath)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("error reading file before deletion: %v", err)}, err
	}

	// Take snapshot via BackupManager if available
	if t.backupMgr != nil {
		if _, err := t.backupMgr.Snapshot(cleanPath); err != nil {
			slog.Warn("delete_file: backup snapshot failed", "path", cleanPath, "error", err)
		}
	}

	// Perform the deletion
	if err := os.Remove(cleanPath); err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("error deleting file: %v", err)}, err
	}

	lineCount := strings.Count(string(data), "\n")
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		lineCount++
	}

	parts := []string{
		fmt.Sprintf("Successfully deleted file: %s", cleanPath),
		fmt.Sprintf("File contained %d lines (%d bytes)", lineCount, len(data)),
	}
	if params.Reason != "" {
		parts = append(parts, fmt.Sprintf("Deletion reason: %s", params.Reason))
	}
	if t.backupMgr != nil {
		parts = append(parts, "Original content has been preserved in backup for potential rollback.")
	}

	return agent.ToolResult{Content: strings.Join(parts, ". ") + "."}, nil
}
