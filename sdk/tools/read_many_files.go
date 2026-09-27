package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// ReadManyFilesTool reads content from multiple files, concatenating results.
// It supports glob patterns, include/exclude filters, and default exclusion of
// common directories like node_modules, .git, etc.
type ReadManyFilesTool struct {
	pathPolicy   PathPolicy
	workspace    string
	maxOutputLen int
	ops          operations.FileOperations
}

type ReadManyFilesParams struct {
	Paths               []string `json:"paths"`
	Include             []string `json:"include,omitempty"`
	Exclude             []string `json:"exclude,omitempty"`
	Recursive           *bool    `json:"recursive,omitempty"`
	UseDefaultExcludes  *bool    `json:"useDefaultExcludes,omitempty"`
	AllowLocalExecution bool     `json:"allowLocalExecution,omitempty"`
}

type ReadManyFilesOption func(*ReadManyFilesTool)

// WithReadManyFilesPathPolicy explicitly controls access outside the workspace.
func WithReadManyFilesPathPolicy(policy PathPolicy) ReadManyFilesOption {
	return func(t *ReadManyFilesTool) { t.pathPolicy = policy }
}

func WithReadManyFilesWorkspace(ws string) ReadManyFilesOption {
	return func(t *ReadManyFilesTool) { t.workspace = ws }
}

func WithReadManyFilesMaxOutputLen(n int) ReadManyFilesOption {
	return func(t *ReadManyFilesTool) { t.maxOutputLen = n }
}

func WithReadManyFilesOperations(ops operations.FileOperations) ReadManyFilesOption {
	return func(t *ReadManyFilesTool) { t.ops = ops }
}

func NewReadManyFilesTool(opts ...ReadManyFilesOption) *ReadManyFilesTool {
	t := &ReadManyFilesTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *ReadManyFilesTool) Name() string { return "read_many_files" }

func (t *ReadManyFilesTool) Description() string {
	return `Reads multiple text files, with glob and exclusion filters. Relative paths resolve against the workspace. Outside-workspace access requires the operator-configured path policy; tool arguments cannot grant it.`
}

func (t *ReadManyFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Required. An array of glob patterns or paths relative to the tool's target directory. Examples: ['src/**/*.ts'], ['README.md', 'docs/']",
			},
			"include": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional. Additional glob patterns to include.",
			},
			"exclude": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional. Glob patterns for files/directories to exclude.",
			},
			"useDefaultExcludes": map[string]any{
				"type":        "boolean",
				"description": "Optional. Whether to apply default exclusion patterns (e.g. node_modules, .git). Defaults to true.",
			},
		},
		"required": []string{"paths"},
	}
}

func (t *ReadManyFilesTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params ReadManyFilesParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if len(params.Paths) == 0 {
		return nil, fmt.Errorf("at least one path is required")
	}
	return json.Marshal(params)
}

// IsConcurrencySafe implements agent.ConcurrencySafeChecker.
func (t *ReadManyFilesTool) IsConcurrencySafe(_ json.RawMessage) bool { return true }

// defaultExcludes returns the default exclusion patterns.
func defaultExcludes() []string {
	return []string{
		"node_modules", ".git", ".vscode", ".idea",
		"dist", "build", "coverage", "__pycache__",
		".DS_Store", "vendor", "target", ".next", ".nuxt",
	}
}

// isBinaryFile does a simple heuristic check: if the file contains null bytes, it's binary.
func isBinaryFile(data []byte) bool {
	// Check first 8KB for null bytes
	limit := len(data)
	if limit > 8192 {
		limit = 8192
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

const maxSingleFileSize = 100 * 1024   // 100KB per file
const maxTotalContentSize = 500 * 1024 // 500KB total
const maxFilesCount = 50

func (t *ReadManyFilesTool) Execute(ctx context.Context, raw json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var params ReadManyFilesParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	// Merge all search patterns
	searchPatterns := append([]string{}, params.Paths...)
	searchPatterns = append(searchPatterns, params.Include...)

	// Build exclude set
	useDefaults := true
	if params.UseDefaultExcludes != nil {
		useDefaults = *params.UseDefaultExcludes
	}
	excludeSet := make(map[string]bool)
	if useDefaults {
		for _, e := range defaultExcludes() {
			excludeSet[e] = true
		}
	}
	for _, e := range params.Exclude {
		excludeSet[e] = true
	}

	// Resolve files from patterns
	var filePaths []string
	baseDir := t.workspace
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}

	for _, pattern := range searchPatterns {
		absPattern := pattern
		if !filepath.IsAbs(pattern) {
			absPattern = filepath.Join(baseDir, pattern)
		}

		// Check if it's a direct file path (no glob chars)
		if !strings.ContainsAny(pattern, "*?[") {
			// Direct path
			if _, err := os.Stat(absPattern); err == nil {
				filePaths = append(filePaths, absPattern)
			}
			continue
		}

		// Glob pattern — find the base directory and the glob part
		matches, err := filepath.Glob(absPattern)
		if err != nil {
			slog.Warn("read_many_files: invalid glob pattern", "pattern", pattern, "error", err)
			continue
		}

		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				continue
			}
			if info.IsDir() {
				continue
			}
			filePaths = append(filePaths, m)
		}
	}

	// Sort and deduplicate
	sort.Strings(filePaths)
	filePaths = uniquePaths(filePaths)

	// Filter: path safety, excludes, binary
	var filteredPaths []string
	for _, fp := range filePaths {
		// Path safety check
		if !t.pathPolicy.Allows(t.workspace, fp) {
			continue
		}

		// Check against exclude patterns
		if isExcludedByPatterns(fp, excludeSet) {
			continue
		}

		filteredPaths = append(filteredPaths, fp)
	}

	if len(filteredPaths) == 0 {
		return agent.ToolResult{Content: "No matching files found."}, nil
	}

	// Read files and build output
	var b strings.Builder
	totalSize := 0
	filesRead := 0
	filesSkipped := 0

	for _, fp := range filteredPaths {
		if filesRead >= maxFilesCount {
			filesSkipped = len(filteredPaths) - filesRead
			break
		}

		data, err := os.ReadFile(fp)
		if err != nil {
			slog.Warn("read_many_files: cannot read", "path", fp, "error", err)
			continue
		}

		// Skip binary files
		if isBinaryFile(data) {
			continue
		}

		content := string(data)
		isTruncated := false

		// Per-file size limit
		if len(content) > maxSingleFileSize {
			content = content[:maxSingleFileSize]
			isTruncated = true
		}

		// Total size limit
		if totalSize+len(content) > maxTotalContentSize {
			filesSkipped = len(filteredPaths) - filesRead
			break
		}

		separator := fmt.Sprintf("--- %s ---", fp)
		b.WriteString(separator)
		b.WriteString("\n\n")
		if isTruncated {
			b.WriteString("<system-reminder>\nTRUNCATED FILE - Use read_file for complete content.\n</system-reminder>\n\n")
		}
		b.WriteString(content)
		b.WriteString("\n\n")

		totalSize += len(content)
		filesRead++
	}

	if filesSkipped > 0 {
		b.WriteString(fmt.Sprintf("\n... %d more files skipped (file count or size limit reached)\n", filesSkipped))
	}

	content := b.String()
	content = TruncateOutput(content, t.maxOutputLen)

	return agent.ToolResult{
		Content: fmt.Sprintf("Read %d files (%s):\n\n%s", filesRead, FormatByteCount(totalSize), content),
	}, nil
}

// uniquePaths removes duplicate paths from a sorted slice.
func uniquePaths(paths []string) []string {
	if len(paths) == 0 {
		return paths
	}
	result := []string{paths[0]}
	for _, p := range paths[1:] {
		if p != result[len(result)-1] {
			result = append(result, p)
		}
	}
	return result
}

// isExcludedByPatterns checks if a file path matches any exclusion pattern.
func isExcludedByPatterns(fp string, excludeSet map[string]bool) bool {
	// Check each path component against the exclude set
	parts := strings.Split(filepath.ToSlash(fp), "/")
	for _, part := range parts {
		if excludeSet[part] {
			return true
		}
	}
	return false
}
