package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSynopsisAfterHook_SmallOutput(t *testing.T) {
	result := ToolResult{Content: "small output"}
	got, err := SynopsisAfterHook(context.Background(), ToolCallContext{}, result)
	if err != nil {
		t.Fatal(err)
	}
	// Small output should pass through unchanged
	if got.Content != "small output" {
		t.Errorf("small output should not be synopsized, got: %s", got.Content)
	}
}

func TestSynopsisAfterHook_LargeOutput(t *testing.T) {
	// Generate large output
	large := strings.Repeat("line of text\n", 500) // ~6500 chars
	result := ToolResult{Content: large}

	// 默认阈值已与 read/grep 的 30000 截断对齐，这里用小阈值直接测概览化行为。
	hook := NewSynopsisAfterHook(SynopsisSettings{Threshold: 4000}, nil)
	got, err := hook(context.Background(), ToolCallContext{ToolName: "web_fetch"}, result)
	if err != nil {
		t.Fatal(err)
	}

	// Synopsis should be much smaller
	if len(got.Content) >= len(large) {
		t.Errorf("synopsis (%d) should be smaller than original (%d)",
			len(got.Content), len(large))
	}

	// Full output should be preserved in UserFacing
	if got.UserFacing != large {
		t.Error("UserFacing should contain full output")
	}

	// Synopsis should mention original size
	if !strings.Contains(got.Content, "概览") {
		t.Errorf("synopsis should mention overview, got: %s", got.Content[:100])
	}
}

func TestSynopsisAfterHook_DefaultThresholdMatchesToolTruncation(t *testing.T) {
	// 30000 字符以内（read/bash 自身截断上限）不应被默认设置概览化。
	large := strings.Repeat("x", DefaultSynopsisThreshold)
	got, err := SynopsisAfterHook(context.Background(), ToolCallContext{ToolName: "web_fetch"}, ToolResult{Content: large})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != large {
		t.Error("output at the tool truncation limit should not be synopsized by default")
	}
}

func TestSynopsisAfterHook_ExemptTool(t *testing.T) {
	large := strings.Repeat("line of text\n", 500)
	hook := NewSynopsisAfterHook(SynopsisSettings{Threshold: 4000}, nil)

	// read/grep 等豁免工具的输出本身就是给模型看的，超阈值也保持原文。
	for _, name := range []string{"read", "grep", "read_many_files"} {
		got, err := hook(context.Background(), ToolCallContext{ToolName: name}, ToolResult{Content: large})
		if err != nil {
			t.Fatal(err)
		}
		if got.Content != large {
			t.Errorf("exempt tool %s output should pass through unchanged", name)
		}
	}

	// 同一输出在未豁免的工具上仍会概览化。
	got, err := hook(context.Background(), ToolCallContext{ToolName: "web_fetch"}, ToolResult{Content: large})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content == large {
		t.Error("non-exempt tool output should be synopsized")
	}
}

func TestSynopsisAfterHook_FullOutputRef(t *testing.T) {
	large := strings.Repeat("data line\n", 800) // ~8000 chars
	store := NewToolOutputStore(0)
	hook := NewSynopsisAfterHook(SynopsisSettings{Threshold: 4000}, store)

	got, err := hook(context.Background(), ToolCallContext{ToolName: "web_fetch"}, ToolResult{Content: large})
	if err != nil {
		t.Fatal(err)
	}

	// 概览结尾给出 full_output 取回指引。
	if !strings.Contains(got.Content, "full_output") || !strings.Contains(got.Content, `ref="out_1"`) {
		t.Errorf("synopsis should point to full_output with a ref, got tail: %s", got.Content[max(0, len(got.Content)-120):])
	}

	// 模型经 full_output 按引用取回全文。
	tool := NewFullOutputTool(store)
	raw, err := json.Marshal(map[string]string{"ref": "out_1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != large {
		t.Error("full_output should return the exact stored output")
	}
}

func TestSynopsisAfterHook_NoDoubleSynopsis(t *testing.T) {
	// Simulate a tool that already produced a synopsis (e.g. kb_read overview=true)
	large := "[输出概览] " + strings.Repeat("line\n", 1000)
	result := ToolResult{Content: large, UserFacing: "full content here"}

	hook := NewSynopsisAfterHook(SynopsisSettings{Threshold: 4000}, nil)
	got, err := hook(context.Background(), ToolCallContext{ToolName: "web_fetch"}, result)
	if err != nil {
		t.Fatal(err)
	}

	// Content should NOT be double-synopsized
	if got.Content != large {
		t.Error("should not re-synopsis content that already contains [输出概览]")
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"go code", "package main\n\nfunc main() {\n}\n", "code"},
		{"python", "def hello():\n    print('hi')\n", "code"},
		{"json object", `{"key": "value", "num": 42}`, "json"},
		{"json array", `[1, 2, 3]`, "json"},
		{"markdown", "# Title\n\n## Section\n\n### Sub", "markdown"},
		{"plain text", "Hello world this is plain text", "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.content, "\n")
			got := detectContentType(tt.content, lines)
			if got != tt.want {
				t.Errorf("detectContentType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeCode(t *testing.T) {
	code := `package main

import "fmt"
import "os"

func main() {
	fmt.Println("hello")
}

func helper(x int) int {
	return x * 2
}

type Foo struct {
	Bar string
}`
	lines := strings.Split(code, "\n")
	got := summarizeCode(lines)

	if !strings.Contains(got, "main") {
		t.Errorf("code summary should mention 'main' function, got: %s", got)
	}
	if !strings.Contains(got, "helper") {
		t.Errorf("code summary should mention 'helper' function, got: %s", got)
	}
	if !strings.Contains(got, "import") || !strings.Contains(got, "fmt") {
		t.Errorf("code summary should mention imports, got: %s", got)
	}
}

func TestSummarizeMarkdown(t *testing.T) {
	md := `# Title

Some text.

## Section A

Content.

## Section B

More content.

### Subsection

Details.`
	lines := strings.Split(md, "\n")
	got := summarizeMarkdown(lines)

	if !strings.Contains(got, "# Title") {
		t.Errorf("markdown summary should contain title, got: %s", got)
	}
	if !strings.Contains(got, "## Section A") {
		t.Errorf("markdown summary should contain sections, got: %s", got)
	}
}

func TestGenerateSynopsis_Code(t *testing.T) {
	largeCode := strings.Repeat("func f"+strings.Repeat("x", 50)+"() {}\n", 200)
	got := GenerateSynopsis(largeCode)

	if !strings.Contains(got, "代码") {
		t.Errorf("code synopsis should mention code structure, got: %s", got[:100])
	}
	if !strings.Contains(got, "摘录") {
		t.Errorf("synopsis should have excerpt, got: %s", got[:100])
	}
}

func TestGenerateSynopsis_JSON(t *testing.T) {
	largeJSON := `{"items": [` + strings.Repeat(`{"id": 1, "name": "test", "data": "padding"},`, 100) + `]}`
	got := GenerateSynopsis(largeJSON)

	if !strings.Contains(got, "JSON") {
		t.Errorf("JSON synopsis should mention JSON, got: %s", got[:100])
	}
}

func TestTruncateToFirstN(t *testing.T) {
	// ASCII case
	got := truncateToFirstN("hello world", 5)
	if got != "hello..." {
		t.Errorf("truncateToFirstN = %q, want 'hello...'", got)
	}
	got = truncateToFirstN("hi", 10)
	if got != "hi" {
		t.Errorf("truncateToFirstN short = %q, want 'hi'", got)
	}
	// UTF-8 / Chinese: must not split multi-byte chars
	got = truncateToFirstN("你好世界测试", 3)
	if got != "你好世..." {
		t.Errorf("truncateToFirstN Chinese = %q, want '你好世...'", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncateToFirstN produced invalid UTF-8: %q", got)
	}
}

func TestTruncateToLastN(t *testing.T) {
	got := truncateToLastN("hello world", 5)
	if got != "...world" {
		t.Errorf("truncateToLastN = %q, want '...world'", got)
	}
	got = truncateToLastN("hi", 10)
	if got != "hi" {
		t.Errorf("truncateToLastN short = %q, want 'hi'", got)
	}
	// UTF-8 / Chinese
	got = truncateToLastN("你好世界测试", 3)
	if got != "...界测试" {
		t.Errorf("truncateToLastN Chinese = %q, want '...界测试'", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncateToLastN produced invalid UTF-8: %q", got)
	}
}
