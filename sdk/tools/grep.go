package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
)

// GrepTool 在文件中搜索正则表达式模式。
// 支持递归目录搜索，返回匹配的文件路径、行号和行内容。
type GrepTool struct {
	pathPolicy   PathPolicy
	workspace    string // 工作目录，用于解析相对路径
	maxOutputLen int    // 最大输出长度，0 表示使用 DefaultMaxOutputLen
	ops          operations.FileOperations
}

type GrepParams struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Include     string `json:"include,omitempty"`      // glob 模式过滤文件名（如 "*.go"）
	IgnoreCase  bool   `json:"ignore_case,omitempty"`  // 忽略大小写
	MaxResults  int    `json:"max_results,omitempty"`  // 最大结果数
	ShowContext int    `json:"show_context,omitempty"` // 显示匹配行上下文行数
}

type grepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// GrepToolOption configures a GrepTool during construction.
type GrepToolOption func(*GrepTool)

// WithGrepPathPolicy explicitly controls access outside the workspace.
func WithGrepPathPolicy(policy PathPolicy) GrepToolOption {
	return func(t *GrepTool) { t.pathPolicy = policy }
}

// WithGrepWorkspace sets the workspace for path resolution.
func WithGrepWorkspace(ws string) GrepToolOption {
	return func(t *GrepTool) { t.workspace = ws }
}

// WithGrepMaxOutputLen sets the max output truncation length.
func WithGrepMaxOutputLen(n int) GrepToolOption {
	return func(t *GrepTool) { t.maxOutputLen = n }
}

// WithGrepOperations sets the FileOperations backend.
func WithGrepOperations(ops operations.FileOperations) GrepToolOption {
	return func(t *GrepTool) { t.ops = ops }
}

func NewGrepTool(opts ...GrepToolOption) *GrepTool {
	t := &GrepTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.ops == nil {
		t.ops = operations.LocalFileOperations{}
	}
	return t
}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return "Search file contents using regex patterns. Supports recursive directory search and file glob filtering."
}

func (t *GrepTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":      map[string]any{"type": "string", "description": "The regex pattern to search for."},
			"path":         map[string]any{"type": "string", "description": "File or directory path to search in."},
			"include":      map[string]any{"type": "string", "description": "Glob pattern to filter files (e.g. *.go, *.ts)."},
			"ignore_case":  map[string]any{"type": "boolean", "description": "Case-insensitive search."},
			"max_results":  map[string]any{"type": "integer", "description": "Maximum number of results (default 100)."},
			"show_context": map[string]any{"type": "integer", "description": "Number of context lines around matches."},
		},
		"required": []string{"pattern", "path"},
	}
}

func (t *GrepTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params GrepParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	// 验证 pattern 是合法正则
	flags := ""
	if params.IgnoreCase {
		flags = "(?i)"
	}
	if _, err := regexp.Compile(flags + params.Pattern); err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	if params.MaxResults <= 0 {
		params.MaxResults = 100
	}
	return json.Marshal(params)
}

func (t *GrepTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var params GrepParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	flags := ""
	if params.IgnoreCase {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + params.Pattern)
	if err != nil {
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

	// 判断是文件还是目录
	info, err := t.ops.Stat(ctx, searchPath)
	if err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	var matches []grepMatch

	if info.IsDir {
		matches = t.searchDir(ctx, re, searchPath, params, params.MaxResults)
	} else {
		matches = t.searchFile(ctx, re, searchPath, params.MaxResults)
	}

	if len(matches) == 0 {
		return agent.ToolResult{Content: "no matches found"}, nil
	}

	// 格式化输出
	var b strings.Builder
	if params.ShowContext > 0 {
		// 按文件分组，输出上下文行
		grouped := groupMatchesByFile(matches)
		for _, fileGroup := range grouped {
			lines, err := t.opsReadLines(ctx, fileGroup.file)
			if err != nil {
				for _, m := range fileGroup.matches {
					b.WriteString(fmt.Sprintf("%s:%d: %s\n", m.File, m.Line, m.Content))
				}
				continue
			}
			showCtx := params.ShowContext
			for _, m := range fileGroup.matches {
				start := m.Line - showCtx
				if start < 1 {
					start = 1
				}
				end := m.Line + showCtx
				if end > len(lines) {
					end = len(lines)
				}
				for i := start; i <= end; i++ {
					prefix := "  "
					if i == m.Line {
						prefix = "> "
					}
					b.WriteString(fmt.Sprintf("%s%s:%d: %s\n", prefix, m.File, i, strings.TrimSpace(lines[i-1])))
				}
				b.WriteString("\n")
			}
		}
	} else {
		for _, m := range matches {
			b.WriteString(fmt.Sprintf("%s:%d: %s\n", m.File, m.Line, m.Content))
		}
	}
	b.WriteString(fmt.Sprintf("\n%d matches found.", len(matches)))

	return agent.ToolResult{Content: TruncateOutput(b.String(), t.maxOutputLen)}, nil
}

func (t *GrepTool) searchDir(ctx context.Context, re *regexp.Regexp, root string, params GrepParams, maxResults int) []grepMatch {
	var matches []grepMatch

	// 构建文件名过滤 pattern
	var includeRe *regexp.Regexp
	if params.Include != "" {
		// 将 glob 转为简单的正则
		globPattern := globToRegex(params.Include)
		includeRe = regexp.MustCompile(globPattern)
	}

	t.ops.Walk(ctx, root, func(path string, entry operations.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 跳过隐藏目录和常见的非搜索目录
		if entry.IsDir {
			name := entry.Name
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// 文件名过滤
		if includeRe != nil && !includeRe.MatchString(entry.Name) {
			return nil
		}

		fileMatches := t.searchFile(ctx, re, path, maxResults-len(matches))
		matches = append(matches, fileMatches...)

		if len(matches) >= maxResults {
			return fmt.Errorf("max results reached")
		}
		return nil
	})

	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}
	return matches
}

func (t *GrepTool) searchFile(ctx context.Context, re *regexp.Regexp, path string, maxResults int) []grepMatch {
	if !t.pathPolicy.Allows(t.workspace, path) {
		return nil
	}
	data, err := t.ops.ReadFile(ctx, path)
	if err != nil {
		return nil
	}

	var matches []grepMatch
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if re.MatchString(line) {
			// 截断过长的行
			content := line
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			matches = append(matches, grepMatch{
				File:    path,
				Line:    lineNum,
				Content: strings.TrimSpace(content),
			})
			if len(matches) >= maxResults {
				break
			}
		}

		select {
		case <-ctx.Done():
			return matches
		default:
		}
	}

	return matches
}

// opsReadLines reads a file through operations and splits into lines.
func (t *GrepTool) opsReadLines(ctx context.Context, path string) ([]string, error) {
	data, err := t.ops.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

// globToRegex 将简单的 glob 模式转换为正则表达式。
// 支持 * (匹配非路径分隔符) 和 ? (单字符)。
func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	for _, ch := range glob {
		switch ch {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.', '(', ')', '+', '|', '^', '$', '@', '%', '[', ']', '{', '}':
			b.WriteRune('\\')
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}

type fileMatchGroup struct {
	file    string
	matches []grepMatch
}

func groupMatchesByFile(matches []grepMatch) []fileMatchGroup {
	var groups []fileMatchGroup
	groupMap := make(map[string]int)
	for _, m := range matches {
		idx, ok := groupMap[m.File]
		if !ok {
			idx = len(groups)
			groups = append(groups, fileMatchGroup{file: m.File})
			groupMap[m.File] = idx
		}
		groups[idx].matches = append(groups[idx].matches, m)
	}
	return groups
}

// IsConcurrencySafe implements agent.ConcurrencySafeChecker.
// GrepTool is always safe to execute concurrently.
func (t *GrepTool) IsConcurrencySafe(params json.RawMessage) bool {
	return true
}
