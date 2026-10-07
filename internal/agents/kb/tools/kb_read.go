package kbtools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
)

// ReadTool reads a file from the knowledge base repository.
type ReadTool struct {
	repoPath string
}

func NewReadTool(repoPath string) *ReadTool {
	return &ReadTool{repoPath: repoPath}
}

func (t *ReadTool) Name() string { return "kb_read" }
func (t *ReadTool) Description() string {
	return `读取知识库中的文件。支持两种模式：

1. 完整模式（默认）：返回 Markdown 原文。
2. 概览模式（overview=true）：只返回文档结构（标题层级 + 每段首句 + 字数统计），
   约为完整内容的 10-20% 大小。适合先了解文档是否值得精读。

路径可以是绝对路径或相对于知识库根目录的相对路径。
工作流建议：先 kb_search → kb_read overview=true → kb_read 完整模式（确认值得精读后）。`
}
func (t *ReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "文件路径（绝对路径或相对于知识库根目录的相对路径）",
			},
			"overview": map[string]any{
				"type":        "boolean",
				"description": "概览模式：只返回文档结构（标题+首句+统计），不返回全文。节省 token。",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "从第几行开始读（从0开始，默认0，仅完整模式有效）",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "读取行数（默认200，仅完整模式有效）",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if p.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return params, nil
}

func (t *ReadTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		Path     string  `json:"path"`
		Overview bool    `json:"overview"`
		Offset   float64 `json:"offset"`
		Limit    float64 `json:"limit"`
	}
	_ = json.Unmarshal(params, &p)

	// Resolve path (relative → absolute)
	path := resolvePath(t.repoPath, p.Path)

	data, err := os.ReadFile(path)
	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("无法读取文件: %v\n\n提示：请确认路径正确。路径可以是相对路径（如 issues/xxx.md）或绝对路径。", err),
			IsError: true,
		}, nil
	}

	lines := strings.Split(string(data), "\n")

	// ── Overview mode: return structured synopsis (L1 tier) ──
	// Inspired by OpenViking's tool_result_synopsis: deliver structure
	// without full content, letting the LLM decide if a deep read is worth it.
	if p.Overview {
		synopsis := generateOverview(p.Path, lines)
		return agent.ToolResult{
			Content:    synopsis,
			UserFacing: synopsis,
		}, nil
	}

	// ── Full read mode ──
	offset := int(p.Offset)
	limit := 200
	if p.Limit > 0 {
		limit = int(p.Limit)
	}

	if offset >= len(lines) {
		return agent.ToolResult{
			Content: fmt.Sprintf("文件只有 %d 行，无法从第 %d 行开始读", len(lines), offset),
			IsError: true,
		}, nil
	}

	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}

	selectedLines := lines[offset:end]
	content := strings.Join(selectedLines, "\n")

	// Truncate if too long (keep under 8K chars for LLM context)
	if len(content) > 8000 {
		// Rune-safe truncation: don't split multi-byte UTF-8
		runes := []rune(content)
		if len(runes) > 8000 {
			content = string(runes[:8000])
		}
		content += "\n\n... (内容过长已截断，使用 offset 参数继续读取)"
	}

	header := fmt.Sprintf("📄 %s (%d行)", path, len(lines))
	if offset > 0 || end < len(lines) {
		header += fmt.Sprintf(" [显示第%d-%d行]", offset+1, end)
	}

	return agent.ToolResult{
		Content:    header + "\n\n" + content,
		UserFacing: content,
	}, nil
}

// generateOverview produces a structured synopsis of a markdown document.
// This is the L1 tier: headers + first sentence per section + word count.
// Typically ~200-500 tokens vs ~2000-8000 for full content.
//
// Adapted from OpenViking's tool_result_synopsis pattern (deterministic,
// no LLM needed — pure text extraction rules).
func generateOverview(relPath string, lines []string) string {
	var b strings.Builder

	totalLines := len(lines)
	wordCount := 0
	var headers []string // lines starting with #
	var sections []string // header + first content line

	scanner := bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n")))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024) // 1MB max line — default 64KB silently truncates long lines
	var currentHeader string
	var headerContent strings.Builder

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#") {
			// Save previous section
			if currentHeader != "" {
				firstLine := strings.TrimSpace(headerContent.String())
				if firstLine != "" {
					firstLine = truncateRunes(firstLine, 120)
					sections = append(sections, fmt.Sprintf("  %s\n    → %s", currentHeader, firstLine))
				} else {
					sections = append(sections, fmt.Sprintf("  %s", currentHeader))
				}
			}
			currentHeader = line
			headers = append(headers, line)
			headerContent.Reset()
			continue
		}

		// Accumulate first non-empty content line after header
		if currentHeader != "" && line != "" && !strings.HasPrefix(line, ">") && headerContent.Len() == 0 {
			headerContent.WriteString(line)
		}

		// Count words
		wordCount += len(strings.Fields(line))
	}

	// Save last section
	if currentHeader != "" {
		firstLine := strings.TrimSpace(headerContent.String())
		if firstLine != "" {
			firstLine = truncateRunes(firstLine, 120)
			sections = append(sections, fmt.Sprintf("  %s\n    → %s", currentHeader, firstLine))
		} else {
			sections = append(sections, fmt.Sprintf("  %s", currentHeader))
		}
	}

	// Build output
	b.WriteString(fmt.Sprintf("📋 概览：%s\n", relPath))
	b.WriteString(fmt.Sprintf("   %d 行 | %d 词 | %d 个标题\n\n", totalLines, wordCount, len(headers)))

	if len(sections) > 0 {
		b.WriteString("📑 文档结构：\n")
		for _, s := range sections {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n💡 如需完整内容，用 kb_read path=" + relPath + "（不加 overview）")

	return b.String()
}

// truncateRunes returns s truncated to maxRunes runes, with "..." if truncated.
// Rune-safe: does not split multi-byte UTF-8 characters.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
