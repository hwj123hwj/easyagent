package feishu

import (
	"strings"
	"testing"
)

func TestTruncateCardText_Short(t *testing.T) {
	text := "hello world"
	got := truncateCardText(text)
	if got != text {
		t.Errorf("expected unchanged text, got %q", got)
	}
}

func TestTruncateCardText_ExactBoundary(t *testing.T) {
	text := strings.Repeat("a", CardKitMaxContentChars)
	got := truncateCardText(text)
	if got != text {
		t.Errorf("expected unchanged text at boundary, got len=%d", len(got))
	}
}

func TestTruncateCardText_OverLimit(t *testing.T) {
	text := strings.Repeat("a", CardKitMaxContentChars+100)
	got := truncateCardText(text)
	if !strings.HasSuffix(got, "（内容过长，已截断）") {
		t.Error("expected truncation notice suffix")
	}
	if len(got) <= CardKitMaxContentChars {
		t.Errorf("expected truncated length > %d, got %d", CardKitMaxContentChars, len(got))
	}
}

func TestFormatElapsed_Seconds(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{1500, "1.5s"},
		{500, "0.5s"},
		{59000, "59.0s"},
	}
	for _, tt := range tests {
		got := formatElapsed(tt.ms)
		if got != tt.want {
			t.Errorf("formatElapsed(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestFormatElapsed_Minutes(t *testing.T) {
	got := formatElapsed(125000) // 125s = 2m 5s
	if got != "2m 5s" {
		t.Errorf("formatElapsed(125000) = %q, want %q", got, "2m 5s")
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{100000, "100,000"},
	}
	for _, tt := range tests {
		got := formatNumber(tt.n)
		if got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRenderFooterMarkdown_EmptyMetrics(t *testing.T) {
	got := RenderFooterMarkdown(FooterMetrics{})
	if got != "" {
		t.Errorf("expected empty string for zero metrics, got %q", got)
	}
}

func TestRenderFooterMarkdown_StatusOnly(t *testing.T) {
	// "Processing" matches the "processing" keyword → grey
	got := RenderFooterMarkdown(FooterMetrics{Status: "Processing"})
	if !strings.Contains(got, "Processing") {
		t.Errorf("expected status in output, got %q", got)
	}
	if !strings.Contains(got, "grey") {
		t.Errorf("expected grey color for processing status, got %q", got)
	}
}

func TestRenderFooterMarkdown_UnknownStatus(t *testing.T) {
	// Unknown status → green
	got := RenderFooterMarkdown(FooterMetrics{Status: "已完成"})
	if !strings.Contains(got, "green") {
		t.Errorf("expected green for normal status, got %q", got)
	}
}

func TestRenderFooterMarkdown_ErrorStatus(t *testing.T) {
	got := RenderFooterMarkdown(FooterMetrics{Status: "Error: timeout"})
	if !strings.Contains(got, "red") {
		t.Errorf("expected red color for error status, got %q", got)
	}
}

func TestRenderFooterMarkdown_CompleteMetrics(t *testing.T) {
	m := FooterMetrics{
		Status:            "已完成",
		ElapsedMs:         3500,
		Model:             "claude-sonnet-4-20250514",
		InputTokens:       1500,
		OutputTokens:      800,
		CacheReadTokens:   1200,
		CacheHitRate:      80.0,
		ContextPercentage: 45.0,
	}
	got := RenderFooterMarkdown(m)

	// Check all parts present
	checks := []string{
		"已完成",
		"3.5s",
		"claude-sonnet-4-20250514",
		"↑1,500 ↓800",
		"缓存读取 1,200 (80.0%)",
		"上下文剩余 55%",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in footer, got %q", want, got)
		}
	}
}

func TestRenderFooterMarkdown_CacheWithoutHitRate(t *testing.T) {
	m := FooterMetrics{CacheReadTokens: 500}
	got := RenderFooterMarkdown(m)
	if !strings.Contains(got, "缓存读取 500") {
		t.Errorf("expected cache text, got %q", got)
	}
	if strings.Contains(got, "(") {
		t.Errorf("should not contain hit rate parens when rate is 0, got %q", got)
	}
}

func TestBuildStreamingCard_Structure(t *testing.T) {
	card := BuildStreamingCard("hello", "footer text")

	// Schema 2.0
	if card["schema"] != "2.0" {
		t.Errorf("schema = %v, want 2.0", card["schema"])
	}

	config, ok := card["config"].(map[string]any)
	if !ok {
		t.Fatal("missing config")
	}
	if config["streaming_mode"] != false {
		t.Error("native typewriter should be disabled")
	}

	body, ok := card["body"].(map[string]any)
	if !ok {
		t.Fatal("missing body")
	}
	elements, ok := body["elements"].([]any)
	if !ok {
		t.Fatal("missing elements")
	}
	// 3 elements: content, loading icon, footer
	if len(elements) != 3 {
		t.Errorf("expected 3 elements, got %d", len(elements))
	}
}

func TestBuildStreamingCard_EmptyFooter(t *testing.T) {
	card := BuildStreamingCard("hello", "")
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	footerElem := elements[2].(map[string]any)
	if footerElem["content"] != " " {
		t.Errorf("expected space for empty footer, got %q", footerElem["content"])
	}
}

func TestBuildFinalCard_WithMetrics(t *testing.T) {
	m := &FooterMetrics{Status: "done", ElapsedMs: 1000}
	card := BuildFinalCard("answer", m)

	config := card["config"].(map[string]any)
	if config["streaming_mode"] != false {
		t.Error("streaming_mode should be false")
	}

	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	// 2 elements: content + footer
	if len(elements) != 2 {
		t.Errorf("expected 2 elements, got %d", len(elements))
	}
}

func TestBuildFinalCard_NilMetrics(t *testing.T) {
	card := BuildFinalCard("answer", nil)
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	// 1 element: content only
	if len(elements) != 1 {
		t.Errorf("expected 1 element without metrics, got %d", len(elements))
	}
}

func TestBuildFinalCard_SummaryTruncation(t *testing.T) {
	longContent := strings.Repeat("x", 200)
	card := BuildFinalCard(longContent, nil)
	config := card["config"].(map[string]any)
	summary := config["summary"].(map[string]any)
	content := summary["content"].(string)
	if len(content) > 120 {
		t.Errorf("summary should be <= 120 chars, got %d", len(content))
	}
}

func TestBuildFinalCard_SummaryStripsMarkdown(t *testing.T) {
	card := BuildFinalCard("**bold** _italic_ #heading `code`", nil)
	config := card["config"].(map[string]any)
	summary := config["summary"].(map[string]any)
	content := summary["content"].(string)
	if strings.ContainsAny(content, "*_#`") {
		t.Errorf("summary should strip markdown chars, got %q", content)
	}
}

func TestBuildWorktreeCard_WithActions(t *testing.T) {
	route := &ChatRoute{
		WorktreeRoot:   "/tmp/repo/.easyagent/worktrees/task",
		WorktreeBranch: "easyagent/task",
	}
	card := BuildWorktreeCard("oc_chat", route, "status text")

	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 3 {
		t.Fatalf("expected status, input, actions elements, got %d", len(elements))
	}
	input := elements[1].(map[string]any)
	if input["tag"] != "input" || input["name"] != worktreeCardCommitMessage {
		t.Fatalf("unexpected input element: %#v", input)
	}
	actions := elements[2].(map[string]any)["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(actions))
	}
	for _, action := range actions {
		button := action.(map[string]any)
		value := button["value"].(map[string]any)
		if value[worktreeCardChatKey] != "oc_chat" {
			t.Fatalf("button missing chat key: %#v", button)
		}
		if value[worktreeCardActionKey] == "" {
			t.Fatalf("button missing action: %#v", button)
		}
	}
}

func TestBuildWorktreeCard_NoActionsWithoutWorktree(t *testing.T) {
	card := BuildWorktreeCard("oc_chat", &ChatRoute{ProjectRoot: "/tmp/repo"}, "no worktree")
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("expected only status element, got %d", len(elements))
	}
}
