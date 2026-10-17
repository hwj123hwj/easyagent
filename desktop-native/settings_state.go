package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

type settingsPage struct {
	ID, Label, Icon, Description string
	Group                        int
}

var settingsPages = []settingsPage{
	{"general", "常规", "settings", "设置界面语言，查看版本与常用快捷键。", 0},
	{"appearance", "外观", "sparkle", "选择适合你的界面配色。", 0},
	{"models", "模型连接", "cpu", "连接模型服务，选择默认模型。", 0},
	{"usage", "用量统计", "activity", "查看近 30 天 Token 消耗与各会话用量排行。", 0},
	{"skillMarket", "技能市场", "download", "浏览并安装官方技能市场里的技能。", 0},
	{"experiments", "实验", "wrench", "实验性功能开关：Computer use 等能力。", 0},
	{"connections", "运行主机", "laptop", "选择任务运行的位置，管理远程 Agent 连接。", 1},
	{"mcp", "MCP 工具", "link", "为当前主机接入工具服务，管理信任与授权。", 1},
	{"capabilities", "技能", "code", "查看当前项目实际加载的能力。", 1},
	{"feishu", "飞书机器人", "feishu", "配置应用凭据、查看桥接状态，并完成私聊配对。", 1},
}

type localPreferences struct {
	Language       string  `json:"language"`
	Theme          string  `json:"theme"`
	WorkbenchWidth float32 `json:"workbench_width,omitempty"`
	TerminalHeight float32 `json:"terminal_height,omitempty"`
}
type demoHost struct {
	ID, Name, URL string
	Local         bool
}
type demoServer struct {
	Name, Scope, Transport, Host, Workspace      string
	Config                                       map[string]any
	Enabled, Trusted, Authorized, Expanded, Tool bool
}
type demoSkill struct {
	Name, Title, Description, Category, Content string
	Installed                                   bool
}
type feishuDemoState struct {
	AppID         string
	Saved, Bridge bool
}
type settingsState struct {
	Open                                                                               bool
	Heading                                                                            ui.Handle
	Tab, Notice, Error                                                                 string
	Preferences                                                                        localPreferences
	Path                                                                               string
	Host                                                                               int
	Hosts                                                                              []demoHost
	EditHost, HostName, HostURL, HostToken                                             string
	Mode, Provider, BaseURL, Model, APIKey                                             string
	Checked, HasKey                                                                    bool
	ComputerEnabled                                                                    bool
	Approval                                                                           string
	Audit                                                                              []string
	Workspace, Scope, MCPName, Transport, Command, Args, Variables, URL, Timeout, JSON string
	Advanced, Trust, ProjectTrust, MCPExtra                                            bool
	MCPBase                                                                            map[string]any
	HostFeishu                                                                         map[string]feishuDemoState
	Servers                                                                            []*demoServer
	Skills                                                                             []*demoSkill
	SkillQuery, SkillFilter, SkillCategory, SkillDetail                                string
	ConfirmSkill, ConfirmServer, ConfirmScope, ConfirmHost, ConfirmWorkspace           string
	ProjectTrustByWorkspace                                                            map[string]bool
	CapabilityOpen                                                                     bool
	AppID, AppSecret                                                                   string
	Bridge, FeishuSaved, Pairing, QR, QRAuthorized                                     bool
}

func newSettings() settingsState {
	return settingsState{
		Tab: "general", HostFeishu: map[string]feishuDemoState{}, ProjectTrustByWorkspace: map[string]bool{}, Preferences: localPreferences{Language: "中文", Theme: "dark"}, Hosts: []demoHost{{"local", localHostName(), "", true}, {"mini", "迷你主机", "http://192.168.5.16:8080", false}}, EditHost: "迷你主机", HostName: "迷你主机", HostURL: "http://192.168.5.16:8080",
		Mode: "沿用命令行配置", Provider: "OpenAI 兼容服务 / 网关", BaseURL: "http://localhost:4001", Model: "coding", Approval: "逐批询问", Workspace: "/demo/easyagent", Scope: "个人", Transport: "stdio", Timeout: "30000",
		Audit: []string{}, Servers: []*demoServer{{Name: "filesystem-demo", Host: "local", Scope: "个人", Transport: "stdio", Enabled: true, Trusted: true, Tool: true}},
		SkillFilter: "精选", SkillCategory: "全部", Skills: []*demoSkill{
			{"frontend-design", "界面设计", "根据现有设计系统实现界面，并验证布局与交互。", "开发", "## 界面设计\n\n这是预览内置的示例技能，不会下载或写入正式技能目录。\n\n- 对照现有界面。\n- 实现交互与响应式布局。\n- 检查可读性和键盘操作。", false},
			{"code-review", "代码审查", "检查修改中的功能、边界与回归风险。", "开发", "## 代码审查\n\n这是示例说明。\n\n阅读修改，验证边界和回归风险，报告具体问题。", true},
			{"project-notes", "项目记录", "组织项目进度、决策与验证证据。", "效率", "## 项目记录\n\n示例技能：整理项目进度与验证证据。", false},
		},
	}
}
func (a *preview) openSettings(tab string) {
	if a.Live != nil && a.Settings.Tab != tab {
		a.cancelSettingsTransient()
	}
	a.Settings.Open = true
	a.Settings.Heading.Focus()
	a.Settings.Tab = tab
	a.Settings.Notice = ""
	a.Settings.Error = ""
	a.ModelMenu = false
	a.PermissionMenu = false
	a.MoreMenu = false
	a.HostMenu = false
	a.SlashMenu = false
	a.NewMenu = false
	if a.Live != nil {
		a.loadSettingsPage()
	}
}
func (a *preview) closeSettings() {
	if a.Live != nil {
		a.cancelSettingsTransient()
	}
	a.Settings.Open = false
	a.Settings.Heading.CancelFocus()
	a.Settings.AppSecret = ""
	a.Settings.APIKey = ""
	a.Settings.HostToken = ""
	a.ReturnToComposer = true
}
func (s *settingsState) page() settingsPage {
	for _, p := range settingsPages {
		if p.ID == s.Tab {
			return p
		}
	}
	return settingsPages[0]
}
func (s *settingsState) host() demoHost       { return s.Hosts[min(s.Host, len(s.Hosts)-1)] }
func (s *settingsState) message(value string) { s.Notice = value; s.Error = "" }
func validServiceURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}
func (s *settingsState) saveHost() {
	if strings.TrimSpace(s.HostName) == "" || !validServiceURL(s.HostURL) {
		s.Error = "请填写名称和有效的 HTTP(S) 服务地址。"
		return
	}
	for i, h := range s.Hosts {
		if !h.Local && h.Name == s.EditHost {
			s.Hosts[i].Name = strings.TrimSpace(s.HostName)
			s.Hosts[i].URL = strings.TrimSpace(s.HostURL)
			s.selectHost(i)
			s.EditHost = s.Hosts[i].Name
			s.HostToken = ""
			s.message("演示连接配置已保存，未连接真实服务。")
			return
		}
	}
	h := demoHost{ID: "preview-" + strconv.Itoa(len(s.Hosts)), Name: strings.TrimSpace(s.HostName), URL: strings.TrimSpace(s.HostURL)}
	s.Hosts = append(s.Hosts, h)
	s.selectHost(len(s.Hosts) - 1)
	s.EditHost = h.Name
	s.HostToken = ""
	s.message("演示连接配置已保存，未连接真实服务。")
}
func (s *settingsState) checkModel() {
	s.Checked = false
	if s.Mode == "在桌面客户端配置" && (!validServiceURL(s.BaseURL) || strings.TrimSpace(s.Model) == "") {
		s.Error = "请填写有效的服务地址和默认模型。"
		return
	}
	s.Checked = true
	s.message("演示检测已完成：显示示例模型目录，没有发出网络请求。")
}
func (s *settingsState) mcpConfig() (map[string]any, error) {
	var config map[string]any
	if s.Advanced {
		if err := json.Unmarshal([]byte(s.JSON), &config); err != nil || config == nil {
			return nil, errors.New("配置须为有效 JSON 对象")
		}
	} else {
		config = map[string]any{}
		for k, v := range s.MCPBase {
			config[k] = v
		}
		for _, key := range []string{"command", "args", "env", "url", "httpUrl", "headers", "timeout"} {
			delete(config, key)
		}
		config["type"] = s.Transport
		config["trust"] = s.Trust
		if s.Transport == "stdio" {
			if strings.TrimSpace(s.Command) == "" {
				return nil, errors.New("请输入启动命令")
			}
			config["command"] = strings.TrimSpace(s.Command)
			var args []string
			for _, line := range strings.Split(s.Args, "\n") {
				if line != "" {
					args = append(args, line)
				}
			}
			config["args"] = args
		} else {
			if !validServiceURL(s.URL) {
				return nil, errors.New("请输入有效 HTTP(S) 地址")
			}
			config["url"] = s.URL
		}
		vars := map[string]string{}
		for _, line := range strings.Split(s.Variables, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(k) == "" {
				return nil, errors.New("每行使用 KEY=VALUE 格式")
			}
			vars[strings.TrimSpace(k)] = v
		}
		if len(vars) > 0 {
			key := "env"
			if s.Transport != "stdio" {
				key = "headers"
			}
			config[key] = vars
		}
		if s.Timeout != "" {
			n, err := strconv.Atoi(s.Timeout)
			if err != nil || n < 0 {
				return nil, errors.New("超时须为非负整数毫秒")
			}
			config["timeout"] = n
		}
	}
	if command, ok := config["command"].(string); ok && strings.TrimSpace(command) != "" {
		return config, nil
	}
	if raw, ok := config["url"].(string); ok && validServiceURL(raw) {
		return config, nil
	}
	return nil, errors.New("配置需要有效的 command 或 HTTP(S) url")
}
func (s *settingsState) saveMCP() {
	if strings.TrimSpace(s.MCPName) == "" {
		s.Error = "请输入服务名称"
		return
	}
	if s.Scope == "项目" && strings.TrimSpace(s.Workspace) == "" {
		s.Error = "项目配置需要工作区路径"
		return
	}
	config, err := s.mcpConfig()
	if err != nil {
		s.Error = err.Error()
		return
	}
	transport := "stdio"
	if _, ok := config["url"]; ok {
		transport = "http"
	}
	trusted, _ := config["trust"].(bool)
	for _, server := range s.Servers {
		if server.Name == strings.TrimSpace(s.MCPName) && server.Scope == s.Scope && server.Host == s.host().ID && (s.Scope != "项目" || server.Workspace == s.Workspace) {
			server.Config = config
			server.Transport = transport
			server.Trusted = trusted
			server.Enabled = true
			s.message("MCP 演示配置已保存，未启动工具服务。")
			return
		}
	}
	s.Servers = append(s.Servers, &demoServer{Name: strings.TrimSpace(s.MCPName), Host: s.host().ID, Config: config, Workspace: s.Workspace, Scope: s.Scope, Transport: transport, Enabled: true, Trusted: trusted, Tool: true})
	s.JSON = ""
	s.Variables = ""
	s.message("MCP 演示配置已保存，未启动工具服务。")
}
func (s *settingsState) loadPreferences(path string) {
	s.Path = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var prefs localPreferences
	if json.Unmarshal(b, &prefs) == nil && (prefs.Theme == "dark" || prefs.Theme == "light" || prefs.Theme == "system") && (prefs.Language == "中文" || prefs.Language == "English") {
		s.Preferences = prefs
	}
}
func (s *settingsState) savePreferences() {
	if s.Path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		s.Error = err.Error()
		return
	}
	b, _ := json.MarshalIndent(s.Preferences, "", "  ")
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		s.Error = err.Error()
		return
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		s.Error = err.Error()
	}
}
func previewTheme(base *ui.Theme, prefs localPreferences) ui.Theme {
	t := *base
	dark := prefs.Theme == "dark" || (prefs.Theme == "system" && base.Dark)
	t.Dark = dark
	t.Radius = 6
	if dark {
		t.Background = bg
		t.Surface = elevated
		t.SurfaceHover = raised
		t.SurfacePressed = raised
		t.Text = ink
		t.TextMuted = muted
		t.Border = line
		t.Accent = accent
		t.AccentText = ui.Hex("#081a17")
		t.AccentHover = ui.Hex("#6ee7b7")
	} else {
		t.Background = ui.Hex("#f8fbfa")
		t.Surface = ui.Hex("#eef5f2")
		t.SurfaceHover = ui.Hex("#e8f1ed")
		t.SurfacePressed = ui.Hex("#e1eee8")
		t.Text = ui.Hex("#16342e")
		t.TextMuted = ui.Hex("#385c50")
		t.Border = ui.Hex("#cadbd3")
		t.Accent = ui.Hex("#087f78")
		t.AccentText = ui.Hex("#ffffff")
		t.AccentHover = ui.Hex("#09685f")
	}
	t.Selection = t.Accent.Alpha(.24)
	t.Focus = t.Accent.Alpha(.4)
	return t
}

func (s *settingsState) selectHost(index int) {
	if index == s.Host {
		return
	}
	s.HostFeishu[s.host().ID] = feishuDemoState{s.AppID, s.FeishuSaved, s.Bridge}
	s.Host = index
	saved := s.HostFeishu[s.host().ID]
	s.AppID, s.FeishuSaved, s.Bridge = saved.AppID, saved.Saved, saved.Bridge
	s.AppSecret, s.APIKey, s.MCPName, s.JSON, s.Variables = "", "", "", "", ""
	s.Pairing, s.QR, s.QRAuthorized, s.ProjectTrust = false, false, false, false
	s.MCPBase = nil
	s.message("已切换演示主机，没有连接真实服务。")
}

func composerSurface(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return ui.Hex("#1a2b29")
	}
	return ui.Hex("#e7efeb")
}

func searchSurface(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return sunken
	}
	return ui.Hex("#e7efeb")
}
