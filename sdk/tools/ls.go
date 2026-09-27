package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// LsTool 列出目录内容，显示文件/目录名称、大小和修改时间。
type LsTool struct {
	pathPolicy   PathPolicy
	workspace    string // 工作目录，用于解析相对路径
	maxOutputLen int    // 最大输出长度，0 表示使用 DefaultMaxOutputLen
	ops          operations.FileOperations
}

type LsParams struct {
	Path    string `json:"path"`
	All     bool   `json:"all,omitempty"`     // 显示隐藏文件
	Recurse bool   `json:"recurse,omitempty"` // 递归列出
}

type lsEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// LsToolOption configures an LsTool during construction.
type LsToolOption func(*LsTool)

// WithLsPathPolicy explicitly controls access outside the workspace.
func WithLsPathPolicy(policy PathPolicy) LsToolOption {
	return func(t *LsTool) { t.pathPolicy = policy }
}

// WithLsWorkspace sets the workspace for path resolution.
func WithLsWorkspace(ws string) LsToolOption {
	return func(t *LsTool) { t.workspace = ws }
}

// WithLsMaxOutputLen sets the max output truncation length.
func WithLsMaxOutputLen(n int) LsToolOption {
	return func(t *LsTool) { t.maxOutputLen = n }
}

// WithLsOperations sets the FileOperations backend.
func WithLsOperations(ops operations.FileOperations) LsToolOption {
	return func(t *LsTool) { t.ops = ops }
}

func NewLsTool(opts ...LsToolOption) *LsTool {
	t := &LsTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *LsTool) Name() string { return "ls" }

func (t *LsTool) Description() string {
	return "List directory contents. Shows file/directory names, sizes, and modification times."
}

func (t *LsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Directory path to list."},
			"all":     map[string]any{"type": "boolean", "description": "Show hidden files."},
			"recurse": map[string]any{"type": "boolean", "description": "Recursively list subdirectories."},
		},
		"required": []string{"path"},
	}
}

func (t *LsTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params LsParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return json.Marshal(params)
}

func (t *LsTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params LsParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	dirPath := ResolvePath(t.workspace, params.Path)

	// Check path safety if workspace is set
	if !t.pathPolicy.Allows(t.workspace, dirPath) {
		return agent.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("path %s is outside workspace %s", params.Path, t.workspace),
		}, fmt.Errorf("path escapes workspace")
	}

	info, err := t.ops.Stat(ctx, dirPath)
	if err != nil {
		return agent.ToolResult{IsError: true}, err
	}
	if !info.IsDir {
		// 如果是文件，返回文件信息
		return agent.ToolResult{
			Content: formatFileEntry(dirPath, info),
		}, nil
	}

	var b strings.Builder
	b.WriteString(dirPath)
	b.WriteString(":\n")

	if params.Recurse {
		t.listRecursive(ctx, dirPath, params, &b, "")
	} else {
		t.listDirectory(ctx, dirPath, params, &b)
	}

	return agent.ToolResult{Content: TruncateOutput(b.String(), t.maxOutputLen)}, nil
}

func (t *LsTool) listDirectory(ctx context.Context, dirPath string, params LsParams, b *strings.Builder) {
	entries, err := t.ops.ReadDir(ctx, dirPath)
	if err != nil {
		b.WriteString(fmt.Sprintf("  error: %s\n", err))
		return
	}

	// 收集并排序
	var fileEntries []lsEntry
	for _, e := range entries {
		name := e.Name
		if !params.All && strings.HasPrefix(name, ".") {
			continue
		}
		fileEntries = append(fileEntries, lsEntry{
			Name:    name,
			IsDir:   e.IsDir,
			Size:    e.Size,
			ModTime: e.ModTime,
		})
	}

	sort.Slice(fileEntries, func(i, j int) bool {
		// 目录在前
		if fileEntries[i].IsDir != fileEntries[j].IsDir {
			return fileEntries[i].IsDir
		}
		return fileEntries[i].Name < fileEntries[j].Name
	})

	for _, e := range fileEntries {
		prefix := "  "
		if e.IsDir {
			b.WriteString(fmt.Sprintf("%s%s/  (%s)\n", prefix, e.Name, e.ModTime.Format("2006-01-02 15:04")))
		} else {
			b.WriteString(fmt.Sprintf("%s%s  %s  (%s)\n", prefix, e.Name, formatSize(e.Size), e.ModTime.Format("2006-01-02 15:04")))
		}
	}
}

func (t *LsTool) listRecursive(ctx context.Context, dirPath string, params LsParams, b *strings.Builder, prefix string) {
	entries, err := t.ops.ReadDir(ctx, dirPath)
	if err != nil {
		b.WriteString(fmt.Sprintf("%s  error: %s\n", prefix, err))
		return
	}

	var fileEntries []lsEntry
	for _, e := range entries {
		name := e.Name
		if !params.All && strings.HasPrefix(name, ".") {
			continue
		}
		fileEntries = append(fileEntries, lsEntry{
			Name:    name,
			IsDir:   e.IsDir,
			Size:    e.Size,
			ModTime: e.ModTime,
		})
	}

	sort.Slice(fileEntries, func(i, j int) bool {
		if fileEntries[i].IsDir != fileEntries[j].IsDir {
			return fileEntries[i].IsDir
		}
		return fileEntries[i].Name < fileEntries[j].Name
	})

	for _, e := range fileEntries {
		displayPath := prefix + "/" + e.Name
		if prefix == "" {
			displayPath = e.Name
		}
		if e.IsDir {
			b.WriteString(fmt.Sprintf("  %s/  (%s)\n", displayPath, e.ModTime.Format("2006-01-02 15:04")))
			// Build full path for recursion
			fullPath := dirPath + "/" + e.Name
			t.listRecursive(ctx, fullPath, params, b, displayPath)
		} else {
			b.WriteString(fmt.Sprintf("  %s  %s  (%s)\n", displayPath, formatSize(e.Size), e.ModTime.Format("2006-01-02 15:04")))
		}
	}
}

func formatSize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case size >= GB:
		return fmt.Sprintf("%.1fG", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.1fM", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.1fK", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%dB", size)
	}
}

func formatFileEntry(path string, info operations.FileInfo) string {
	if info.IsDir {
		return fmt.Sprintf("%s/  (directory, %s)", path, info.ModTime.Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("%s  %s  (%s)", path, formatSize(info.Size), info.ModTime.Format("2006-01-02 15:04"))
}

// IsConcurrencySafe implements agent.ConcurrencySafeChecker.
// LsTool is always safe to execute concurrently.
func (t *LsTool) IsConcurrencySafe(params json.RawMessage) bool {
	return true
}
