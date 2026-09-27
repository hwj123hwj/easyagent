package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// PatchTool applies standard unified diff patches to modify multiple files.
// Supports adding, updating, and deleting files with context-aware changes.
type PatchTool struct {
	pathPolicy PathPolicy
	workspace  string
	ops        operations.FileOperations
	backupMgr  *BackupManager
}

type PatchParams struct {
	PatchText string `json:"patchText"`
}

type PatchOption func(*PatchTool)

// WithPatchPathPolicy explicitly controls access outside the workspace.
func WithPatchPathPolicy(policy PathPolicy) PatchOption {
	return func(t *PatchTool) { t.pathPolicy = policy }
}

func WithPatchWorkspace(ws string) PatchOption {
	return func(t *PatchTool) { t.workspace = ws }
}

func WithPatchOperations(ops operations.FileOperations) PatchOption {
	return func(t *PatchTool) { t.ops = ops }
}

func WithPatchBackupManager(bm *BackupManager) PatchOption {
	return func(t *PatchTool) { t.backupMgr = bm }
}

func NewPatchTool(opts ...PatchOption) *PatchTool {
	t := &PatchTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *PatchTool) Name() string { return "patch" }

func (t *PatchTool) Description() string {
	return "Apply a patch to modify multiple files. Supports adding, updating, and deleting files with context-aware changes."
}

func (t *PatchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patchText": map[string]any{
				"type":        "string",
				"description": "The full patch text that describes all changes to be made. Must use the standard unified diff format.",
			},
		},
		"required": []string{"patchText"},
	}
}

func (t *PatchTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params PatchParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.PatchText) == "" {
		return nil, fmt.Errorf("patchText is required")
	}
	return json.Marshal(params)
}

// RequiresConfirmation implements agent.ToolWithConfirmation.
func (t *PatchTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params PatchParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "即将应用 patch（参数解析失败，仍需确认）", true
	}

	// Parse to get affected files
	hunks := parseUnifiedDiff(params.PatchText)
	affectedFiles := make([]string, 0, len(hunks))
	seen := make(map[string]bool)
	for _, h := range hunks {
		if !seen[h.Path] {
			affectedFiles = append(affectedFiles, h.Path)
			seen[h.Path] = true
		}
	}

	desc := fmt.Sprintf("即将应用 patch（%d 个文件）", len(affectedFiles))
	for _, f := range affectedFiles {
		desc += "\n  " + f
	}
	return desc, true
}

func (t *PatchTool) Execute(ctx context.Context, raw json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var params PatchParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	hunks := parseUnifiedDiff(params.PatchText)
	if len(hunks) == 0 {
		return agent.ToolResult{IsError: true, Content: "No valid hunks found in patch text"}, fmt.Errorf("no hunks in patch")
	}

	var added, modified, deleted []string

	for _, hunk := range hunks {
		filePath := hunk.Path
		if !filepath.IsAbs(filePath) {
			if t.workspace != "" {
				filePath = filepath.Join(t.workspace, filePath)
			} else {
				cwd, _ := os.Getwd()
				filePath = filepath.Join(cwd, filePath)
			}
		}
		filePath = filepath.Clean(filePath)

		// Path safety
		if !t.pathPolicy.Allows(t.workspace, filePath) {
			return agent.ToolResult{IsError: true, Content: fmt.Sprintf("access denied: %s is outside workspace", filePath)}, fmt.Errorf("path escapes workspace")
		}

		// Backup
		if t.backupMgr != nil {
			if _, err := t.backupMgr.Snapshot(filePath); err != nil {
				slog.Warn("patch: backup snapshot failed", "path", filePath, "error", err)
			}
		}

		switch hunk.Type {
		case "add":
			dir := filepath.Dir(filePath)
			if err := t.ops.MkdirAll(ctx, dir, 0o755); err != nil {
				return agent.ToolResult{IsError: true, Content: fmt.Sprintf("create dir for %s: %v", filePath, err)}, err
			}
			if err := t.ops.WriteFile(ctx, filePath, []byte(hunk.Content), 0o644); err != nil {
				return agent.ToolResult{IsError: true, Content: fmt.Sprintf("write %s: %v", filePath, err)}, err
			}
			added = append(added, filePath)

		case "delete":
			if err := os.Remove(filePath); err != nil {
				return agent.ToolResult{IsError: true, Content: fmt.Sprintf("delete %s: %v", filePath, err)}, err
			}
			deleted = append(deleted, filePath)

		case "update":
			data, err := t.ops.ReadFile(ctx, filePath)
			if err != nil {
				return agent.ToolResult{IsError: true, Content: fmt.Sprintf("read %s: %v", filePath, err)}, err
			}
			newContent := applyHunksToContent(string(data), hunk.Chunks)
			if err := t.ops.WriteFile(ctx, filePath, []byte(newContent), 0o644); err != nil {
				return agent.ToolResult{IsError: true, Content: fmt.Sprintf("write %s: %v", filePath, err)}, err
			}
			modified = append(modified, filePath)
		}
	}

	var summary []string
	if len(added) > 0 {
		summary = append(summary, fmt.Sprintf("Added: %s", strings.Join(added, ", ")))
	}
	if len(modified) > 0 {
		summary = append(summary, fmt.Sprintf("Modified: %s", strings.Join(modified, ", ")))
	}
	if len(deleted) > 0 {
		summary = append(summary, fmt.Sprintf("Deleted: %s", strings.Join(deleted, ", ")))
	}

	return agent.ToolResult{
		Content: fmt.Sprintf("Patch applied successfully.\n%s", strings.Join(summary, "\n")),
	}, nil
}

// patchHunk represents a parsed section of a unified diff.
type patchHunk struct {
	Path    string      // target file path
	Type    string      // "add", "delete", "update"
	Content string      // for "add": full file content
	Chunks  []diffChunk // for "update": list of diff chunks
}

// diffChunk represents a single change hunk within a file diff.
type diffChunk struct {
	OldStart int
	OldCount int
	Lines    []diffLine
}

type diffLine struct {
	Type    byte // ' ' context, '-' removed, '+' added
	Content string
}

// parseUnifiedDiff parses a simplified unified diff format.
// Supports: --- a/file, +++ b/file, @@ -old,count +new,count @@, and +/-/space lines.
func parseUnifiedDiff(patchText string) []patchHunk {
	scanner := bufio.NewScanner(strings.NewReader(patchText))
	var hunks []patchHunk

	var currentFile string
	var currentType string
	var currentChunks []diffChunk
	var currentChunk *diffChunk
	var addContent strings.Builder
	var isNewFile bool
	var isDeletedFile bool

	for scanner.Scan() {
		line := scanner.Text()

		// Detect file headers
		if strings.HasPrefix(line, "--- ") {
			// Start of a new file diff. Save previous if exists.
			if currentFile != "" && currentType == "update" && len(currentChunks) > 0 {
				hunks = append(hunks, patchHunk{
					Path:   currentFile,
					Type:   "update",
					Chunks: currentChunks,
				})
			} else if currentFile != "" && isNewFile {
				hunks = append(hunks, patchHunk{
					Path:    currentFile,
					Type:    "add",
					Content: addContent.String(),
				})
			} else if currentFile != "" && isDeletedFile {
				hunks = append(hunks, patchHunk{
					Path: currentFile,
					Type: "delete",
				})
			}

			currentFile = ""
			currentChunks = nil
			currentChunk = nil
			addContent.Reset()
			isNewFile = false
			isDeletedFile = false

			// Parse --- line to detect deleted file
			rest := strings.TrimPrefix(line, "--- ")
			rest = strings.TrimSpace(rest)
			if rest == "/dev/null" {
				isNewFile = true // +++ will tell us the new file
			}
			continue
		}

		if strings.HasPrefix(line, "+++ ") {
			rest := strings.TrimPrefix(line, "+++ ")
			rest = strings.TrimSpace(rest)
			if rest == "/dev/null" {
				isDeletedFile = true
				continue
			}
			// Extract file path: strip a/ or b/ prefix
			currentFile = rest
			currentFile = strings.TrimPrefix(currentFile, "a/")
			currentFile = strings.TrimPrefix(currentFile, "b/")
			currentType = "update"

			if isNewFile {
				currentType = "add"
			}
			continue
		}

		if currentFile == "" {
			continue
		}

		// Parse @@ hunk headers
		if strings.HasPrefix(line, "@@") {
			// Flush previous chunk
			if currentChunk != nil {
				currentChunks = append(currentChunks, *currentChunk)
			}
			currentChunk = &diffChunk{}
			// Parse @@ -old,count +new,count @@
			// We'll just track the lines; detailed parsing not strictly needed for application
			continue
		}

		if currentType == "add" {
			// For new files, collect all content (strip leading +/- markers)
			if strings.HasPrefix(line, "+") {
				addContent.WriteString(line[1:])
				addContent.WriteString("\n")
			} else if strings.HasPrefix(line, " ") {
				addContent.WriteString(line[1:])
				addContent.WriteString("\n")
			}
			continue
		}

		// For update hunks, collect diff lines
		if currentChunk != nil {
			if len(line) > 0 {
				switch line[0] {
				case '+':
					currentChunk.Lines = append(currentChunk.Lines, diffLine{Type: '+', Content: line[1:]})
				case '-':
					currentChunk.Lines = append(currentChunk.Lines, diffLine{Type: '-', Content: line[1:]})
				case ' ':
					currentChunk.Lines = append(currentChunk.Lines, diffLine{Type: ' ', Content: line[1:]})
				}
			}
		}
	}

	// Flush last file
	if currentFile != "" {
		if currentChunk != nil {
			currentChunks = append(currentChunks, *currentChunk)
		}
		if currentType == "update" && len(currentChunks) > 0 {
			hunks = append(hunks, patchHunk{
				Path:   currentFile,
				Type:   "update",
				Chunks: currentChunks,
			})
		} else if isNewFile {
			hunks = append(hunks, patchHunk{
				Path:    currentFile,
				Type:    "add",
				Content: addContent.String(),
			})
		} else if isDeletedFile {
			hunks = append(hunks, patchHunk{
				Path: currentFile,
				Type: "delete",
			})
		}
	}

	return hunks
}

// applyHunksToContent applies diff chunks to the original file content.
func applyHunksToContent(original string, chunks []diffChunk) string {
	lines := strings.Split(original, "\n")
	result := make([]string, 0, len(lines))

	for _, chunk := range chunks {
		// Find the matching region in the original file using context lines
		matchStart := findChunkLocation(lines, chunk)
		if matchStart < 0 {
			// Couldn't find exact match; fall back to applying at end
			matchStart = len(lines)
		}

		// Add lines before the chunk
		result = append(result, lines[:matchStart]...)

		// Apply the chunk
		for _, dl := range chunk.Lines {
			switch dl.Type {
			case ' ':
				// Context line — keep it
				result = append(result, dl.Content)
			case '-':
				// Removed line — skip it (it should already be at matchStart)
			case '+':
				// Added line
				result = append(result, dl.Content)
			}
		}

		// Skip the old lines in the original
		oldCount := 0
		for _, dl := range chunk.Lines {
			if dl.Type == '-' || dl.Type == ' ' {
				oldCount++
			}
		}
		newStart := matchStart + oldCount
		if newStart < len(lines) {
			lines = lines[newStart:]
		} else {
			lines = nil
		}
	}

	// Append remaining lines
	result = append(result, lines...)

	return strings.Join(result, "\n")
}

// findChunkLocation finds where a chunk's context matches in the file lines.
func findChunkLocation(lines []string, chunk diffChunk) int {
	// Build expected context (the non-added, non-removed lines)
	var contextBefore []string
	for _, dl := range chunk.Lines {
		if dl.Type == ' ' {
			contextBefore = append(contextBefore, dl.Content)
		} else {
			break
		}
	}

	if len(contextBefore) == 0 {
		// No context — use first removed line as anchor
		for _, dl := range chunk.Lines {
			if dl.Type == '-' {
				contextBefore = []string{dl.Content}
				break
			}
		}
	}

	if len(contextBefore) == 0 {
		return 0
	}

	// Search for the context pattern
	for i := 0; i <= len(lines)-len(contextBefore); i++ {
		match := true
		for j, ctx := range contextBefore {
			if lines[i+j] != ctx {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}

	return -1
}
