package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func knowledgeFixture(tb testing.TB, view string) *preview {
	tb.Helper()
	a := newConnectedPreview(tb.TempDir())
	tb.Cleanup(a.closeLive)
	a.Workbench = true
	a.Live.Workbench.Tab = "kb"
	a.Live.Workbench.KBView = view
	entries := make([]any, 3407)
	tags := make([]any, 6304)
	for i := range entries {
		entries[i] = map[string]any{"path": fmt.Sprintf("docs/%d.md", i), "rel_path": fmt.Sprintf("docs/%d.md", i), "title": fmt.Sprintf("知识条目 %04d", i), "summary": "用于验证大量知识条目的滚动和界面响应。", "category": "knowledge", "tags": []any{"性能", "桌面", "排版"}}
	}
	for i := range tags {
		tags[i] = map[string]any{"name": fmt.Sprintf("知识标签 %04d", i), "count": 12}
	}
	a.Live.Data["kb"] = map[string]any{"entries": entries}
	a.Live.Data["kb-tags"] = map[string]any{"tags": tags}
	a.Live.Data["kb-stats"] = map[string]any{"total_entries": 3407, "tags": 6304, "categories": 9}
	return a
}

func BenchmarkKnowledgeWorkbench(b *testing.B) {
	for _, view := range []string{"browse", "tags"} {
		b.Run(view, func(b *testing.B) {
			v := view
			if v == "browse" {
				v = ""
			}
			a := knowledgeFixture(b, v)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tt := ui.NewTester(a.View, 1240, 820)
				tt.Frame()
			}
		})
	}
}

func TestKnowledgeVirtualizedEntriesAndTags(t *testing.T) {
	for _, view := range []string{"", "tags"} {
		t.Run("view-"+view, func(t *testing.T) {
			a := knowledgeFixture(t, view)
			tt := ui.NewTester(a.View, 1240, 820)
			if len(tt.Texts()) > 500 {
				t.Fatalf("built offscreen items: %d texts", len(tt.Texts()))
			}
			label := "知识条目：知识条目 3406"
			if view == "" {
				a.Live.Workbench.KBList.ScrollTo(3406, ui.End)
			} else {
				a.Live.Workbench.KBTags.ScrollTo(6303, ui.End)
				label = "标签：知识标签 6303"
			}
			tt.Frame()
			if _, ok := tt.Find(label); !ok {
				t.Fatalf("last item unreachable: %s", label)
			}
			if len(tt.Texts()) > 500 {
				t.Fatalf("built offscreen items after scrolling: %d", len(tt.Texts()))
			}
		})
	}
}

func TestKnowledgeSearchDeadlineTicksWithoutInput(t *testing.T) {
	a := knowledgeFixture(t, "")
	a.Live.Workbench.KBSearchAt = time.Now().Add(-time.Second)
	a.liveTick()
	if !a.Live.Workbench.KBSearchAt.IsZero() {
		t.Fatal("debounce waited for another input frame")
	}
}

func TestKnowledgeCountFormatting(t *testing.T) {
	for _, v := range []any{float64(42), 42, int64(42)} {
		if got := formatCount(v); got != "42" {
			t.Fatalf("count %T: %s", v, got)
		}
	}
}

func TestKnowledgeHealthVirtualizesExpandedFindings(t *testing.T) {
	a := knowledgeFixture(t, "health")
	entries := list(object(a.Live.Data["kb"])["entries"])
	a.Live.Data["kb-health"] = map[string]any{"total_entries": float64(len(entries)), "entries_missing_summary": entries}
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "缺少摘要 · 3407")
	if len(tt.Texts()) > 500 {
		t.Fatalf("expanded all findings: %d", len(tt.Texts()))
	}
	if _, ok := tt.Find("知识条目 0000 docs/0.md"); !ok {
		t.Fatal("missing visible finding")
	}
}
