package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsNavigationKeepsConversation(t *testing.T) {
	a := newPreview()
	a.current().Draft = "保留草稿"
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "设置")
	for _, page := range settingsPages {
		click(t, tt, page.Label)
		if !a.Settings.Open || a.Settings.Tab != page.ID || !tt.HasText(page.Description) {
			t.Fatalf("page %s missing", page.ID)
		}
	}
	click(t, tt, "返回工作区")
	if a.Settings.Open || a.current().Draft != "保留草稿" || !tt.Focused("消息输入") {
		t.Fatal("return lost draft or focus")
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	if !a.Settings.Open {
		t.Fatal("settings shortcut missing")
	}
}
func TestSettingsPreferencePersistenceAndThemes(t *testing.T) {
	a := newPreview()
	s := &a.Settings
	s.Path = filepath.Join(t.TempDir(), "preferences.json")
	s.APIKey = "secret-not-to-save"
	s.HostToken = "host-secret"
	s.AppSecret = "bot-secret"
	a.openSettings("appearance")
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "浅色")
	if s.Preferences.Theme != "light" {
		t.Fatal("theme selection failed")
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "API") {
		t.Fatal("preferences leaked secret")
	}
	recovered := newSettings()
	recovered.loadPreferences(s.Path)
	if recovered.Preferences.Theme != "light" {
		t.Fatal("preferences not restored")
	}
	click(t, tt, "跟随系统")
	tt.SetDark(false)
	if theme := previewTheme(ui.LightTheme(), s.Preferences); theme.Dark {
		t.Fatal("system light ignored")
	}
	tt.SetDark(true)
	if theme := previewTheme(ui.DarkTheme(), s.Preferences); !theme.Dark {
		t.Fatal("system dark ignored")
	}
	a.openSettings("general")
	tt.Frame()
	click(t, tt, "语言选择")
	click(t, tt, "English")
	if !tt.HasText("General") || !tt.HasText("Back to workspace") {
		t.Fatal("language not applied")
	}
	a.closeSettings()
	tt.Frame()
	if s.APIKey != "" || s.AppSecret != "" || s.HostToken != "" {
		t.Fatal("leaving settings retained credentials")
	}
}
func TestSettingsModelHostAndMCPValidation(t *testing.T) {
	a := newPreview()
	s := &a.Settings
	s.EditHost = "新增连接"
	s.HostName = "test"
	s.HostURL = "file:///tmp"
	s.saveHost()
	if s.Error == "" || len(s.Hosts) != 2 {
		t.Fatal("invalid service URL accepted")
	}
	s.HostURL = "http://localhost:8080"
	s.saveHost()
	if len(s.Hosts) != 3 || s.HostToken != "" {
		t.Fatal("demo host save failed")
	}
	s.selectHost(0)
	s.Mode = "在桌面客户端配置"
	s.BaseURL = "https://user:secret@host"
	s.checkModel()
	if s.Checked {
		t.Fatal("credential URL accepted")
	}
	s.BaseURL = "http://localhost:4001"
	s.checkModel()
	if !s.Checked {
		t.Fatal("valid demo model refused")
	}
	s.MCPName = "demo"
	s.Advanced = true
	s.JSON = "[]"
	s.saveMCP()
	if s.Error == "" || len(s.Servers) != 1 {
		t.Fatal("MCP array accepted")
	}
	s.JSON = `{"command":"demo","args":["one"],"custom":"keep","env":{"NAME":"value"},"timeout":2000}`
	s.toggleAdvanced()
	if s.Advanced || s.Command != "demo" || s.Args != "one" {
		t.Fatal("JSON -> form lost values")
	}
	config, err := s.mcpConfig()
	if err != nil || config["custom"] != "keep" {
		t.Fatal("form round trip lost extra config")
	}
	s.saveMCP()
	if len(s.Servers) != 2 || s.Servers[1].Host != "local" {
		t.Fatal("MCP not scoped to host")
	}
	s.selectHost(1)
	s.AppID = "remote-id"
	s.FeishuSaved = true
	s.selectHost(0)
	if s.FeishuSaved || s.AppID != "" {
		t.Fatal("Feishu leaked across hosts")
	}
	s.selectHost(1)
	if s.AppID != "remote-id" || !s.FeishuSaved {
		t.Fatal("host-local Feishu state lost")
	}
}
func TestSettingsSkillFlow(t *testing.T) {
	a := newPreview()
	a.openSettings("skillMarket")
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "查看技能：frontend-design")
	click(t, tt, "安装技能")
	if !a.Settings.Skills[0].Installed {
		t.Fatal("demo install failed")
	}
	click(t, tt, "卸载")
	click(t, tt, "取消")
	if !a.Settings.Skills[0].Installed {
		t.Fatal("cancel uninstalled skill")
	}
	click(t, tt, "卸载")
	click(t, tt, "确认卸载")
	if a.Settings.Skills[0].Installed {
		t.Fatal("confirm uninstall failed")
	}
}
func TestHeaderAlignment(t *testing.T) {
	a := newPreview()
	tt := ui.NewTester(a.View, 1240, 820)
	icon, _ := tt.Find("切换会话侧栏")
	mark, _ := tt.Find("品牌标记")
	name, _ := tt.Find("应用名称")
	sep, _ := tt.Find("标题栏分隔线")
	centers := []float32{icon.Y + icon.H/2, mark.Y + mark.H/2, name.Y + name.H/2, sep.Y + sep.H/2}
	for _, center := range centers {
		if center < 23 || center > 25 {
			t.Fatalf("titlebar controls not on same center: %v", centers)
		}
	}
}
func TestSettingsSnapshots(t *testing.T) {
	dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS")
	if dir == "" {
		t.Skip("visual capture optional")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, size := range []struct {
		name string
		w, h int
	}{{"desktop", 1240, 820}, {"compact", 900, 680}} {
		for _, page := range settingsPages {
			a := newPreview()
			a.openSettings(page.ID)
			if page.ID == "models" {
				a.Settings.Mode = "在桌面客户端配置"
			}
			tt := ui.NewTester(a.View, size.w, size.h)
			if !tt.HasText(page.Description) {
				t.Fatal("page content missing")
			}
			saveSnapshot(t, tt, filepath.Join(dir, "settings-"+page.ID+"-"+size.name+".png"))
		}
	}
	a := newPreview()
	a.openSettings("appearance")
	a.Settings.Preferences.Theme = "light"
	tt := ui.NewTester(a.View, 1240, 820)
	saveSnapshot(t, tt, filepath.Join(dir, "settings-light.png"))
	a.closeSettings()
	tt.Frame()
	saveSnapshot(t, tt, filepath.Join(dir, "chat-light.png"))
	a.openSettings("general")
	tt.Frame()
	click(t, tt, "语言选择")
	click(t, tt, "English")
	saveSnapshot(t, tt, filepath.Join(dir, "settings-english.png"))
}
func saveSnapshot(t *testing.T, tt *ui.Tester, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, tt.Image())
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestMCPEditRetainsSavedConfiguration(t *testing.T) {
	s := newSettings()
	s.MCPName = "custom"
	s.Command = "my-command"
	s.Args = "first\nsecond"
	s.Timeout = "1500"
	s.MCPBase = map[string]any{"custom": "keep"}
	s.saveMCP()
	server := s.Servers[len(s.Servers)-1]
	s.Command = ""
	s.Args = ""
	s.MCPBase = nil
	s.loadServer(server)
	if s.Command != "my-command" || s.Args != "first\nsecond" || s.MCPBase["custom"] != "keep" {
		t.Fatal("editing replaced saved configuration")
	}
	s.Scope = "项目"
	s.Workspace = "/first"
	s.saveMCP()
	s.Workspace = "/second"
	s.saveMCP()
	if len(s.Servers) != 4 {
		t.Fatal("workspace-specific configurations overwrote each other")
	}
}

func TestMCPDeleteKeepsFullIdentity(t *testing.T) {
	a := newPreview()
	s := &a.Settings
	s.Servers = []*demoServer{
		{Name: "same", Scope: "项目", Host: "local", Workspace: "/first"},
		{Name: "same", Scope: "项目", Host: "local", Workspace: "/second"},
		{Name: "same", Scope: "项目", Host: "mini", Workspace: "/second"},
	}
	s.Workspace = "/second"
	a.openSettings("mcp")
	tt := ui.NewTester(a.View, 1240, 1000)
	click(t, tt, "工具服务：same")
	click(t, tt, "删除")
	if s.ConfirmHost != "local" || s.ConfirmWorkspace != "/second" {
		t.Fatal("delete button did not capture full server identity")
	}
	s.Host = 1
	s.Workspace = "/first"
	tt.Frame()
	click(t, tt, "确认删除")
	if len(s.Servers) != 2 || s.Servers[0].Workspace != "/first" || s.Servers[1].Host != "mini" {
		t.Fatal("deletion did not preserve full confirmed identity")
	}
}
func TestEnglishFeishuStatus(t *testing.T) {
	a := newPreview()
	a.Settings.Preferences.Language = "English"
	a.openSettings("feishu")
	tt := ui.NewTester(a.View, 1240, 1000)
	if !tt.HasText("Status: Stopped") {
		t.Fatal("composed Feishu status not translated")
	}
	if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
		saveSnapshot(t, tt, filepath.Join(dir, "settings-feishu-english.png"))
	}
}
