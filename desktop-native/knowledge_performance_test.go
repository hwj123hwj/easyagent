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
				a.Live.Workbench.KBTags.ScrollTo(len(a.Live.Workbench.KBCloud.rows)-1, ui.End)
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

func TestKnowledgeTagCloudParityAndCache(t *testing.T) {
	a := knowledgeFixture(t, "tags")
	tags := []any{map[string]any{"name": "常用", "count": 100}, map[string]any{"name": "少用", "count": 1}}
	a.Live.Data["kb-tags"] = map[string]any{"tags": tags}
	a.Live.Workbench.KBCategory = "docs"
	a.Live.Workbench.KBQuery = "preserve"
	tt := ui.NewTester(a.View, 1240, 820)
	cloud := &a.Live.Workbench.KBCloud
	if cloud.tags[0].size != 22 || cloud.tags[1].size != 12 {
		t.Fatalf("lost frequency sizing: %+v", cloud.tags)
	}
	first := &cloud.tags[0]
	tt.Frame()
	if first != &cloud.tags[0] {
		t.Fatal("remeasured unchanged tags")
	}
	click(t, tt, "标签：常用")
	w := &a.Live.Workbench
	if w.KBView != "tags" || w.KBActiveTag != "常用" || w.KBCategory != "docs" || w.KBQuery != "preserve" {
		t.Fatal("tag drilldown changed browse filters")
	}
	a.Live.Data["kb-tag-entries"] = map[string]any{"entries": []any{map[string]any{"title": "目标文章", "rel_path": "target.md"}}}
	tt.Frame()
	click(t, tt, "知识条目：目标文章")
	if w.KBPath != "target.md" {
		t.Fatal("entry did not open")
	}
	click(t, tt, "返回")
	if w.KBView != "tags" || w.KBActiveTag != "常用" {
		t.Fatal("detail lost tag context")
	}
	click(t, tt, "返回标签云")
	if w.KBActiveTag != "" {
		t.Fatal("tag cloud did not return")
	}
}

func BenchmarkKnowledgeTagCloudFrame(b *testing.B) {
	a := knowledgeFixture(b, "tags")
	tt := ui.NewTester(a.View, 1240, 820)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tt.Frame()
	}
}

func TestKnowledgeTagCloudReflowsWithoutRemeasure(t *testing.T) {
	a := knowledgeFixture(t, "tags")
	var width float32 = 600
	tt := ui.NewTester(func(c *ui.Context) {
		a.Live.Workbench.KBCloud.layout(c, list(object(a.Live.Data["kb-tags"])["tags"]), width)
	}, 700, 600)
	cloud := &a.Live.Workbench.KBCloud
	first := &cloud.tags[0]
	wideRows := len(cloud.rows)
	width = 220
	tt.Frame()
	if first != &cloud.tags[0] || len(cloud.rows) <= wideRows {
		t.Fatal("resize failed to reuse metrics and reflow")
	}
	n := 0
	for _, row := range cloud.rows {
		used := float32(0)
		for j, i := range row {
			if j > 0 {
				used += 12
			}
			used += min(width, cloud.tags[i].width)
			if i != n {
				t.Fatal("tag order changed")
			}
			n++
		}
		if used > width {
			t.Fatal("row overflows pane")
		}
	}
	if n != 6304 {
		t.Fatalf("tags lost during resize: %d", n)
	}
}
