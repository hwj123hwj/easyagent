package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *preview) mcpSettings(c *ui.Context) {
	s := &a.Settings
	projectKey := s.host().ID + "\x00" + s.Workspace
	s.ProjectTrust = s.ProjectTrustByWorkspace[projectKey]
	ui.Row(c).Children(func() {
		a.heading(c, "MCP 工具连接")
		ui.Spacer(c)
		a.action(c, "刷新").OnClick(func() { s.message("演示工具目录已刷新。") })
	})
	a.note(c, "连接配置属于当前服务主机。项目配置需显式信任后才可执行。")
	ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.input(c, "项目工作区", &s.Workspace, "服务主机上的绝对路径", false) })
		a.action(c, "读取").OnClick(func() { s.message("已读取演示工作区，没有访问文件系统。") })
	})
	ui.Row(c).Padding(10, 0).Gap(12).Children(func() {
		label := "项目配置 · 未信任"
		if s.ProjectTrust {
			label = "项目配置 · 已信任"
		}
		a.text(c, label).FontSize(12).Grow(1)
		button := "信任此项目"
		if s.ProjectTrust {
			button = "撤销信任"
		}
		a.action(c, button).Disabled(strings.TrimSpace(s.Workspace) == "").OnClick(func() {
			s.ProjectTrust = !s.ProjectTrust
			s.ProjectTrustByWorkspace[projectKey] = s.ProjectTrust
			s.message("仅更新演示项目的信任状态。")
		})
	})
	count := 0
	for _, server := range s.Servers {
		if server.Host != s.host().ID || (server.Scope == "项目" && server.Workspace != s.Workspace) {
			continue
		}
		count++
		ui.Column(c.Key(server.Scope+server.Name)).Gap(10).Padding(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(8).Children(func() {
			e := ui.ButtonBase(c).Label("工具服务：" + server.Name).Justify(ui.Start).Gap(8).OnClick(func() { server.Expanded = !server.Expanded })
			e.Children(func() {
				name := "chevron-right"
				if server.Expanded {
					name = "chevron-down"
				}
				icon(c, name)
				ui.Text(c, server.Name).FontSize(13).FontWeight(600).Grow(1).MinWidth(0).MaxLines(1)
				ui.Text(c, server.Scope+" · "+server.Transport).FontSize(11).TextColor(c.Theme().TextMuted)
				state := "已停用"
				if server.Enabled {
					state = "演示就绪"
				}
				a.text(c, state).FontSize(11).TextColor(c.Theme().Accent)
			})
			if server.Expanded {
				ui.Row(c).Gap(6).Wrap().Children(func() {
					a.action(c, "修改配置").OnClick(func() {
						s.loadServer(server)
						s.message("已载入演示配置。")
					})
					for _, label := range []string{"重新连接", "刷新工具"} {
						a.action(c, label).OnClick(func() { s.message("演示工具状态已更新，未执行真实连接。") })
					}
					enabled := "启用"
					if server.Enabled {
						enabled = "停用"
					}
					a.action(c, enabled).OnClick(func() { server.Enabled = !server.Enabled })
					trusted := "信任服务"
					if server.Trusted {
						trusted = "撤销信任"
					}
					a.action(c, trusted).OnClick(func() { server.Trusted = !server.Trusted })
					if server.Transport != "stdio" {
						authorized := "授权登录"
						if server.Authorized {
							authorized = "退出授权"
						}
						a.action(c, authorized).OnClick(func() {
							server.Authorized = !server.Authorized
							s.message("仅切换演示授权状态，没有发起真实登录。")
						})
					}
					a.action(c, "删除").TextColor(c.Theme().Danger).OnClick(func() { s.confirmServerDelete(server) })
				})
				ui.Row(c).Children(func() {
					ui.Text(c, "demo_read").Font("monospace").FontSize(12).Grow(1)
					ui.Checkbox(c, &server.Tool, a.tr("启用工具"))
				})
				a.note(c, "示例只读工具，用于检查工具开关和配置交互。")
			}
		})
	}
	if count == 0 {
		a.heading(c, "接入你的第一个工具服务")
		a.note(c, "下方填写名称与标准 MCP 配置，可使用本机命令或远程服务。")
	}
	a.heading(c, "添加或修改 MCP 服务")
	a.dropdown(c, "配置范围", &s.Scope, []string{"个人", "项目"})
	a.input(c, "服务名称", &s.MCPName, "例如 filesystem", false)
	ui.Row(c).Children(func() {
		title := "工具连接配置"
		button := "高级 JSON"
		if s.Advanced {
			title = "高级 JSON"
			button = "返回表单"
		}
		a.text(c, title).FontSize(13).FontWeight(600)
		ui.Spacer(c)
		a.action(c, button).OnClick(s.toggleAdvanced)
	})
	if s.Advanced {
		a.multiline(c, "标准 MCP 配置", &s.JSON, 6, 12)
	} else {
		a.dropdown(c, "连接方式", &s.Transport, []string{"stdio", "http"})
		if s.Transport == "stdio" {
			a.input(c, "启动命令", &s.Command, "npx", false)
			a.multiline(c, "命令参数 · 每行一个", &s.Args, 3, 6)
		} else {
			a.input(c, "工具服务地址", &s.URL, "https://example.com/mcp", false)
		}
		e := flatButton(c, "chevron-right", a.tr("环境变量 / 请求头与超时")).Justify(ui.Start).OnClick(func() { s.MCPExtra = !s.MCPExtra })
		if s.MCPExtra {
			e.TextColor(c.Theme().Accent)
			a.multiline(c, "每行 KEY=VALUE", &s.Variables, 3, 6)
			a.input(c, "超时（毫秒）", &s.Timeout, "30000", false)
		}
		ui.Checkbox(c, &s.Trust, a.tr("信任此服务"))
	}
	a.note(c, "保存演示配置不会自动信任项目，不会执行命令或创建网络连接。")
	a.primary(c, "保存配置").Disabled(strings.TrimSpace(s.MCPName) == "").OnClick(s.saveMCP)
	confirm := s.ConfirmServer != ""
	ui.Modal(c, &confirm, func() {
		a.text(c, "删除演示工具服务？").FontSize(18).FontWeight(600)
		ui.Text(c, s.ConfirmServer).Font("monospace")
		a.note(c, "只删除当前预览的配置，正式 MCP 服务不受影响。")
		ui.Row(c).Gap(10).Children(func() {
			a.action(c, "取消").OnClick(func() { s.ConfirmServer = "" })
			a.primary(c, "确认删除").OnClick(s.deleteServer)
		})
	})
	if !confirm {
		s.ConfirmServer = ""
	}
}
func (s *settingsState) toggleAdvanced() {
	if !s.Advanced {
		config, err := s.mcpConfig()
		if err != nil {
			config = map[string]any{}
		}
		b, _ := json.MarshalIndent(config, "", "  ")
		s.JSON = string(b)
		s.Advanced = true
		s.Error = ""
		return
	}
	var config map[string]any
	if json.Unmarshal([]byte(s.JSON), &config) != nil || config == nil {
		s.Error = "配置须为有效 JSON 对象"
		return
	}
	s.MCPBase = config
	s.Command, _ = config["command"].(string)
	s.URL, _ = config["url"].(string)
	if s.URL == "" {
		s.URL, _ = config["httpUrl"].(string)
	}
	s.Transport = "stdio"
	if s.URL != "" {
		s.Transport = "http"
	}
	s.Trust, _ = config["trust"].(bool)
	s.Args = ""
	if args, ok := config["args"].([]any); ok {
		for _, value := range args {
			str, ok := value.(string)
			if !ok {
				s.Error = "命令参数须为字符串数组"
				return
			}
			if s.Args != "" {
				s.Args += "\n"
			}
			s.Args += str
		}
	}
	s.Variables = ""
	key := "env"
	if s.Transport != "stdio" {
		key = "headers"
	}
	if pairs, ok := config[key].(map[string]any); ok {
		for k, v := range pairs {
			s.Variables += k + "=" + fmt.Sprint(v) + "\n"
		}
	}
	s.Timeout = ""
	if value, ok := config["timeout"].(float64); ok {
		s.Timeout = fmt.Sprintf("%.0f", value)
	}
	s.Advanced = false
	s.Error = ""
}
func (a *preview) capabilitySettings(c *ui.Context) {
	s := &a.Settings
	ui.Row(c).Children(func() {
		a.heading(c, "当前会话的能力")
		ui.Spacer(c)
		a.action(c, "刷新").OnClick(func() { s.message("示例能力目录已刷新。") })
	})
	ui.Text(c, a.tr("项目")+"："+s.Workspace).FontSize(13)
	a.note(c, "这里展示预览会话的示例能力。正式客户端按项目与个人 .agents/skills 加载。")
	a.heading(c, "已加载技能 · 1")
	ui.Column(c).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Padding(12, 0).Children(func() {
		flatButton(c, "chevron-right", "code-review").Justify(ui.Start).TextColor(c.Theme().Text).OnClick(func() { s.CapabilityOpen = !s.CapabilityOpen })
		a.note(c, "项目 · 可由模型调用")
		if s.CapabilityOpen {
			a.note(c, "检查当前修改的功能与回归风险。")
			ui.Text(c, ".agents/skills/code-review/SKILL.md").Font("monospace").FontSize(12).Selectable()
		}
	})
	a.heading(c, "工作流")
	a.note(c, "入口 workflow，使用 create_workflow / get_workflow / resume_workflow 创建、查看与恢复。")
	a.note(c, "可用工具：create_workflow · get_workflow · resume_workflow（演示）")
	ui.Column(c).Gap(10).Padding(12, 0).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		flatButton(c, "chevron-right", a.tr("界面验收工作流")).Justify(ui.Start).TextColor(c.Theme().Text).OnClick(func() { s.WorkflowOpen = !s.WorkflowOpen })
		a.note(c, "已完成 · 演示记录")
		if s.WorkflowOpen {
			ui.Text(c, "workflow-demo-001").Font("monospace").FontSize(12).Selectable()
			a.note(c, "结果：布局、草稿、命令菜单与审批交互已验证。")
			a.action(c, "在对话中查看").OnClick(func() {
				session := a.current()
				if session.Draft != "" {
					session.Draft += "\n"
				}
				session.Draft += "请用 get_workflow 查看工作流 workflow-demo-001 的状态和结果，不要新建流程。"
				a.closeSettings()
			})
		}
	})
}
func (a *preview) feishuSettings(c *ui.Context) {
	s := &a.Settings
	ui.Column(c).Gap(8).Padding(18, 0).BorderWidth(1, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		a.text(c, "配置所在主机").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Children(func() {
			ui.Text(c, s.host().Name).FontSize(16).FontWeight(500)
			ui.Spacer(c)
			a.action(c, "刷新状态").OnClick(func() { s.message("飞书演示状态已刷新。") })
		})
		a.note(c, "配置与配对均针对这台主机，消息由这里的 Agent 处理。")
	})
	if s.host().Local {
		a.heading(c, "快速绑定（扫码登录）")
		a.note(c, "使用飞书手机客户端扫码授权，保存后启动飞书桥接。此预览未接入授权服务。")
		if !s.QR {
			a.primary(c, "扫码绑定飞书机器人").OnClick(func() {
				s.QR = true
				s.QRAuthorized = false
				s.Error = "预览未连接授权服务，没有可用的真实二维码。"
			})
		} else {
			ui.Box(c).Size(192, 192).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(6).Center().Children(func() {
				ui.Column(c).Padding(14).Gap(12).AlignItems(ui.Center).Children(func() {
					icon(c, "link")
					a.text(c, "尚未生成授权二维码").FontSize(12).TextColor(c.Theme().TextMuted)
				})
			})
			ui.Row(c).Gap(8).Wrap().Children(func() {
				a.action(c, "取消扫码").OnClick(func() { s.QR = false; s.QRAuthorized = false; s.Error = "" })
				a.action(c, "演示扫码确认").OnClick(func() {
					s.QRAuthorized = true
					s.Error = ""
					s.message("已进入模拟授权完成状态，没有进行真实授权。")
				})
			})
			if s.QRAuthorized {
				a.primary(c, "确认保存授权凭据").OnClick(func() {
					s.QR = false
					s.FeishuSaved = true
					s.AppID = "cli_preview_demo"
					s.message("演示凭据已确认，真实授权尚未接入。")
				})
			}
		}
		a.heading(c, "飞书桥接（本机）")
		status := "已停止"
		if s.Bridge {
			status = "运行中（演示）"
		}
		a.note(c, a.tr("当前状态：")+a.tr(status))
		action := "启动飞书桥接"
		if s.Bridge {
			action = "停止飞书桥接"
		}
		a.action(c, action).Disabled(!s.FeishuSaved).OnClick(func() {
			s.Bridge = !s.Bridge
			s.message("仅切换演示桥接状态，未启动或停止实际进程。")
		})
	} else {
		a.heading(c, "服务状态")
		ui.Grid(c).Columns(3).Gap(20).Children(func() {
			for _, service := range []string{"Agent", "飞书桥接", "服务托管"} {
				ui.Column(c).Gap(6).Children(func() {
					a.text(c, service).FontSize(12).TextColor(c.Theme().TextMuted)
					a.text(c, "未连接 · 演示").FontSize(13)
				})
			}
		})
		a.note(c, "开机后台运行（linger）：未启用 · 演示")
	}
	a.heading(c, "自建应用凭据（高级）")
	a.input(c, "App ID", &s.AppID, "cli_…", false).ReadOnly(s.FeishuSaved)
	a.input(c, "App Secret", &s.AppSecret, "演示输入（不保存）", true)
	a.note(c, "此预览不保存真实密钥，仅用于检查表单和状态变化。")
	a.primary(c, "保存并重启桥接").Disabled(strings.TrimSpace(s.AppID) == "" || (!s.FeishuSaved && strings.TrimSpace(s.AppSecret) == "")).OnClick(func() {
		s.FeishuSaved = true
		s.AppSecret = ""
		s.Bridge = true
		s.message("演示配置已应用，没有保存密钥或重启真实桥接。")
	})
	a.heading(c, "使用者配对")
	if s.Pairing {
		a.text(c, "私聊机器人发送以下指令").FontSize(12)
		command := "/pair PREVIEW-ONLY"
		ui.Row(c).Gap(10).Children(func() {
			ui.TextInput(c, &command).ReadOnly(true).Label("私聊配对指令").Grow(1).Font("monospace")
			a.action(c, "复制").OnClick(func() {
				c.WriteClipboard(command)
				s.message("演示配对指令已复制，不能用于真实机器人。")
			})
		})
		a.note(c, "这是无效的演示配对码，不会绑定账号。")
		a.action(c, "隐藏指令").OnClick(func() { s.Pairing = false })
	} else {
		a.note(c, "尚未配对")
		a.action(c, "查看私聊配对指令").Disabled(!s.FeishuSaved).OnClick(func() { s.Pairing = true })
	}
}

func (s *settingsState) loadServer(server *demoServer) {
	s.MCPName, s.Scope = server.Name, server.Scope
	config := map[string]any{}
	for k, v := range server.Config {
		config[k] = v
	}
	if len(config) == 0 {
		config["type"] = server.Transport
		if server.Transport == "stdio" {
			config["command"] = "demo-command"
		} else {
			config["url"] = "https://example.invalid/mcp"
		}
	}
	config["trust"] = server.Trusted
	b, _ := json.MarshalIndent(config, "", "  ")
	s.JSON = string(b)
	s.Advanced = true
	s.toggleAdvanced()
}

func (s *settingsState) confirmServerDelete(server *demoServer) {
	s.ConfirmServer, s.ConfirmScope, s.ConfirmHost, s.ConfirmWorkspace = server.Name, server.Scope, server.Host, server.Workspace
}
func (s *settingsState) deleteServer() {
	for i, server := range s.Servers {
		if server.Name == s.ConfirmServer && server.Scope == s.ConfirmScope && server.Host == s.ConfirmHost && server.Workspace == s.ConfirmWorkspace {
			s.Servers = append(s.Servers[:i], s.Servers[i+1:]...)
			break
		}
	}
	s.ConfirmServer = ""
	s.message("演示配置已删除。")
}
