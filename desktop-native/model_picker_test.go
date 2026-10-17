package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestModelPickerWheelAndKeyboard(t *testing.T) {
	for _, width := range []int{900, 1240} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := alignmentPreview(t)
			a.ModelMenu = true
			a.Live.Models = nil
			for i := 0; i < 137; i++ {
				a.Live.Models = append(a.Live.Models, fmt.Sprintf("model-%03d", i))
			}
			tt := ui.NewTester(a.View, width, 640)
			click(t, tt, "搜索模型")
			viewport, ok := tt.Find("模型列表")
			if !ok {
				t.Fatal("missing list")
			}
			before, _ := tt.Find("选择模型：model-000")
			tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 400)
			for i := 0; i < 5; i++ {
				tt.Frame()
			}
			after, _ := tt.Find("选择模型：model-000")
			if after.H > 0 && after.Y >= before.Y-100 {
				t.Fatalf("wheel snapped back: before=%+v after=%+v", before, after)
			}
			tt.Key(0, ui.KeyDown)
			tt.Frame()
			row, _ := tt.Find("选择模型：model-001")
			if row.Y < viewport.Y || row.Y+row.H > viewport.Y+viewport.H+1 {
				t.Fatalf("keyboard row not visible: %+v in %+v", row, viewport)
			}
			tt.Type("136")
			tt.Frame()
			if !tt.HasText("model-136") || a.Live.ModelIndex != 0 {
				t.Fatal("search failed to reset navigation")
			}
			tt.Key(0, ui.KeyEscape)
			if a.ModelMenu {
				t.Fatal("escape did not dismiss")
			}
		})
	}
}

func TestCompletionMenusKeepKeyboardSelectionVisible(t *testing.T) {
	for _, kind := range []string{"commands", "mentions"} {
		t.Run(kind, func(t *testing.T) {
			a := alignmentPreview(t)
			s := a.current()
			label := "命令列表"
			if kind == "commands" {
				a.SlashMenu = true
			} else {
				label = "文件引用列表"
				s.Draft = "@"
				files := []any{}
				for i := 0; i < 40; i++ {
					files = append(files, fmt.Sprintf("file-%02d.go", i))
				}
				a.Live.Data["mention/"+s.RemoteID] = files
			}
			tt := ui.NewTester(a.View, 900, 640)
			click(t, tt, "消息输入")
			for i := 0; i < 9; i++ {
				tt.Key(0, ui.KeyDown)
			}
			tt.Frame()
			rowLabel := "file-09.go"
			if kind == "commands" {
				m := a.commandMatches(s.Draft)
				rowLabel = m[9].Name + " · " + m[9].Description
			}
			viewport, ok := tt.Find(label)
			if !ok {
				t.Fatal("missing scroll list")
			}
			row, ok := tt.Find(rowLabel)
			if !ok || row.H == 0 || row.Y < viewport.Y || row.Y+row.H > viewport.Y+viewport.H+1 {
				t.Fatalf("keyboard selection invisible: %s %+v in %+v", rowLabel, row, viewport)
			}
			tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, -400)
			for i := 0; i < 3; i++ {
				tt.Frame()
			}
			row, _ = tt.Find(rowLabel)
			if row.H > 0 && row.Y+row.H <= viewport.Y+viewport.H {
				t.Fatal("wheel forced back to keyboard selection")
			}
		})
	}
}

func TestFilesShortcutSelectsFiles(t *testing.T) {
	a := alignmentPreview(t)
	a.Workbench = true
	a.Live.Workbench.Tab = "context"
	tt := ui.NewTester(a.View, 900, 640)
	tt.Key(ui.Cmd, ui.KeyP)
	if !a.Workbench || a.Live.Workbench.Tab != "files" {
		t.Fatal("file shortcut opened another panel")
	}
	tt.Key(ui.Cmd, ui.KeyP)
	if a.Workbench {
		t.Fatal("file shortcut failed to close files")
	}
}
