package agent

// ────────────────────────────────────────────────────────────────────────────
//  Tool Output Synopsis
//
//  Adapted from OpenViking's tool_result_synopsis.py.
//  When a tool returns a large output (over threshold chars), the After hook
//  automatically generates a deterministic synopsis (structure + excerpt) and
//  replaces the LLM-facing Content with it. The full output is preserved in
//  UserFacing (for the UI to display), and optionally in a ToolOutputStore
//  so the model can retrieve it via the full_output tool.
//
//  This prevents large tool outputs from consuming context window tokens.
//  The synopsis gives the LLM enough information to decide if it needs to
//  re-read the full output (e.g. via full_output).
//
//  Key differences from OpenViking:
//  1. No external storage — synopsis replaces Content in-place, full text
//     stays in UserFacing (OpenViking stores to VikingFS with a ref)
//  2. Pure rules-based — no LLM call needed for synopsis generation
//  3. Content-type aware: code gets imports/symbols, text gets headers/excerpt
// ────────────────────────────────────────────────────────────────────────────

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	// DefaultSynopsisThreshold 与 read/grep/bash 自身的 51200 字符（50KB）
	// 截断（sdk/tools DefaultMaxOutputLen）对齐：自带截断的工具不会触发
	// 概览化，概览只处理 web_fetch 等没有自带截断的天然大输出，不再卡
	// 编码场景的读文件与搜索结果。
	DefaultSynopsisThreshold = 51200
	synopsisSkipMarker       = "[输出概览]" // if Content already contains this, skip double-synopsis
	maxExcerptChars          = 600
	maxHeadersShown          = 10
	maxCodeSymbols           = 15
	defaultStoreEntries      = 16
)

// SynopsisSettings 控制 NewSynopsisAfterHook 的行为，零值字段沿用默认。
type SynopsisSettings struct {
	// Threshold 超过该字符数的工具输出被概览化；<=0 时用 DefaultSynopsisThreshold。
	Threshold int
	// ExemptTools 不做概览化的工具名。输出本身就是给模型看的内容的工具
	// （read/grep 等）应豁免，概览化主要留给 bash/web_fetch 等天然大输出。
	// nil 沿用默认豁免表；空 map 表示不豁免任何工具。
	ExemptTools map[string]bool
}

// DefaultSynopsisSettings 返回默认设置：阈值 DefaultSynopsisThreshold，
// 豁免 read / grep / read_many_files。
func DefaultSynopsisSettings() SynopsisSettings {
	return SynopsisSettings{
		Threshold: DefaultSynopsisThreshold,
		ExemptTools: map[string]bool{
			"read":            true,
			"grep":            true,
			"read_many_files": true,
		},
	}
}

func (s SynopsisSettings) threshold() int {
	if s.Threshold > 0 {
		return s.Threshold
	}
	return DefaultSynopsisThreshold
}

func (s SynopsisSettings) exempt(toolName string) bool {
	if s.ExemptTools == nil {
		s = DefaultSynopsisSettings()
	}
	return s.ExemptTools[toolName]
}

// NewSynopsisAfterHook 构造自动概览化钩子。store 非 nil 时，概览化会把全文
// 登记进 store 并在结尾给出 full_output 取回指引；nil 时保持旧行为
// （全文只对用户可见）。
func NewSynopsisAfterHook(settings SynopsisSettings, store *ToolOutputStore) AfterToolCallHook {
	return func(_ context.Context, call ToolCallContext, result ToolResult) (ToolResult, error) {
		if settings.exempt(call.ToolName) {
			return result, nil // 工具输出本身就是给模型看的，不概览化
		}
		if len(result.Content) <= settings.threshold() {
			return result, nil // small enough, no synopsis needed
		}

		// Skip if Content already appears to be a synopsis (avoid double-synopsis
		// when a tool like kb_read overview=true already returned structured output)
		if strings.Contains(result.Content, synopsisSkipMarker) {
			return result, nil
		}

		// Preserve full output for the UI (only if not already set by the tool)
		if result.UserFacing == "" {
			result.UserFacing = result.Content
		}

		// Generate synopsis and replace LLM-facing content
		ref := ""
		if store != nil {
			ref = store.Put(result.Content)
		}
		result.Content = generateSynopsis(result.Content, ref)

		return result, nil
	}
}

// SynopsisAfterHook 用默认设置且不带全文取回的自动概览化钩子。
// Usage:
//
//	hooks.After = append(hooks.After, SynopsisAfterHook)
func SynopsisAfterHook(ctx context.Context, call ToolCallContext, result ToolResult) (ToolResult, error) {
	return NewSynopsisAfterHook(DefaultSynopsisSettings(), nil)(ctx, call, result)
}

// GenerateSynopsis produces a deterministic synopsis of a large text output.
// It detects content type (code, markdown, JSON, plain text) and extracts
// the most relevant structural information.
func GenerateSynopsis(content string) string {
	return generateSynopsis(content, "")
}

// generateSynopsis 生成概览；ref 非空时结尾给出 full_output 取回指引，
// 替代「告知后可取全文」但无处可取的旧文案。
func generateSynopsis(content, ref string) string {
	var b strings.Builder

	originalLen := len(content)
	b.WriteString(fmt.Sprintf("📋 [输出概览] 原始内容 %d 字符\n\n", originalLen))

	// Detect content type and extract structure
	lines := strings.Split(content, "\n")

	switch detectContentType(content, lines) {
	case "code":
		b.WriteString(summarizeCode(lines))
	case "markdown":
		b.WriteString(summarizeMarkdown(lines))
	case "json":
		b.WriteString(summarizeJSON(lines))
	default:
		b.WriteString(summarizeText(content, lines))
	}

	// Always include head + tail excerpt
	b.WriteString("\n📎 开头摘录:\n")
	b.WriteString(truncateToFirstN(content, maxExcerptChars/2))
	b.WriteString("\n\n📎 结尾摘录:\n")
	b.WriteString(truncateToLastN(content, maxExcerptChars/2))

	if ref != "" {
		b.WriteString(fmt.Sprintf("\n\n💡 如需完整内容，调用 full_output 工具取回（ref=\"%s\"）。", ref))
	} else {
		b.WriteString(fmt.Sprintf("\n\n💡 完整内容已对用户可见（%d 字符）。如需查看完整内容请告知。", originalLen))
	}

	return b.String()
}

// detectContentType guesses the content type from text patterns.
func detectContentType(content string, lines []string) string {
	// Check for code patterns
	codeRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*(func |func\(|package |import |var |const |type )`),
		regexp.MustCompile(`(?m)^\s*(class |def |import |from .* import|public |private )`),
		regexp.MustCompile(`(?m)^\s*(fn |pub fn|use |mod )`),
	}
	for _, r := range codeRegexes {
		if r.MatchString(content) {
			return "code"
		}
	}

	// Check for JSON
	trimmed := strings.TrimSpace(content)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		return "json"
	}

	// Check for markdown
	headerCount := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			headerCount++
		}
	}
	if headerCount >= 3 {
		return "markdown"
	}

	return "text"
}

// summarizeCode extracts imports and function/class definitions.
func summarizeCode(lines []string) string {
	var b strings.Builder
	b.WriteString("📦 代码结构:\n")

	symbolRegex := regexp.MustCompile(`^\s*(func\s+|func\(|class\s+|def\s+|async\s+def\s+|fn\s+|pub\s+fn|public\s+class|public\s+static|type\s+)\s*(\w+)`)
	importRegex := regexp.MustCompile(`^\s*(import\s+|from\s+|use\s+|#include|require\()`)

	symbols := 0
	imports := 0
	for _, line := range lines {
		if importRegex.MatchString(line) && imports < 8 {
			b.WriteString(fmt.Sprintf("  📥 %s\n", strings.TrimSpace(line)))
			imports++
			continue // don't double-count an import line as a symbol
		}
		if m := symbolRegex.FindStringSubmatch(line); m != nil {
			b.WriteString(fmt.Sprintf("  • %s\n", strings.TrimSpace(m[0])))
			symbols++
			if symbols >= maxCodeSymbols {
				b.WriteString("  ...(更多符号省略)\n")
				break
			}
		}
	}

	b.WriteString(fmt.Sprintf("\n  共 %d 行\n", len(lines)))
	return b.String()
}

// summarizeMarkdown extracts headers and section structure.
func summarizeMarkdown(lines []string) string {
	var b strings.Builder
	b.WriteString("📑 文档结构:\n")

	headers := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			b.WriteString(fmt.Sprintf("  %s\n", trimmed))
			headers++
			if headers >= maxHeadersShown {
				b.WriteString("  ...(更多标题省略)\n")
				break
			}
		}
	}

	b.WriteString(fmt.Sprintf("\n  共 %d 行\n", len(lines)))
	return b.String()
}

// summarizeJSON extracts top-level keys and structure.
func summarizeJSON(lines []string) string {
	var b strings.Builder
	b.WriteString("🔧 JSON 结构:\n")

	// Simple approach: find top-level keys (lines with low indentation)
	keyRegex := regexp.MustCompile(`^[\{\[]?\s*"([^"]+)"\s*:`)

	keys := make(map[string]bool)
	for _, line := range lines {
		if m := keyRegex.FindStringSubmatch(line); m != nil {
			if !keys[m[1]] {
				b.WriteString(fmt.Sprintf("  • %s\n", m[1]))
				keys[m[1]] = true
				if len(keys) >= 15 {
					break
				}
			}
		}
	}

	b.WriteString(fmt.Sprintf("\n  共 %d 行\n", len(lines)))
	return b.String()
}

// summarizeText extracts statistics for plain text.
func summarizeText(content string, lines []string) string {
	var b strings.Builder
	b.WriteString("📝 文本概要:\n")
	b.WriteString(fmt.Sprintf("  • %d 行\n", len(lines)))
	b.WriteString(fmt.Sprintf("  • %d 字符\n", len(content)))
	b.WriteString(fmt.Sprintf("  • %d 词\n", len(strings.Fields(content))))
	return b.String()
}

// truncateToFirstN returns the first n runes, with "..." if truncated.
// Rune-safe: does not split multi-byte UTF-8 characters.
func truncateToFirstN(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// truncateToLastN returns the last n runes, with "..." if truncated.
// Rune-safe: does not split multi-byte UTF-8 characters.
func truncateToLastN(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return "..." + string(runes[len(runes)-n:])
}
