package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// MultiEditTool performs multiple edits sequentially on the same file or across multiple files.
// It is transactional per file: if any edit fails, all edits to that file are rolled back.
type MultiEditTool struct {
	pathPolicy  PathPolicy
	workspace   string
	ops         operations.FileOperations
	backupMgr   *BackupManager
	readTracker *ReadTracker
}

// MultiEditSingleEntry represents one edit operation within a multi-edit call.
type MultiEditSingleEntry struct {
	FilePath   string `json:"file_path,omitempty"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type MultiEditParams struct {
	FilePath string                 `json:"file_path"`
	Edits    []MultiEditSingleEntry `json:"edits"`
}

type MultiEditOption func(*MultiEditTool)

// WithMultiEditReadTracker sets the ReadTracker for stale-read detection.
func WithMultiEditReadTracker(tracker *ReadTracker) MultiEditOption {
	return func(t *MultiEditTool) { t.readTracker = tracker }
}

// WithMultiEditPathPolicy explicitly controls access outside the workspace.
func WithMultiEditPathPolicy(policy PathPolicy) MultiEditOption {
	return func(t *MultiEditTool) { t.pathPolicy = policy }
}

func WithMultiEditWorkspace(ws string) MultiEditOption {
	return func(t *MultiEditTool) { t.workspace = ws }
}

func WithMultiEditOperations(ops operations.FileOperations) MultiEditOption {
	return func(t *MultiEditTool) { t.ops = ops }
}

func WithMultiEditBackupManager(bm *BackupManager) MultiEditOption {
	return func(t *MultiEditTool) { t.backupMgr = bm }
}

func NewMultiEditTool(opts ...MultiEditOption) *MultiEditTool {
	t := &MultiEditTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *MultiEditTool) Name() string { return "multiedit" }

func (t *MultiEditTool) Description() string {
	return `Perform multiple edits sequentially on the same file or across multiple files. The "edits" parameter MUST be an array of objects, NOT strings.

Each edit object has: "old_string" (exact text to find), "new_string" (replacement text), and optional "file_path" (overrides the top-level file_path) and "replace_all" (boolean).

All edits are applied sequentially. If any edit fails (e.g. old_string not found), the tool rolls back all changes to the file and reports the error.`
}

func (t *MultiEditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the primary file to modify (used when edit entries don't specify their own file_path).",
			},
			"edits": map[string]any{
				"type":        "array",
				"description": "Array of edit objects to perform sequentially. DO NOT stringify the objects inside this array.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path":   map[string]any{"type": "string", "description": "Optional override: absolute path to the file for this edit."},
						"old_string":  map[string]any{"type": "string", "description": "The exact literal text to replace."},
						"new_string":  map[string]any{"type": "string", "description": "The text to replace it with."},
						"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (default false)."},
					},
					"required": []string{"old_string", "new_string"},
				},
			},
		},
		"required": []string{"file_path", "edits"},
	}
}

func (t *MultiEditTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params MultiEditParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	if len(params.Edits) == 0 {
		return nil, fmt.Errorf("at least one edit is required")
	}
	for i, e := range params.Edits {
		if e.OldString == "" {
			return nil, fmt.Errorf("edits[%d]: old_string is required", i)
		}
	}
	return json.Marshal(params)
}

// RequiresConfirmation implements agent.ToolWithConfirmation.
func (t *MultiEditTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params MultiEditParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "即将执行多文件编辑（参数解析失败，仍需确认）", true
	}
	cleanPath := ResolvePath(t.workspace, params.FilePath)
	return fmt.Sprintf("即将编辑文件（%d 处替换）:\n  %s", len(params.Edits), cleanPath), true
}

func (t *MultiEditTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params MultiEditParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	// Group edits by target file for transactional handling
	type fileEdits struct {
		path  string
		edits []MultiEditSingleEntry
	}
	fileGroups := make(map[string]*fileEdits)
	groupOrder := []string{} // preserve order

	for _, edit := range params.Edits {
		editPath := edit.FilePath
		if editPath == "" {
			editPath = params.FilePath
		}
		cleanPath := ResolvePath(t.workspace, editPath)

		if _, exists := fileGroups[cleanPath]; !exists {
			fileGroups[cleanPath] = &fileEdits{path: cleanPath}
			groupOrder = append(groupOrder, cleanPath)
		}
		fileGroups[cleanPath].edits = append(fileGroups[cleanPath].edits, edit)
	}

	// Process each file transactionally
	var allResults []string
	editsApplied := 0

	for _, filePath := range groupOrder {
		group := fileGroups[filePath]
		result, applied, err := t.applyEditsToFile(ctx, filePath, group.edits)
		if err != nil {
			return agent.ToolResult{
				IsError: true,
				Content: fmt.Sprintf("Multi-edit failed on %s after %d edits applied: %s\n\nResults so far:\n%s",
					filePath, applied, err.Error(), strings.Join(allResults, "\n")),
			}, err
		}
		allResults = append(allResults, result)
		editsApplied += applied
	}

	return agent.ToolResult{
		Content: fmt.Sprintf("Executed %d edits across %d file(s).\n%s",
			editsApplied, len(fileGroups), strings.Join(allResults, "\n")),
	}, nil
}

// applyEditsToFile applies multiple edits to a single file with transactional rollback.
func (t *MultiEditTool) applyEditsToFile(ctx context.Context, filePath string, edits []MultiEditSingleEntry) (string, int, error) {
	// Path safety check
	if !t.pathPolicy.Allows(t.workspace, filePath) {
		return "", 0, fmt.Errorf("path %s is outside workspace", filePath)
	}

	// Read file content
	data, err := t.ops.ReadFile(ctx, filePath)
	if err != nil {
		return "", 0, fmt.Errorf("read file: %w", err)
	}
	originalContent := string(data)

	// Check stale read if file was previously read
	if t.readTracker != nil {
		var modTime time.Time
		if stat, statErr := t.ops.Stat(ctx, filePath); statErr == nil {
			modTime = stat.ModTime
		}
		if isStale, recordedHash, currentHash := t.readTracker.CheckStale(filePath, data, modTime); isStale {
			return "", 0, fmt.Errorf("file %s was modified externally since last read (recorded hash: %.8s..., current hash: %.8s...); re-read the file before editing",
				filePath, recordedHash, currentHash)
		}
	}

	// Take snapshot for backup
	if t.backupMgr != nil {
		if _, err := t.backupMgr.Snapshot(filePath); err != nil {
			slog.Warn("multiedit: backup snapshot failed", "path", filePath, "error", err)
		}
	}

	// Apply edits sequentially
	currentContent := originalContent
	applied := 0

	for i, edit := range edits {
		if !strings.Contains(currentContent, edit.OldString) {
			// Rollback: restore original content
			if err := t.ops.WriteFile(ctx, filePath, []byte(originalContent), 0o644); err != nil {
				slog.Error("multiedit: rollback failed", "path", filePath, "error", err)
			}
			return "", applied, fmt.Errorf("edits[%d]: old_string not found in %s", i, filePath)
		}

		count := strings.Count(currentContent, edit.OldString)
		if !edit.ReplaceAll && count > 1 {
			// Rollback
			if err := t.ops.WriteFile(ctx, filePath, []byte(originalContent), 0o644); err != nil {
				slog.Error("multiedit: rollback failed", "path", filePath, "error", err)
			}
			return "", applied, fmt.Errorf("edits[%d]: old_string appears %d times (must be unique or use replace_all)", i, count)
		}

		if edit.ReplaceAll {
			currentContent = strings.ReplaceAll(currentContent, edit.OldString, edit.NewString)
		} else {
			currentContent = strings.Replace(currentContent, edit.OldString, edit.NewString, 1)
		}
		applied++
	}

	// Write the final content
	if err := t.ops.WriteFile(ctx, filePath, []byte(currentContent), 0o644); err != nil {
		// Rollback
		if rbErr := t.ops.WriteFile(ctx, filePath, []byte(originalContent), 0o644); rbErr != nil {
			slog.Error("multiedit: rollback failed", "path", filePath, "error", rbErr)
		}
		return "", 0, fmt.Errorf("write file: %w", err)
	}

	if t.readTracker != nil {
		t.readTracker.Invalidate(filePath)
	}

	return fmt.Sprintf("edited %s (%d edits applied)", filePath, applied), applied, nil
}
