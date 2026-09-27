package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// defaultSkipDirs are directories that are always skipped during search.
var defaultSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".svn":         true,
	".hg":          true,
}

// FindTool searches for files and directories matching name patterns.
type FindTool struct {
	pathPolicy PathPolicy
	workspace  string // 工作目录，用于解析相对路径
	ops        operations.FileOperations
}

type FindParams struct {
	Path       string `json:"path"`
	Pattern    string `json:"pattern,omitempty"`
	MaxDepth   int    `json:"max_depth,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
	Type       string `json:"type,omitempty"` // "file" | "dir" | "" (both)
}

// FindToolOption configures a FindTool during construction.
type FindToolOption func(*FindTool)

// WithFindPathPolicy explicitly controls access outside the workspace.
func WithFindPathPolicy(policy PathPolicy) FindToolOption {
	return func(t *FindTool) { t.pathPolicy = policy }
}

// WithFindWorkspace sets the workspace for path resolution.
func WithFindWorkspace(ws string) FindToolOption {
	return func(t *FindTool) { t.workspace = ws }
}

// WithFindOperations sets the FileOperations backend.
func WithFindOperations(ops operations.FileOperations) FindToolOption {
	return func(t *FindTool) { t.ops = ops }
}

func NewFindTool(opts ...FindToolOption) *FindTool {
	t := &FindTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *FindTool) Name() string { return "find" }

func (t *FindTool) Description() string {
	return "Find files and directories by name pattern. Supports glob patterns and recursive search. Skips .git, node_modules, vendor, __pycache__ by default."
}

func (t *FindTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Directory to search in."},
			"pattern":     map[string]any{"type": "string", "description": "Glob pattern for file names (e.g. *.go, test_*.ts)."},
			"max_depth":   map[string]any{"type": "integer", "description": "Maximum search depth (default unlimited)."},
			"max_results": map[string]any{"type": "integer", "description": "Maximum number of results (default 50)."},
			"type":        map[string]any{"type": "string", "description": "Filter by type: 'file', 'dir', or empty for both."},
		},
		"required": []string{"path"},
	}
}

func (t *FindTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params FindParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if params.MaxResults <= 0 {
		params.MaxResults = 50
	}
	if params.Type != "" && params.Type != "file" && params.Type != "dir" {
		return nil, fmt.Errorf("type must be 'file', 'dir', or empty")
	}
	return json.Marshal(params)
}

func (t *FindTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params FindParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	searchPath := ResolvePath(t.workspace, params.Path)

	// Check path safety if workspace is set
	if !t.pathPolicy.Allows(t.workspace, searchPath) {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("path %s is outside workspace %s", params.Path, t.workspace),
		}, fmt.Errorf("path escapes workspace")
	}

	info, err := t.ops.Stat(ctx, searchPath)
	if err != nil {
		return agent.ToolResult{IsError: true}, err
	}
	if !info.IsDir {
		return agent.ToolResult{IsError: true}, fmt.Errorf("%s is not a directory", searchPath)
	}

	var results []string
	rootDepth := strings.Count(searchPath, string(filepath.Separator))

	err = t.ops.Walk(ctx, searchPath, func(path string, entry operations.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip default directories
		if entry.IsDir && defaultSkipDirs[entry.Name] {
			return filepath.SkipDir
		}

		// Skip hidden directories
		if entry.IsDir && strings.HasPrefix(entry.Name, ".") {
			return filepath.SkipDir
		}

		// Depth control
		if params.MaxDepth > 0 {
			currentDepth := strings.Count(path, string(filepath.Separator)) - rootDepth
			if currentDepth > params.MaxDepth {
				if entry.IsDir {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Type filter
		if params.Type == "file" && entry.IsDir {
			return nil
		}
		if params.Type == "dir" && !entry.IsDir {
			return nil
		}

		// Pattern matching
		if params.Pattern != "" {
			matched, _ := filepath.Match(params.Pattern, entry.Name)
			if !matched {
				return nil
			}
		}

		results = append(results, path)
		if len(results) >= params.MaxResults {
			return fmt.Errorf("max results reached")
		}
		return nil
	})

	if err != nil && len(results) < params.MaxResults {
		// Walk returned a real error (not max results)
		return agent.ToolResult{IsError: true}, err
	}

	if len(results) == 0 {
		return agent.ToolResult{Content: "no files found"}, nil
	}

	var b strings.Builder
	for _, r := range results {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("\n%d results found.", len(results)))

	return agent.ToolResult{Content: b.String()}, nil
}

// IsConcurrencySafe implements agent.ConcurrencySafeChecker.
// FindTool is always safe to execute concurrently.
func (t *FindTool) IsConcurrencySafe(params json.RawMessage) bool {
	return true
}
