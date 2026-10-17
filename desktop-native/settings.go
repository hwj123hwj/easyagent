package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

// Shared desktop translations retain the existing wording (Apache-2.0).
//
//go:embed locale.json
var localeJSON []byte

//go:embed locale_preview.txt
var previewLocale string

var english = func() map[string]string {
	var m map[string]string
	_ = json.Unmarshal(localeJSON, &m)
	for _, line := range strings.Split(previewLocale, "\n") {
		if zh, en, ok := strings.Cut(line, "|"); ok {
			m[zh] = en
		}
	}
	return m
}()

func (a *preview) tr(text string) string {
	if a.Settings.Preferences.Language == "English" {
		if en := english[text]; en != "" {
			return en
		}
	}
	return text
}
func (a *preview) text(c *ui.Context, text string) ui.Element { return ui.Text(c, a.tr(text)) }
func (a *preview) action(c *ui.Context, label string) ui.Element {
	return flatButton(c, "", a.tr(label)).Padding(6, 10).Background(c.Theme().Surface).Border(1, c.Theme().Border).TextColor(c.Theme().Text)
}
func (a *preview) primary(c *ui.Context, label string) ui.Element {
	return a.action(c, label).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Border(0, c.Theme().Accent)
}
func (a *preview) note(c *ui.Context, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	a.text(c, text).FontSize(12).LineHeight(1.6).TextColor(c.Theme().TextMuted)
}
func (a *preview) heading(c *ui.Context, text string) {
	a.text(c, text).FontSize(14).FontWeight(600).Margin(12, 0, 2, 0)
}
func (a *preview) input(c *ui.Context, label string, value *string, placeholder string, secret bool) ui.Element {
	var e ui.Element
	ui.Column(c).MinWidth(0).Gap(6).Children(func() {
		a.text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
		e = ui.TextInput(c.Key(label), value).Label(a.tr(label)).Placeholder(a.tr(placeholder)).FontSize(13).Padding(11, 13).Background(c.Theme().Surface).Border(1, c.Theme().Border)
		if secret {
			e.Password()
		}
	})
	return e
}
func (a *preview) multiline(c *ui.Context, label string, value *string, min, max int) {
	ui.Column(c).MinWidth(0).Gap(6).Children(func() {
		a.text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.TextArea(c.Key(label), value).Label(a.tr(label)).Lines(min, max).Font("monospace").FontSize(12).Padding(8).Background(c.Theme().Surface).Border(1, c.Theme().Border)
	})
}
func (a *preview) dropdown(c *ui.Context, label string, value *string, choices []string) ui.Element {
	var e ui.Element
	ui.Column(c).MinWidth(0).Gap(6).Children(func() {
		a.text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
		e = a.selectControl(c.Key(label), label, value, choices).Height(42)
	})
	return e
}
func (a *preview) selectControl(c *ui.Context, label string, value *string, choices []string) ui.Element {
	sel := ui.SelectBase(c, value)
	sel.Trigger.Label(a.tr(label)).Gap(8).Padding(6, 8).Radius(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).FontSize(12).TextColor(c.Theme().Text).Children(func() {
		a.text(c, *value).Grow(1).MinWidth(0).SingleLine()
		icon(c, "chevron-down")
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Gap(2).Radius(6).MinWidth(180).MaxWidth(480).Background(c.Theme().SurfaceHover).Border(1, c.Theme().Border).Margin(4, 0, 0, 0)
		for _, choice := range choices {
			option := sel.Item(choice).Label(a.tr(choice)).Justify(ui.Start).Padding(7, 8).Gap(8).Radius(4).FontSize(12).TextColor(c.Theme().Text)
			if option.Highlighted() {
				option.Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent)
			}
			option.Children(func() {
				a.text(c, choice).Grow(1).MinWidth(0).SingleLine()
				if choice == *value {
					icon(c, "check")
				}
			})
		}
	})
	return sel.Trigger
}

func (a *preview) preferenceRow(c *ui.Context, label, detail string, control func()) {
	ui.Row(c).Gap(20).Padding(12, 16).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() { a.text(c, label).FontSize(13).FontWeight(500); a.note(c, detail) })
		control()
	})
}
func (a *preview) settingsView(c *ui.Context) {
	s := &a.Settings
	page := s.page()
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(240).Background(c.Theme().Surface).BorderWidth(0, 1, 0, 0).BorderColor(c.Theme().Border).Padding(16, 12, 14, 12).Children(func() {
			flatButton(c, "arrow-left", a.tr("返回工作区")).Justify(ui.Start).Padding(6, 10).Height(32).TextColor(c.Theme().Text).OnClick(a.closeSettings)
			ui.Scroll(c).Grow(1).MinHeight(0).Padding(14, 0).Children(func() {
				for group, title := range []string{"基础设置", "连接与能力"} {
					a.text(c, title).FontSize(11).TextColor(c.Theme().TextMuted).Padding(8, 8, 6, 8)
					for _, p := range settingsPages {
						if p.Group != group {
							continue
						}
						item := ui.ButtonBase(c.Key(p.ID)).Label(a.tr(p.Label)).Height(34).Justify(ui.Start).Gap(9).Padding(6, 10).Radius(6).TextColor(c.Theme().TextMuted).FontSize(13).Selected(p.ID == s.Tab).OnClick(func() { a.openSettings(p.ID) })
						if s.Tab == p.ID {
							item.Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent)
						} else if item.Hovered() {
							item.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
						}
						item.Children(func() { icon(c, p.Icon); a.text(c, p.Label).SingleLine() })
					}
				}
			})
			ui.Row(c).Gap(8).Padding(10).Children(func() {
				ui.Text(c, "ea·").FontSize(20).FontWeight(600).TextColor(c.Theme().Accent)
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, "EasyAgent").FontSize(12)
					version := "Go 原生 · " + nativeAppVersion()
					a.text(c, version).FontSize(10).TextColor(c.Theme().TextMuted)
				})
			})
		})
		ui.Scroll(c.Key(s.Tab)).Grow(1).MinWidth(0).Padding(24, 32, 40, 32).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(780).MinWidth(0).Margin(0, ui.Auto).Gap(12).Children(func() {
				ui.Column(c).Gap(6).Margin(0, 0, 6, 0).Children(func() {
					a.text(c, page.Label).Bind(&s.Heading).Focusable().Role(ui.RoleHeading).FontSize(22).FontWeight(600)
					a.note(c, page.Description)
					if page.ID == "models" || page.ID == "connections" || page.ID == "mcp" || page.ID == "feishu" {
						ui.Row(c).Gap(8).Padding(6, 0).TextColor(c.Theme().TextMuted).Children(func() {
							kind := "laptop"
							if !s.host().Local {
								kind = "globe"
							}
							icon(c, kind)
							ui.Text(c, s.host().Name).FontSize(12)
							if a.Live == nil {
								a.text(c, "演示环境").FontSize(12)
							} else {
								ui.Text(c, a.connectionLabel()).FontSize(12)
							}
						})
					}
					if a.Live == nil {
						a.text(c, "语言与外观保存在此设备；连接与能力设置仅为演示。").FontSize(11).TextColor(c.Theme().TextMuted)
					} else if page.ID == "general" || page.ID == "appearance" {
						a.note(c, "语言与外观保存在此设备。")
					} else {
						a.note(c, "设置属于当前运行主机，保存后会实际生效。")
					}
				})
				if a.Live != nil {
					a.liveSettingsPage(c)
				} else {
					switch page.ID {
					case "general":
						a.generalSettings(c)
					case "appearance":
						a.appearanceSettings(c)
					case "models":
						a.modelSettings(c)
					case "usage":
						a.usageSettings(c)
					case "skillMarket":
						a.skillSettings(c)
					case "experiments":
						a.experimentSettings(c)
					case "connections":
						a.connectionSettings(c)
					case "mcp":
						a.mcpSettings(c)
					case "capabilities":
						a.capabilitySettings(c)
					case "feishu":
						a.feishuSettings(c)
					}
				}
				if s.Error != "" {
					a.text(c, s.Error).FontSize(12).TextColor(c.Theme().Danger).Role(ui.RoleStatus)
				}
				if s.Notice != "" {
					a.text(c, s.Notice).FontSize(12).TextColor(c.Theme().Accent).Role(ui.RoleStatus)
				}
			})
		})
	})
}
func (a *preview) generalSettings(c *ui.Context) {
	s := &a.Settings
	ui.Column(c).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() {
		a.preferenceRow(c, "界面语言", "选择应用的显示语言。", func() {
			if a.selectControl(c, "界面语言", &s.Preferences.Language, []string{"中文", "English"}).Width(180).Label(a.tr("语言选择")).Changed() {
				s.savePreferences()
				a.syncNativeLocale()
			}
		})
		version := "EasyAgent " + nativeAppVersion()
		a.preferenceRow(c, "桌面版本", version, func() {
			a.action(c, "检查更新").Disabled(!a.UpdaterReady).OnClick(updater.CheckForUpdates)
		})
	})
	if a.UpdaterReady {
		checks := updater.AutomaticChecks()
		if ui.Checkbox(c, &checks, "自动检查更新").Changed() {
			updater.SetAutomaticChecks(checks)
		}
		downloads := updater.AutomaticDownloads()
		if ui.Checkbox(c, &downloads, "自动下载并安装更新，下次启动生效").Changed() {
			updater.SetAutomaticDownloads(downloads)
		}
		if last := updater.LastCheck(); !last.IsZero() {
			a.note(c, "上次成功检查："+last.Local().Format("2006-01-02 15:04"))
		}
	}
	a.note(c, "仅安装经过签名验证的原生更新包；运行中的任务或未保存文件会阻止重启。")
	a.heading(c, "常用快捷键")
	ui.Column(c).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() {
		for _, shortcut := range []struct{ label, key string }{{"发送消息", "Enter"}, {"输入换行", "Shift + Enter"}, {"打开命令菜单", "/"}, {"文件工作台", "⌘ P"}, {"代码审查", "⌘ Shift G"}} {
			ui.Row(c).Padding(10, 16).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
				a.text(c, shortcut.label).FontSize(12)
				ui.Spacer(c)
				ui.Text(c, shortcut.key).Font("monospace").FontSize(11).Padding(3, 7).Radius(4).Background(c.Theme().SurfaceHover).Border(1, c.Theme().Border)
			})
		}
	})
	if a.Live == nil {
		a.note(c, "文件工作台仅展示演示目录，代码审查尚未接入。")
	}
}
func (a *preview) appearanceSettings(c *ui.Context) {
	s := &a.Settings
	a.heading(c, "配色主题")
	a.note(c, "立即应用到当前界面，选择会保留在此设备。")
	ui.Row(c).AlignItems(ui.Stretch).Gap(12).Children(func() {
		for _, option := range []struct{ id, label, detail string }{
			{"dark", "青夜", "深青背景 · 柔和浅青"},
			{"terracotta", "经典陶土", "暖炭画布 · 陶土橙 Accent"},
			{"light", "浅色", "明亮纸面 · 暖调红褐"},
			{"system", "跟随系统", "随系统外观自动切换"},
		} {
			selected := s.Preferences.Theme == option.id
			e := ui.ButtonBase(c.Key(option.id)).Label(a.tr(option.label)).Grow(1).Basis(0).MinWidth(0).Column().AlignItems(ui.Stretch).Justify(ui.Start).TextColor(c.Theme().Text).Padding(10, 12).Gap(10).Radius(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Selected(selected).TextColor(c.Theme().Text).OnClick(func() { s.Preferences.Theme = option.id; s.savePreferences() })
			if selected {
				e.BorderColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.08))
			}
			e.Children(func() {
				background := ui.Hex("#faf9f5")
				side := ui.Hex("#ffffff")
				color := ui.Hex("#c25a3c")
				if option.id == "terracotta" {
					background = ui.Hex("#262624")
					side = ui.Hex("#2f2e2c")
					color = ui.Hex("#d97757")
				} else if option.id == "dark" {
					background = bg
					side = elevated
					color = accent
				}
				ui.Row(c).FillWidth().Height(64).Radius(6).Clip().Border(1, c.Theme().Border).Background(background).AlignItems(ui.Stretch).Children(func() {
					if option.id == "system" {
						ui.Box(c).Absolute().Right(0).Top(0).Bottom(0).WidthPercent(50).Background(ui.Hex("#faf9f5"))
					}
					ui.Box(c).WidthPercent(28).Background(side)
					ui.Column(c).Grow(1).Padding(12, 10).Gap(8).Children(func() {
						ui.Box(c).Height(4).WidthPercent(78).Radius(2).Background(muted)
						ui.Box(c).Height(4).WidthPercent(50).Radius(2).Background(color)
					})
				})
				ui.Row(c).Children(func() {
					a.text(c, option.label).FontSize(13).FontWeight(500).TextColor(c.Theme().Text)
					ui.Spacer(c)
					if selected {
						ui.Icon(c, icons["circle-check"]).FontSize(17).TextColor(c.Theme().Accent)
					}
				})
				a.text(c, option.detail).FontSize(11).TextColor(c.Theme().TextMuted)
			})
		}
	})
}
func (a *preview) modelSettings(c *ui.Context) {
	s := &a.Settings
	ui.Row(c).Children(func() {
		a.heading(c, "当前模型")
		ui.Spacer(c)
		a.action(c, "刷新模型").OnClick(func() { s.message("示例模型目录已刷新。") })
	})
	ui.Text(c, s.host().Name+" · "+a.tr("当前模型")+"："+a.current().Model).FontSize(13)
	if !s.host().Local {
		a.heading(c, "模型配置属于服务主机")
		a.note(c, "远程模型需在对应主机的 EasyAgent 配置中设置。")
		a.action(c, "选择运行主机").OnClick(func() { a.openSettings("connections") })
		return
	}
	a.heading(c, "本地模型来源")
	ui.RadioGroup(c, func() {
		ui.Row(c).Gap(24).Wrap().Children(func() {
			for _, mode := range []string{"沿用命令行配置", "在桌面客户端配置"} {
				if ui.Radio(c, &s.Mode, mode, a.tr(mode)).Changed() {
					s.Checked = false
					s.Error = ""
				}
			}
		})
	})
	if s.Mode == "沿用命令行配置" {
		a.note(c, "沿用 easyagent.yaml、个人配置文件和 EA_* 环境变量。")
	} else {
		if a.dropdown(c, "模型服务", &s.Provider, []string{"OpenAI 兼容服务 / 网关", "Anthropic"}).Changed() {
			s.APIKey = ""
			s.Checked = false
		}
		if a.input(c, "服务地址", &s.BaseURL, "http://localhost:4001", false).Changed() {
			s.Checked = false
			s.Notice = ""
			s.Error = ""
		}
		if a.input(c, "默认模型", &s.Model, "填写服务中的模型 ID", false).Changed() {
			s.Checked = false
			s.Notice = ""
		}
		if a.input(c, "API Key", &s.APIKey, "演示输入（不保存）", true).Changed() {
			s.Checked = false
			s.Notice = ""
		}
		a.note(c, "此预览不会请求模型服务，密钥只在当前页面内存中暂存。")
	}
	ui.Row(c).Gap(12).Wrap().Children(func() {
		incomplete := s.Mode == "在桌面客户端配置" && (strings.TrimSpace(s.BaseURL) == "" || strings.TrimSpace(s.Model) == "")
		a.action(c, "检测连接").Disabled(incomplete).OnClick(s.checkModel)
		running := false
		for _, session := range a.Sessions {
			if session.Running {
				running = true
			}
		}
		a.primary(c, "应用并重新连接").Disabled(incomplete || running).OnClick(func() {
			s.checkModel()
			if !s.Checked {
				return
			}
			s.HasKey = s.APIKey != "" || s.HasKey
			s.APIKey = ""
			if s.Mode == "在桌面客户端配置" {
				a.current().Model = strings.TrimSpace(s.Model)
			}
			s.message("演示模型配置已应用，未重启或连接真实 Agent。")
		})
	})
	if s.Checked {
		a.note(c, "示例模型目录 · 不代表真实推理验证")
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for _, name := range []string{"coding", "glm-5.3-flash", "deepseek-flash"} {
				a.action(c, name).Disabled(s.Mode != "在桌面客户端配置").OnClick(func() { s.Model = name })
			}
		})
	}
}
func (a *preview) connectionSettings(c *ui.Context) {
	s := &a.Settings
	a.heading(c, "运行主机")
	a.note(c, "文件路径、工具和 MCP 均属于所选服务主机。")
	ui.Column(c).Border(1, c.Theme().Border).Radius(8).Background(c.Theme().Surface).Children(func() {
		for index, h := range s.Hosts {
			e := ui.ButtonBase(c.Key(h.ID)).Label("连接："+h.Name).Justify(ui.Start).Padding(12, 16).Gap(16).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).OnClick(func() { s.selectHost(index) })
			if index == s.Host {
				e.Background(c.Theme().Accent.Alpha(.1))
			}
			e.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, h.Name).FontSize(13)
					detail := h.URL
					if h.Local {
						detail = a.tr("应用管理的本地 Agent")
					}
					ui.Text(c, detail).FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(1)
				})
				label := "连接"
				if index == s.Host {
					label = "当前连接"
				}
				a.text(c, label).FontSize(12).TextColor(c.Theme().Accent)
				icon(c, "arrow-right")
			})
		}
	})
	a.heading(c, "远程连接配置")
	choices := []string{}
	for _, h := range s.Hosts {
		if !h.Local {
			choices = append(choices, h.Name)
		}
	}
	choices = append(choices, "新增连接")
	if a.dropdown(c, "选择配置", &s.EditHost, choices).Changed() {
		s.HostToken = ""
		if s.EditHost == "新增连接" {
			s.HostName = ""
			s.HostURL = ""
		} else {
			for _, h := range s.Hosts {
				if h.Name == s.EditHost {
					s.HostName = h.Name
					s.HostURL = h.URL
				}
			}
		}
	}
	a.input(c, "名称", &s.HostName, "连接名称", false)
	a.input(c, "Agent 服务地址", &s.HostURL, "http://192.168.5.16:8080", false)
	a.input(c, "API 令牌", &s.HostToken, "演示输入（不保存）", true)
	a.note(c, "预览模式仅保存当前窗口的演示配置，令牌不会写入偏好文件。")
	a.primary(c, "保存并连接").Disabled(strings.TrimSpace(s.HostURL) == "").OnClick(s.saveHost)
}
func (a *preview) usageSettings(c *ui.Context) {
	s := &a.Settings
	ui.Row(c).Children(func() {
		a.heading(c, "近 30 天用量概览")
		ui.Spacer(c)
		a.action(c, "刷新").OnClick(func() { s.message("演示用量已刷新。") })
	})
	a.note(c, "以下为演示执行收据统计，未读取真实会话用量。")
	ui.Grid(c).Columns(4).Gap(12).Children(func() {
		for _, value := range []struct{ label, number string }{{"总 Tokens", "60.2K"}, {"输入 Tokens", "42.0K"}, {"输出 Tokens", "18.2K"}, {"总任务数 (Runs)", "17"}} {
			ui.Column(c).Padding(12).Gap(4).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).MinWidth(0).Children(func() {
				a.text(c, value.label).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, value.number).FontSize(20).FontWeight(600)
			})
		}
	})
	a.heading(c, "会话消耗排行")
	ui.Column(c).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(8).Children(func() {
		ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
			a.text(c, "会话").Grow(1).FontSize(12)
			for _, label := range []string{"Runs", "输入", "输出", "合计"} {
				a.text(c, label).Width(60).TextAlign(ui.End).FontSize(12)
			}
		})
		for i, session := range a.Sessions {
			if i > 2 {
				break
			}
			ui.ButtonBase(c.Key(session.ID)).Label("查看用量会话："+session.Title).Justify(ui.Start).Padding(10, 12).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).OnClick(func() { a.selectSession(session.ID); a.closeSettings() }).Children(func() {
				ui.Text(c, session.Title).FontSize(12).Grow(1).MinWidth(0).MaxLines(1)
				for _, value := range []string{fmt.Sprint(8 - i*2), "14.0K", "6.1K", "20.1K"} {
					ui.Text(c, value).FontSize(12).Width(60).TextAlign(ui.End).TextColor(c.Theme().TextMuted)
				}
			})
		}
	})
}
func (a *preview) experimentSettings(c *ui.Context) {
	s := &a.Settings
	a.action(c, "刷新状态").OnClick(func() { s.message("电脑控制演示状态已刷新。") })
	a.note(c, "电脑控制可以读取屏幕与操作应用。此预览仅切换界面状态，不会请求系统权限或操作应用。")
	ui.Column(c).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() {
		a.preferenceRow(c, "启用电脑控制", "默认关闭。关闭时新会话不会暴露电脑控制工具。", func() { ui.Switch(c, &s.ComputerEnabled).Label(a.tr("启用电脑控制")) })
		a.preferenceRow(c, "审批策略", "普通操作可自动执行；退出、注销、锁屏等操作仍会询问。", func() {
			ui.Row(c).Gap(6).Children(func() {
				for _, mode := range []string{"逐批询问", "自动执行"} {
					e := a.action(c, mode).OnClick(func() { s.Approval = mode })
					if mode == s.Approval {
						e.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
					}
				}
			})
		})
		a.preferenceRow(c, "Provider", "", func() { ui.Text(c, "native-preview").Font("monospace").FontSize(12) })
		a.preferenceRow(c, "可用性", "", func() { a.text(c, "演示组件").FontSize(12) })
		a.preferenceRow(c, "请求系统权限", "辅助功能与屏幕录制是两项独立授权。", func() {
			a.action(c, "请求系统权限").OnClick(func() { s.message("预览不请求系统权限。正式客户端的授权流程保持原样。") })
		})
		a.preferenceRow(c, "系统权限", "", func() { a.text(c, "未请求").FontSize(12) })
		a.preferenceRow(c, "已控制应用（审计）", "实际被模型执行过操作的应用。", func() { a.text(c, "暂无").FontSize(12) })
		a.preferenceRow(c, "当前占用", "", func() { a.text(c, "空闲").FontSize(12) })
	})
}
func (a *preview) skillSettings(c *ui.Context) {
	s := &a.Settings
	if s.SkillDetail != "" {
		for _, skill := range s.Skills {
			if skill.Name != s.SkillDetail {
				continue
			}
			a.action(c, "返回技能列表").OnClick(func() { s.SkillDetail = "" })
			a.text(c, skill.Title).FontSize(20).FontWeight(600)
			ui.Text(c, skill.Name).Font("monospace").FontSize(12).TextColor(c.Theme().TextMuted)
			a.note(c, skill.Description)
			ui.Row(c).Gap(10).Children(func() {
				if skill.Installed {
					a.action(c, "卸载").OnClick(func() { s.ConfirmSkill = skill.Name })
				} else {
					a.primary(c, "安装技能").OnClick(func() { skill.Installed = true; s.message("示例技能已标记安装，未下载或写入文件。") })
				}
				a.action(c, "复制使用方式").OnClick(func() { c.WriteClipboard("$" + skill.Name); s.message("使用方式已复制。") })
			})
			a.heading(c, "使用方式")
			ui.Text(c, "$"+skill.Name).Font("monospace").Padding(10).Background(c.Theme().Surface).Radius(6)
			doc := markdownDocument{}
			doc.render(c, skill.Content)
			a.heading(c, "技能文件")
			ui.Text(c, "SKILL.md  ·  演示预览").Font("monospace").FontSize(12)
		}
	} else {
		ui.TextInput(c, &s.SkillQuery).Label(a.tr("搜索技能")).Placeholder(a.tr("搜索技能…")).Padding(8, 10).Background(c.Theme().Surface).Border(1, c.Theme().Border)
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, filter := range []string{"精选", "热门", "名称", "已安装"} {
				e := a.action(c, filter).OnClick(func() { s.SkillFilter = filter })
				if filter == s.SkillFilter {
					e.TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.1))
				}
			}
			ui.Spacer(c)
			a.action(c, "刷新").OnClick(func() { s.message("示例技能目录已刷新。") })
		})
		ui.Row(c).Gap(8).Children(func() {
			for _, category := range []string{"全部", "开发", "效率"} {
				e := a.action(c, category).OnClick(func() { s.SkillCategory = category })
				if category == s.SkillCategory {
					e.TextColor(c.Theme().Accent)
				}
			}
		})
		a.note(c, "以下为本地示例目录，未连接真实技能市场。")
		skills := append([]*demoSkill(nil), s.Skills...)
		if s.SkillFilter == "名称" {
			sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		}
		matches := 0
		for _, skill := range skills {
			if s.SkillFilter == "已安装" && !skill.Installed || s.SkillCategory != "全部" && s.SkillCategory != skill.Category || !strings.Contains(strings.ToLower(skill.Name+skill.Title+skill.Description), strings.ToLower(s.SkillQuery)) {
				continue
			}
			matches++
			ui.Column(c.Key(skill.Name)).Padding(14, 0).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
				ui.Row(c).Gap(10).Children(func() {
					ui.ButtonBase(c).Label("查看技能：" + skill.Name).Justify(ui.Start).Grow(1).OnClick(func() { s.SkillDetail = skill.Name }).Children(func() { a.text(c, skill.Title).FontSize(14).FontWeight(600) })
					if skill.Installed {
						a.action(c, "已安装").OnClick(func() { s.SkillDetail = skill.Name })
					} else {
						a.action(c, "安装").OnClick(func() { skill.Installed = true; s.message("示例技能已标记安装，未下载或写入文件。") })
					}
				})
				ui.Text(c, skill.Name).Font("monospace").FontSize(11).TextColor(c.Theme().TextMuted)
				a.note(c, skill.Description)
			})
		}
		if matches == 0 {
			a.note(c, "没有匹配的技能。")
		}
	}
	confirm := s.ConfirmSkill != ""
	ui.Modal(c, &confirm, func() {
		a.text(c, "卸载示例技能？").FontSize(18).FontWeight(600)
		a.note(c, "仅移除当前预览中的安装标记，不会删除文件。")
		ui.Row(c).Gap(10).Children(func() {
			a.action(c, "取消").OnClick(func() { s.ConfirmSkill = "" })
			a.primary(c, "确认卸载").OnClick(func() {
				for _, skill := range s.Skills {
					if skill.Name == s.ConfirmSkill {
						skill.Installed = false
					}
				}
				s.ConfirmSkill = ""
				s.message("示例技能已卸载。")
			})
		})
	})
	if !confirm {
		s.ConfirmSkill = ""
	}
}

func (a *preview) syncNativeLocale() {
	if a.Window == nil {
		return
	}
	tag := "zh-Hans"
	if a.Settings.Preferences.Language == "English" {
		tag = "en"
	}
	mygo.App.SetLocale(tag)
}

func nativeAppVersion() string {
	if version := mygo.App.Version(); version != "" {
		return version
	}
	return "开发版"
}
