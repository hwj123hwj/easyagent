package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func alignmentPreview(t *testing.T) *preview {
	t.Helper()
	a := newConnectedPreview(t.TempDir())
	t.Cleanup(a.closeLive)
	s := a.current()
	s.RemoteID = "layout"
	s.Title = "当前会话"
	s.Workspace = "/demo/easyagent"
	s.Project = "easyagent"
	s.Model = "coding"
	s.Messages = []*message{{User: true, Text: "对齐阅读列与输入框。"}, {Text: "这是原生回复正文。"}}
	a.Live.Models = []string{"coding", "glm-5.3-flash"}
	return a
}
func TestNativeAlignedReadingColumnAndSettingsAccess(t *testing.T) {
	for _, size := range []struct {
		w, h      int
		workbench bool
	}{{1240, 820, false}, {1240, 820, true}, {900, 680, false}, {900, 680, true}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a := alignmentPreview(t)
			a.Workbench = size.workbench
			tt := ui.NewTester(a.View, size.w, size.h)
			author, ok := tt.Find("回复作者")
			if !ok {
				t.Fatal("missing reply author")
			}
			input, ok := tt.Find("输入面板")
			if !ok || author.X != input.X {
				t.Fatalf("reading column and composer drift: author=%+v input=%+v", author, input)
			}
			if _, ok := tt.Find("设置"); !ok {
				t.Fatal("settings unreachable with sidebar hidden")
			}
			a.Sidebar = false
			tt.Frame()
			click(t, tt, "设置")
			if !a.Settings.Open {
				t.Fatal("collapsed sidebar lost settings action")
			}
		})
	}
}
func TestNativeProjectCollapseAndSuggestionsPreserveDraft(t *testing.T) {
	a := alignmentPreview(t)
	a.Sessions = append(a.Sessions, &session{ID: 8, Title: "同项目另一会话", Workspace: "/demo/easyagent", Project: "easyagent"})
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "折叠项目：easyagent")
	if _, ok := tt.Find("同项目另一会话"); ok {
		t.Fatal("collapsed project still lists sessions")
	}
	click(t, tt, "折叠项目：easyagent")
	if _, ok := tt.Find("同项目另一会话"); !ok {
		t.Fatal("project cannot reopen")
	}
	a.current().Messages = nil
	a.current().Draft = "保留我的草稿"
	tt.Frame()
	for _, label := range []string{"梳理一个想法", "一起看看代码", "排查一个问题"} {
		count := 0
		for _, text := range tt.Texts() {
			if text == label {
				count++
			}
		}
		// Tester.Texts includes the button accessibility label and its one text child.
		if count != 2 {
			t.Fatalf("duplicated suggestion %q: %d", label, count)
		}
	}
	click(t, tt, "一起看看代码")
	if a.current().Draft != "保留我的草稿" {
		t.Fatal("suggestion overwrote an existing draft")
	}
}
func TestNativeLiveCommandPanelTracksComposer(t *testing.T) {
	for _, width := range []int{1240, 900} {
		a := alignmentPreview(t)
		a.Workbench = true
		tt := ui.NewTester(a.View, width, 820)
		click(t, tt, "消息输入")
		tt.Type("/")
		tt.Frame()
		input, _ := tt.Find("输入面板")
		panel, ok := tt.Find("命令菜单")
		if !ok || panel.X != input.X || panel.W != input.W || panel.Y+panel.H > input.Y-7 {
			t.Fatalf("command popup drift: input=%+v panel=%+v", input, panel)
		}
		tt.Key(0, ui.KeyDown)
		tt.Key(0, ui.KeyTab)
		if a.SlashMenu || a.current().Draft != "/new " {
			t.Fatal("native slash keyboard completion failed")
		}
		if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
			saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("live-command-%d.png", width)))
		}
	}
}
func TestNativeWorkbenchRailKeepsTerminalIndependent(t *testing.T) {
	a := alignmentPreview(t)
	a.Workbench = true
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "审查")
	if a.Live.Workbench.Tab != "review" {
		t.Fatal("review rail action missing")
	}
	a.Live.Workbench.TerminalOpen = true
	a.Live.Workbench.TerminalHost = "local"
	a.Live.Workbench.TerminalSession = "layout"
	a.Live.Workbench.TerminalWorkspace = "/demo/easyagent"
	a.Workbench = false
	tt.Frame()
	if !tt.HasText("终端 · easyagent") {
		t.Fatal("terminal still depends on right workbench")
	}
	click(t, tt, "关闭终端")
	if a.Live.Workbench.TerminalOpen {
		t.Fatal("terminal close failed")
	}
}

func TestNativeResizePersistsAndDoesNotReplaceDraft(t *testing.T) {
	a := alignmentPreview(t)
	a.Workbench = true
	a.current().Draft = "resize draft"
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "调整工作台宽度")
	tt.Key(0, ui.KeyLeft)
	if a.Settings.Preferences.WorkbenchWidth != 450 || a.current().Draft != "resize draft" {
		t.Fatal("workbench resize or draft preservation failed")
	}
	recovered := newSettings()
	recovered.loadPreferences(a.Settings.Path)
	if recovered.Preferences.WorkbenchWidth != 450 {
		t.Fatal("workbench width not saved")
	}
}

func TestNativeLiveScreenStates(t *testing.T) {
	dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS")
	if dir == "" {
		t.Skip("set EASYAGENT_PREVIEW_SNAPSHOTS to inspect live UI")
	}
	for _, theme := range []string{"dark", "light"} {
		for _, mode := range []string{"chat", "empty", "workbench", "commands", "permissions", "models", "market"} {
			a := alignmentPreview(t)
			a.Settings.Preferences.Theme = theme
			if mode == "empty" {
				a.current().Messages = nil
			}
			if mode == "workbench" {
				a.Workbench = true
			}
			if mode == "market" {
				a.Live.Data["market"] = map[string]any{"skills": []any{
					map[string]any{"id": "one", "name": "native-ui", "display_name": "原生界面设计", "description": "这是一段用来检查长描述换行和截断的技能介绍。详情页仍保留完整说明，列表内保持两行摘要。", "version": "1.0.0"},
					map[string]any{"id": "two", "name": "review", "display_name": "代码审查", "description": "检查功能、边界与回归风险，保留已有代码和配置。", "version": "2.0.0"},
				}}
				a.openSettings("skillMarket")
			}
			tt := ui.NewTester(a.View, 1240, 820)
			if mode == "commands" {
				click(t, tt, "消息输入")
				tt.Type("/")
				tt.Frame()
			}
			if mode == "permissions" {
				click(t, tt, "每次询问")
			}
			if mode == "models" {
				click(t, tt, "coding")
			}
			saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("native-%s-%s.png", mode, theme)))
		}
	}
}
