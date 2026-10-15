package main

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}
func TestDraftAndNativeInput(t *testing.T) {
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "消息输入")
	tt.Type("第一行")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("第二行")
	if a.current().Draft != "第一行\n第二行" {
		t.Fatalf("native newline: %q", a.current().Draft)
	}
	click(t, tt, "原生 Markdown 与代码")
	click(t, tt, "消息输入")
	tt.Type("另一个草稿")
	click(t, tt, "青夜界面试验")
	if a.current().Draft != "第一行\n第二行" {
		t.Fatal("draft lost on switch")
	}
	click(t, tt, "消息输入")
	tt.Key(0, ui.KeyEnter)
	if !a.current().Running || a.current().Draft != "" {
		t.Fatal("Enter did not send")
	}
	origin := a.current()
	click(t, tt, "原生 Markdown 与代码")
	a.tick()
	tt.Frame()
	if origin.Position == 0 || a.current().Draft != "另一个草稿" {
		t.Fatal("stream contaminated another session")
	}
	click(t, tt, "青夜界面试验")
	click(t, tt, "停止输出")
	stopped := origin.Messages[len(origin.Messages)-1].Text
	a.tick()
	if origin.Running || origin.Messages[len(origin.Messages)-1].Text != stopped {
		t.Fatal("stop failed")
	}
}
func TestApprovalAndArchive(t *testing.T) {
	for _, allow := range []bool{false, true} {
		a := newPreview()
		tt := ui.NewTester(a.View, 1240, 820)
		click(t, tt, "工具审批演示")
		if allow {
			click(t, tt, "允许一次")
		} else {
			click(t, tt, "拒绝")
		}
		if a.current().Pending {
			t.Fatal("approval still pending")
		}
		if allow {
			for a.current().Running {
				a.tick()
			}
			tt.Frame()
			if !tt.HasText("审批完成。") {
				t.Fatal("approval result missing")
			}
		} else {
			if !tt.HasText("已拒绝。") {
				t.Fatal("rejection missing")
			}
		}
	}
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "会话操作")
	click(t, tt, "归档会话")
	if !a.current().Archived || !a.Archived {
		t.Fatal("archive failed")
	}
	click(t, tt, "消息输入")
	tt.Type("归档不可编辑")
	if a.current().Draft != "" {
		t.Fatal("archived session editable")
	}
	click(t, tt, "会话操作")
	click(t, tt, "恢复会话")
	if a.current().Archived {
		t.Fatal("restore failed")
	}
}
func TestPickerSlashSearchAndFork(t *testing.T) {
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "coding")
	click(t, tt, "搜索模型")
	tt.Type("deepseek")
	click(t, tt, "deepseek-flash")
	if a.current().Model != "deepseek-flash" || a.ModelMenu {
		t.Fatal("model selection failed")
	}
	click(t, tt, "斜杠命令")
	click(t, tt, "/approval  演示工具审批")
	tt.Key(0, ui.KeyEnter)
	if !a.current().Pending {
		t.Fatal("slash command did not enter approval")
	}
	click(t, tt, "拒绝")
	click(t, tt, "搜索会话")
	tt.Type("不存在")
	if !tt.HasText("没有匹配的会话") {
		t.Fatal("search empty state missing")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	click(t, tt, "原生 Markdown 与代码")
	click(t, tt, "复制代码")
	if !strings.Contains(tt.Clipboard(), "ui.Column(c)") {
		t.Fatal("native code copy failed")
	}
	click(t, tt, "从回复 1 创建分支")
	if !strings.HasSuffix(a.current().Title, " · 分支") {
		t.Fatal("fork failed")
	}
	a.current().Messages[1].Text = "分支修改"
	for _, s := range a.Sessions {
		if s.ID == 2 && s.Messages[1].Text == "分支修改" {
			t.Fatal("fork shares message pointers")
		}
	}
	tt.Key(ui.Cmd, ui.KeyN)
	if a.current().Title != "新会话" || len(a.current().Messages) != 0 {
		t.Fatal("new session shortcut failed")
	}
}
func TestIMEDoesNotSubmit(t *testing.T) {
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "消息输入")
	tt.Compose("原生", 2)
	tt.Key(0, ui.KeyEnter)
	if a.current().Running {
		t.Fatal("IME candidate confirmation sent message")
	}
}
func TestPreviewSnapshots(t *testing.T) {
	dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS")
	if dir == "" {
		t.Skip("set EASYAGENT_PREVIEW_SNAPSHOTS to capture visual artifacts")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		name      string
		w, h      float32
		active    int
		workbench bool
	}{{"desktop", 1240, 820, 1, false}, {"compact", 900, 680, 1, false}, {"approval", 1240, 820, 1, false}, {"markdown", 1240, 820, 2, false}, {"workbench", 1240, 820, 1, true}, {"compact-workbench", 900, 680, 1, true}} {
		a := newPreview()
		a.Active = spec.active
		a.Workbench = spec.workbench
		tt := ui.NewTester(a.View, int(spec.w), int(spec.h))
		if spec.name == "compact" {
			click(t, tt, "消息输入")
			tt.Type("/")
		}
		if spec.name == "approval" {
			click(t, tt, "消息输入")
			tt.Type("保留这条未发送的草稿")
			click(t, tt, "会话操作")
			click(t, tt, "演示工具审批")
		}
		f, err := os.Create(filepath.Join(dir, spec.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if r, ok := tt.Find("消息输入"); !ok || r.W < 180 || r.Y+r.H > spec.h {
			t.Fatalf("%s composer overflow: %+v", spec.name, r)
		}
	}
}

func TestTypedSlashCompletionAndApprovalKeepsDraft(t *testing.T) {
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "消息输入")
	tt.Type("/")
	if !a.SlashMenu || !tt.HasText("/approval  演示工具审批") {
		t.Fatal("typed slash did not reveal command")
	}
	tt.Key(0, ui.KeyTab)
	if a.SlashMenu || a.current().Draft != "/approval " || a.current().Pending {
		t.Fatal("Tab should complete, not execute")
	}
	tt.Key(0, ui.KeyEnter)
	if !a.current().Pending {
		t.Fatal("completed command did not execute")
	}
	click(t, tt, "拒绝")
	click(t, tt, "消息输入")
	tt.Type("保留这条未发送的草稿")
	click(t, tt, "会话操作")
	click(t, tt, "演示工具审批")
	if a.current().Draft != "保留这条未发送的草稿" {
		t.Fatal("toolbar consumed draft")
	}
	click(t, tt, "允许一次")
	for a.current().Running {
		a.tick()
	}
	tt.Frame()
	if a.current().Draft != "保留这条未发送的草稿" {
		t.Fatal("approval completion consumed draft")
	}
	click(t, tt, "会话操作")
	click(t, tt, "演示工具审批")
	click(t, tt, "拒绝")
	if a.current().Draft != "保留这条未发送的草稿" {
		t.Fatal("rejection consumed draft")
	}
}

func TestPermissionMenuAlignmentAndKeyboard(t *testing.T) {
	for _, size := range []struct {
		w, h      int
		workbench bool
	}{{1240, 820, true}, {900, 680, false}, {900, 680, true}} {
		a := newPreview()
		a.Workbench = size.workbench
		tt := ui.NewTester(a.View, size.w, size.h)
		anchor, _ := tt.Find("每次询问")
		click(t, tt, "每次询问")
		first, ok := tt.Find("权限：每次询问")
		second, _ := tt.Find("权限：完全权限")
		if !ok || first.W < 210 || first.X != second.X || first.X < anchor.X || first.X > anchor.X+10 || second.Y+second.H > anchor.Y {
			t.Fatalf("menu should align left above trigger with full-width rows: anchor=%+v first=%+v second=%+v", anchor, first, second)
		}
		if !tt.Focused("权限：每次询问") {
			t.Fatal("current permission should receive menu focus")
		}
		tt.Key(0, ui.KeyDown)
		tt.Key(0, ui.KeyEnter)
		if a.Permission != "完全权限" || a.PermissionMenu || !tt.Focused("消息输入") {
			t.Fatal("keyboard selection must close and restore composer focus")
		}
		click(t, tt, "完全权限")
		tt.Key(0, ui.KeyEscape)
		if a.PermissionMenu || !tt.Focused("消息输入") {
			t.Fatalf("Escape focus: menu=%v composer=%v trigger=%v option=%v", a.PermissionMenu, tt.Focused("消息输入"), tt.Focused("完全权限"), tt.Focused("权限：完全权限"))
		}
	}
}
func TestModelMenuEmptyAndMenuDismissal(t *testing.T) {
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "coding")
	if !tt.Focused("搜索模型") {
		t.Fatal("model search should autofocus")
	}
	tt.Type("unknown")
	if !tt.HasText("没有匹配的模型") {
		t.Fatal("model search empty state missing")
	}
	composer, _ := tt.Find("消息输入")
	tt.ClickAt(composer.X+8, composer.Y+8)
	if a.ModelMenu {
		t.Fatal("outside click did not dismiss menu")
	}
	click(t, tt, "每次询问")
	click(t, tt, "coding")
	if a.PermissionMenu || !a.ModelMenu {
		t.Fatal("opening another menu should dismiss previous menu")
	}
}
func TestPermissionSnapshots(t *testing.T) {
	dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS")
	if dir == "" {
		t.Skip("visual capture optional")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		name      string
		w, h      int
		workbench bool
	}{{"permission", 1240, 820, true}, {"permission-compact", 900, 680, true}, {"model-menu", 1240, 820, true}} {
		a := newPreview()
		a.Workbench = spec.workbench
		tt := ui.NewTester(a.View, spec.w, spec.h)
		if spec.name == "model-menu" {
			click(t, tt, "coding")
		} else {
			click(t, tt, "每次询问")
		}
		f, err := os.Create(filepath.Join(dir, spec.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommandsAnchorAboveComposer(t *testing.T) {
	for _, spec := range []struct {
		name            string
		w, h            int
		workbench, long bool
	}{{"slash-input", 1240, 820, true, false}, {"slash-compact", 900, 680, false, false}, {"slash-compact-workbench", 900, 680, true, false}, {"slash-tall-input", 900, 680, true, true}} {
		a := newPreview()
		a.Workbench = spec.workbench
		tt := ui.NewTester(a.View, spec.w, spec.h)
		click(t, tt, "消息输入")
		if spec.long {
			tt.Type(strings.Repeat("这是一条多行草稿。", 50))
			click(t, tt, "斜杠命令")
		} else {
			tt.Type("/")
		}
		frame, _ := tt.Find("输入面板")
		popup, ok := tt.Find("命令菜单")
		if !ok || popup.X != frame.X || popup.Y+popup.H > frame.Y-7 || popup.X+popup.W > frame.X+frame.W || popup.Y < 48 {
			t.Fatalf("%s must align above input without covering it: frame=%+v menu=%+v", spec.name, frame, popup)
		}
		if !spec.long && !tt.Focused("消息输入") {
			t.Fatal("typing slash must keep the input focused")
		}
		before := a.current().Draft
		click(t, tt, "发送消息")
		if !spec.long && (a.current().Running || a.current().Pending || a.current().Draft != before) {
			t.Fatal("partial command must not send")
		}
		tt.Key(0, ui.KeyEscape)
		if a.SlashMenu || !tt.Focused("消息输入") || a.current().Draft != before {
			t.Fatal("Escape should dismiss command menu without losing draft or focus")
		}
		click(t, tt, "斜杠命令")
		popup, _ = tt.Find("命令菜单")
		if popup.Y+popup.H > frame.Y-7 {
			t.Fatal("button-opened commands should use same placement")
		}
		if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
			f, err := os.Create(filepath.Join(dir, spec.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
