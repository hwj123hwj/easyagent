package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"strings"
	"testing"
)

func TestLargeMarkdownUsesVirtualSource(t *testing.T) {
	for _, width := range []int{900, 1240} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := alignmentPreview(t)
			a.Workbench = true
			text := "| 项目 | 描述 |\n|---|---|\n" + strings.Repeat("| 条目 | 中文排版 😀 abc |\n", 5000) + "最后一行完整保留"
			a.current().Messages = []*message{{Text: text}}
			tt := ui.NewTester(a.View, width, 640)
			tt.SetLocale("zh-Hans")
			if _, ok := tt.Find("大文本源码"); !ok {
				t.Fatal("large table entered unbounded rich layout")
			}
			for i := 0; i < 10; i++ {
				tt.Frame()
			}
			if a.current().Messages[0].Text != text {
				t.Fatal("source was truncated")
			}
		})
	}
}
func TestRichLayoutBudget(t *testing.T) {
	for _, text := range []string{strings.Repeat("中", 30000), strings.Repeat("x\n", 401)} {
		if !largeRichText(text) {
			t.Fatal("unbounded rich text")
		}
	}
	if largeRichText("# 普通文档\n\n正常 **Markdown**") {
		t.Fatal("normal markdown disabled")
	}
}

func TestLargeSourceFindKeepsEditorFocus(t *testing.T) {
	a := alignmentPreview(t)
	a.current().Messages = []*message{{Text: strings.Repeat("long source line\n", 500)}}
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "大文本源码")
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if a.current().FindOpen {
		t.Fatal("conversation search stole editor shortcut")
	}
}
