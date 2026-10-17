package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"strings"
)

var (
	bg       = ui.Hex("#131718")
	elevated = ui.Hex("#181d1f")
	raised   = ui.Hex("#1e2426")
	sunken   = ui.Hex("#0e1112")
	ink      = ui.Hex("#edf2f0")
	muted    = ui.Hex("#9aa8a3")
	accent   = ui.Hex("#4ecdc4")
	line     = ui.Hex("#ffffff").Alpha(.08)
)

func icon(c *ui.Context, name string) {
	if svg := icons[name]; svg != nil {
		ui.Icon(c, svg).FontSize(16)
	}
}
func flatButton(c *ui.Context, name, label string) ui.Element {
	e := ui.ButtonBase(c).Label(label).Gap(6).Padding(5, 7).Radius(6).TextColor(c.Theme().TextMuted).FontSize(12)
	if e.Hovered() {
		e.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
	}
	e.Children(func() {
		if name != "" {
			icon(c, name)
		}
		if label != "" {
			ui.Text(c, label).SingleLine()
		}
	})
	return e
}
func iconButton(c *ui.Context, name, label string) ui.Element {
	e := ui.ButtonBase(c).Label(label).Size(30, 30).Center().Radius(6).TextColor(c.Theme().TextMuted)
	if e.Hovered() {
		e.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
	}
	e.Children(func() { icon(c, name) })
	return e.Tooltip(label)
}
func (a *preview) menuPanel(c *ui.Context, anchor ui.Element, open *bool, body func()) ui.Element {
	return ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			*open = false
			if !a.current().Archived {
				a.ReturnToComposer = true
			}
		}
		panel.AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).Width(240).Padding(6).Gap(4).Radius(8).Background(c.Theme().SurfaceHover).Border(1, c.Theme().Border).Margin(6, 0, 0, 0)
		body()
	})
}

// Menu rows have a full-width hit target and share selection/focus treatment.
func menuOption(c *ui.Context, name, label string, selected bool) ui.Element {
	e := ui.ButtonBase(c).Label(label).FillWidth().Height(34).Justify(ui.Start).Gap(8).Padding(6, 8).Radius(5).FontSize(12).TextColor(c.Theme().Text).Selected(selected)
	if selected {
		e.Background(c.Theme().Accent.Alpha(.1)).TextColor(c.Theme().Accent)
	} else if e.Hovered() {
		e.Background(c.Theme().Text.Alpha(.06))
	}
	e.Children(func() {
		if name != "" {
			icon(c, name)
		}
		ui.Text(c, label).SingleLine().MinWidth(0)
		ui.Spacer(c)
		if selected {
			icon(c, "check")
		}
	})
	return e
}
func menuCaption(c *ui.Context, label string) {
	ui.Text(c, label).Padding(5, 8).FontSize(11).TextColor(c.Theme().TextMuted)
}

func (a *preview) View(c *ui.Context) {
	theme := previewTheme(c.Theme(), a.Settings.Preferences)
	c.SetTheme(&theme)
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openSettings("general")
	}
	if c.Shortcut(ui.Cmd, ui.KeyP) && !a.Settings.Open {
		a.Workbench = !a.Workbench
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) && !a.Settings.Open {
		a.newSession()
	}
	if a.Live != nil && !a.Settings.Open && c.Shortcut(ui.Cmd, ui.KeyBackquote) {
		a.toggleTerminal()
	}
	if a.Live != nil && c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
		a.openResource("review")
	}
	ui.Column(c).Fill().Background(c.Theme().Background).TextColor(c.Theme().Text).FontSize(14).Children(func() {
		a.chrome(c)
		if a.Live != nil {
			a.liveNotices(c)
		}
		if a.Settings.Open {
			a.settingsView(c)
			return
		}
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			width, _ := c.Size()
			if a.Sidebar && (!a.Workbench || width >= 1100) {
				a.sidebar(c)
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.toolbar(c)
				a.transcript(c)
				a.composer(c)
				if a.Live != nil {
					a.liveTerminal(c)
				}
			})
			if a.Workbench {
				a.workbench(c)
			}
		})
		if a.Live != nil {
			a.musicBar(c)
		}
	})
	if a.Live != nil {
		a.liveModals(c)
	}
	if a.ReturnToComposer && !a.Settings.Open {
		a.Composer.Focus()
		if a.Composer.Focused(c) {
			a.ReturnToComposer = false
		}
	}
}
func (a *preview) chrome(c *ui.Context) {
	if a.Live != nil {
		a.liveChrome(c)
		return
	}
	bar := c.TitleBar()
	ui.Row(c).Height(48).Padding(1, max(bar.Right, 14)+6, 0, max(bar.Left, 76)+6).Gap(10).AlignItems(ui.Center).DragWindow().BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		if !a.Settings.Open {
			iconButton(c, "panel", a.tr("切换会话侧栏")).OnClick(func() {
				width, _ := c.Size()
				if a.Workbench && width < 1100 {
					a.Workbench = false
					a.Sidebar = true
				} else {
					a.Sidebar = !a.Sidebar
				}
			})
		}
		ui.Text(c, "ea·").Label("品牌标记").FontSize(24).FontWeight(600).LineHeight(1).TextColor(c.Theme().Accent)
		ui.Text(c, "EasyAgent").Label("应用名称").FontSize(13).FontWeight(600).LineHeight(1)
		ui.Box(c).Label("标题栏分隔线").Height(18).Width(1).Background(c.Theme().Border).Margin(0, 4)
		host := flatButton(c, "laptop", a.Settings.host().Name).OnClick(func() { a.toggleMenu(&a.HostMenu) })
		a.menuPanel(c, host, &a.HostMenu, func() {
			a.text(c, "本机演示环境").Bold()
			a.text(c, "预览未连接 EasyAgent 服务").FontSize(12).TextColor(c.Theme().TextMuted)
		})
		a.text(c, "原生预览 · 演示数据").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		if !a.Settings.Open {
			iconButton(c, "terminal", a.tr("终端尚未接入此预览")).Disabled(true)
			iconButton(c, "columns", a.tr("切换工作台")).OnClick(func() { a.Workbench = !a.Workbench })
		}
	})
}
func (a *preview) sidebar(c *ui.Context) {
	if a.Live != nil {
		a.liveSidebar(c)
		return
	}
	ui.Column(c).Width(250).Background(c.Theme().Surface).BorderWidth(0, 1, 0, 0).BorderColor(c.Theme().Border).Children(func() {
		ui.Column(c).Padding(14).Gap(12).Children(func() {
			a.text(c, "对话").FontSize(14).Bold()
			ui.Row(c).Gap(6).Children(func() {
				flatButton(c, "plus", a.tr("新建会话")).Grow(1).Padding(8, 10).TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.07)).Border(1, c.Theme().Accent.Alpha(.28)).OnClick(a.newSession)
				iconButton(c, "folder", a.tr("项目目录仅使用演示数据")).Disabled(true)
			})
			ui.Row(c).Gap(7).Padding(7, 9).Radius(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
				icon(c, "search")
				ui.TextInputBase(c.Key("search"), &a.Search).Grow(1).MinWidth(0).FontSize(12).Label("搜索会话").Placeholder("搜索会话…")
			})
			ui.Row(c).Gap(14).Children(func() {
				recent := flatButton(c, "", a.tr("最近")).OnClick(func() { a.Archived = false })
				archive := flatButton(c, "", a.tr("已归档")).OnClick(func() { a.Archived = true })
				if a.Archived {
					archive.TextColor(c.Theme().Accent)
				} else {
					recent.TextColor(c.Theme().Accent)
				}
			})
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, 10).Children(func() {
			matches := 0
			for _, project := range []string{"easyagent", "hwj"} {
				var sessions []*session
				for _, s := range a.Sessions {
					if s.Project == project && s.Archived == a.Archived && strings.Contains(strings.ToLower(s.Title), strings.ToLower(a.Search)) {
						sessions = append(sessions, s)
					}
				}
				if len(sessions) == 0 {
					continue
				}
				matches += len(sessions)
				ui.Row(c).Gap(6).Padding(12, 7, 7, 7).TextColor(c.Theme().TextMuted).FontSize(12).Children(func() {
					icon(c, "chevron-down")
					icon(c, "folder")
					ui.Text(c, project)
					ui.Spacer(c)
					ui.Text(c, fmt.Sprint(len(sessions)))
				})
				for _, s := range sessions {
					item := ui.ButtonBase(c.Key(s.ID)).Label(s.Title).Padding(9, 9).Gap(7).Radius(6).MinWidth(0).OnClick(func() { a.selectSession(s.ID) })
					if a.Active == s.ID {
						item.Background(c.Theme().Accent.Alpha(.12)).Border(1, c.Theme().Accent.Alpha(.26))
					} else if item.Hovered() {
						item.Background(c.Theme().SurfaceHover)
					}
					item.Children(func() {
						icon(c, "chat")
						ui.Text(c, s.Title).FontSize(13).MaxLines(1).Grow(1).MinWidth(0)
						if s.Running {
							ui.Text(c, "●").TextColor(c.Theme().Accent)
						} else if s.Pending {
							ui.Text(c, "●").TextColor(ui.Hex("#fbbf24"))
						}
					})
				}
			}
			if matches == 0 {
				a.text(c, "没有匹配的会话").Padding(12, 8).TextColor(c.Theme().TextMuted).FontSize(12)
			}
		})
		ui.Column(c).Padding(12).Gap(6).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
			flatButton(c, "settings", a.tr("设置")).Justify(ui.Start).OnClick(func() { a.openSettings("general") })
			a.text(c, "MyGo 0.5.5 · Go 原生").FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 7)
		})
	})

}
func (a *preview) toolbar(c *ui.Context) {
	if a.Live != nil {
		a.liveToolbar(c)
		return
	}
	s := a.current()
	ui.Row(c).Height(52).Padding(0, 20).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, s.Title).FontSize(14).Bold().MaxLines(1).Grow(1).MinWidth(0)
		ui.Row(c).Gap(5).FontSize(11).TextColor(c.Theme().TextMuted).Children(func() { icon(c, "folder"); ui.Text(c, s.Project).SingleLine() })
		status := "就绪"
		color := c.Theme().TextMuted
		if s.Running {
			status = "生成中"
			color = c.Theme().Accent
		} else if s.Pending {
			status = "等待审批"
			color = ui.Hex("#fbbf24")
		} else if s.Archived {
			status = "已归档"
		}
		ui.Text(c, "● "+status).FontSize(11).TextColor(color).SingleLine()
		more := iconButton(c, "menu", a.tr("会话操作")).OnClick(func() { a.toggleMenu(&a.MoreMenu) })
		a.menuPanel(c, more, &a.MoreMenu, func() {
			label := "归档会话"
			if s.Archived {
				label = "恢复会话"
			}
			menuOption(c, "archive", label, false).Disabled(s.Running || s.Pending).OnClick(func() { s.Archived = !s.Archived; a.Archived = s.Archived; a.MoreMenu = false })
			menuOption(c, "shield", "演示工具审批", false).Disabled(s.Running || s.Pending || s.Archived).OnClick(func() { a.requestApproval(); a.MoreMenu = false })
			menuOption(c, "copy", "复制会话文本", false).OnClick(func() {
				var b strings.Builder
				for _, m := range s.Messages {
					b.WriteString(m.Text + "\n\n")
				}
				c.WriteClipboard(b.String())
				a.MoreMenu = false
				c.Toast("已复制会话文本")
			})
		})
	})
}
func (a *preview) transcript(c *ui.Context) {
	s := a.current()
	if a.Live != nil {
		a.conversationFind(c, s)
	}
	scroll := ui.Scroll(c.Key(s.ID)).Grow(1).MinHeight(0).TrackScroll(&s.Scroll)
	scroll.Children(func() {
		ui.Column(c).FillWidth().MaxWidth(816).MinWidth(0).Margin(0, ui.Auto).Padding(28, 20, 12, 20).Gap(24).Selectable().Children(func() {
			if len(s.Messages) == 0 {
				ui.Column(c).Padding(80, 0).Gap(12).Children(func() {
					size := float32(38)
					title := "开始一段新对话"
					if a.Live != nil {
						size = 58
						title = "今天，想一起完成什么？"
					}
					ui.Text(c, "ea·").FontSize(size).FontWeight(600).TextColor(c.Theme().Accent)
					a.text(c, title).FontSize(29).FontWeight(500).Margin(20, 0, 4, 0)
					if a.Live == nil {
						a.text(c, "在下方输入消息，或用 /approval 体验工具审批。").TextColor(c.Theme().TextMuted)
					} else {
						a.text(c, "说说你的任务。回复、工具执行和每一步结果，都留在这段对话里。").FontSize(14).LineHeight(1.85).TextColor(c.Theme().TextMuted)
					}
					if a.Live == nil {
						a.text(c, "此预览只使用演示数据。").FontSize(12).TextColor(c.Theme().TextMuted)
					} else {
						ui.Row(c).Wrap().Gap(12).Margin(18, 0, 0, 0).Children(func() {
							for _, suggestion := range []string{"梳理一个想法", "一起看看代码", "排查一个问题"} {
								ui.ButtonBase(c).Label(a.tr(suggestion)).Gap(8).Radius(6).TextColor(c.Theme().TextMuted).Padding(8, 12).Border(1, c.Theme().Border).Children(func() { a.text(c, suggestion); icon(c, "arrow-right") }).OnClick(func() {
									if s.Draft == "" {
										s.Draft = suggestion
									}
									a.ReturnToComposer = true
								})
							}
						})
					}
				})
			}
			a.conversationMessages(c, s)
			if a.Live != nil {
				a.runResults(c, s)
				a.recoveryView(c, s)
			}
		})
	})
	if s.Scroll.MaxY > 0 && s.Scroll.Y < s.Scroll.MaxY-48 {
		ui.Overlay(c, func() {
			flatButton(c, "chevron-down", "回到最新").AttachTo(scroll, ui.AnchorBottom, ui.AnchorBottom).Margin(0, 0, 12, 0).Background(c.Theme().Surface).Border(1, c.Theme().Border).OnClick(func() { follow(s) })
		})
	}
}
func (a *preview) tool(c *ui.Context, m *message) {
	if a.Live != nil {
		a.nativeTool(c, m)
		return
	}
	ui.Column(c).MinWidth(0).Gap(8).Children(func() {
		label := "展开工具结果"
		if m.Expanded {
			label = "收起工具结果"
		}
		if a.Live != nil {
			a.musicCard(c, m)
		}
		e := ui.ButtonBase(c).Label(label).Justify(ui.Start).Gap(7).Padding(7, 9).Radius(6).TextColor(c.Theme().TextMuted).Background(c.Theme().Surface).FontSize(12).OnClick(func() { m.Expanded = !m.Expanded })
		e.Children(func() {
			if m.Expanded {
				icon(c, "chevron-down")
			} else {
				icon(c, "chevron-right")
			}
			stateIcon := "check"
			if m.Kind == "thought" || m.Status == "执行中" {
				stateIcon = "activity"
			}
			if m.Status == "失败" {
				stateIcon = "x"
			}
			state := ui.Row(c).AlignItems(ui.Center)
			if m.Status == "失败" {
				state.TextColor(c.Theme().Danger)
			}
			state.Children(func() { icon(c, stateIcon) })
			label := m.Tool
			if m.Status != "" {
				label += " · " + m.Status
			}
			ui.Text(c, label).MaxLines(1).MinWidth(0)
		})
		if m.Expanded {
			ui.Scroll(c).MaxHeight(360).Padding(10).Background(c.Theme().Surface).Radius(6).Children(func() { ui.Text(c, m.Text+"\n"+m.Result).Font("monospace").FontSize(12).LineHeight(1.7).Selectable() })
		}
	})
}
func (a *preview) composer(c *ui.Context) {
	if a.Live != nil {
		a.liveComposer(c)
		return
	}
	s := a.current()
	ui.Column(c).Padding(8, 20, 14, 20).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(776).MinWidth(0).Margin(0, ui.Auto).Gap(9).Children(func() {
			if s.Pending {
				ui.Column(c).Padding(12, 14).Gap(9).Background(c.Theme().Surface).Border(1, ui.Hex("#fbbf24").Alpha(.3)).Radius(8).Children(func() {
					ui.Row(c).Gap(7).Children(func() { icon(c, "shield"); a.text(c, "工具需要你的批准 · 演示").Bold().FontSize(13) })
					a.text(c, "go test ./...  ·  不会执行真实命令").Font("monospace").FontSize(12).TextColor(c.Theme().TextMuted)
					ui.Row(c).Gap(8).Children(func() {
						flatButton(c, "check", a.tr("允许一次")).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).OnClick(func() { a.approve(true) })
						flatButton(c, "x", a.tr("拒绝")).Border(1, c.Theme().Border).OnClick(func() { a.approve(false) })
					})
				})
			}
			composerBox := ui.Column(c).Label("输入面板").Padding(13, 14, 10, 14).Gap(6).Radius(12).Background(composerSurface(c)).Border(1, c.Theme().Accent.Alpha(.32))
			composerBox.Children(func() {
				input := ui.TextAreaBase(c.Key(fmt.Sprintf("draft-%d", s.ID)), &s.Draft).Bind(&a.Composer).Lines(2, 6).MinWidth(0).FontSize(14).LineHeight(1.75).Label("消息输入").Placeholder("发送消息，输入 / 查看命令…").Disabled(s.Archived)
				composing := input.Composing()
				if input.Changed() && !composing {
					a.SlashMenu = strings.HasPrefix(s.Draft, "/") && !strings.ContainsAny(s.Draft, " \n") && strings.HasPrefix("/approval", s.Draft)
				}
				input.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind == ui.InputKeyDown && !composing && a.SlashMenu && ev.Key == ui.KeyTab {
						a.completeCommand(s)
						return true
					}
					if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0 && !composing {
						if a.SlashMenu && strings.HasPrefix(s.Draft, "/") && !strings.Contains(s.Draft, " ") {
							a.completeCommand(s)
							return true
						}
						a.submit()
						return true
					}
					return false
				})
				ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
					iconButton(c, "plus", a.tr("附件尚未接入此预览")).Disabled(true)
					permission := flatButton(c.Key("permission-trigger"), "shield", a.Permission).OnClick(func() { a.toggleMenu(&a.PermissionMenu) })
					a.menuPanel(c, permission, &a.PermissionMenu, func() {
						menuCaption(c, "权限模式 · 演示")
						ui.Column(c).FocusGroup(ui.Vertical).Gap(2).Children(func() {
							for _, mode := range []string{"每次询问", "完全权限"} {
								selected := a.Permission == mode
								option := menuOption(c.Key(mode), "shield", mode, selected).Label("权限：" + mode)
								if selected {
									option.AutoFocus()
								}
								option.OnClick(func() { a.Permission = mode; a.PermissionMenu = false; a.Composer.Focus() })
							}
						})
						ui.Divider(c).Margin(3, 8)
						menuCaption(c, "仅切换界面，工具仍需审批")
					})
					flatButton(c, "activity", "10.1%").Disabled(true).Tooltip("演示上下文占用")
					model := flatButton(c.Key("model-trigger"), "cpu", s.Model).OnClick(func() { a.toggleMenu(&a.ModelMenu); a.ModelSearch = "" })
					a.menuPanel(c, model, &a.ModelMenu, func() {
						menuCaption(c, "选择模型 · 演示")
						ui.TextInput(c.Key("model-search"), &a.ModelSearch).Label("搜索模型").Placeholder("搜索模型…").AutoFocus().Background(c.Theme().Surface).Border(1, c.Theme().Border).Padding(7, 8).FontSize(12)
						ui.Column(c).FocusGroup(ui.Vertical).Gap(2).Children(func() {
							matches := 0
							for _, name := range []string{"coding", "glm-5.3-flash", "deepseek-flash"} {
								if strings.Contains(name, strings.ToLower(a.ModelSearch)) {
									matches++
									menuOption(c.Key(name), "cpu", name, s.Model == name).OnClick(func() { s.Model = name; a.ModelMenu = false; a.Composer.Focus() })
								}
							}
							if matches == 0 {
								menuCaption(c, "没有匹配的模型")
							}
						})
					})
					flatButton(c, "", "/").Label("斜杠命令").OnClick(func() { a.toggleMenu(&a.SlashMenu) })
					ui.Spacer(c)
					iconButton(c, "mic", a.tr("语音输入尚未接入此预览")).Disabled(true)
					if s.Running {
						iconButton(c, "stop", a.tr("停止输出")).Size(36, 36).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).OnClick(a.stop)
					} else {
						iconButton(c, "send", a.tr("发送消息")).Size(36, 36).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Disabled(strings.TrimSpace(s.Draft) == "" || s.Pending || s.Archived || a.SlashMenu).OnClick(a.submit)
					}
				})
			})
			// Command discovery belongs to the input panel, not the toolbar icon.
			popup := a.menuPanel(c, composerBox, &a.SlashMenu, func() {
				ui.Row(c).Padding(5, 8).Children(func() {
					a.text(c, "命令").FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Spacer(c)
					a.text(c, "Tab 补全 · Esc 关闭").FontSize(11).TextColor(c.Theme().TextMuted)
				})
				choice := ui.ButtonBase(c).Label("/approval  演示工具审批").Selected(true).FillWidth().Padding(10, 10).Gap(10).Justify(ui.Start).Radius(5).Background(c.Theme().Accent.Alpha(.1)).TextColor(c.Theme().Accent)
				choice.Children(func() {
					icon(c, "shield")
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, "/approval").Font("monospace").FontSize(13).SingleLine()
						a.text(c, "演示工具审批").FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
					})
					ui.Text(c, "Tab").FontSize(11).TextColor(c.Theme().TextMuted)
				})
				choice.OnClick(func() { a.completeCommand(s); a.Composer.Focus() })
			})
			if popup.Valid() {
				popup.Label("命令菜单").AttachTo(composerBox, ui.AnchorTopLeft, ui.AnchorBottomLeft).Width(360).Margin(0, 0, 8, 0)
			}

			footer := "Enter 发送 · Shift+Enter 换行"
			if s.Archived {
				footer = "会话已归档 · 在会话操作中恢复后继续"
			}
			ui.Row(c).Children(func() {
				ui.Text(c, footer).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Spacer(c)
				a.text(c, "⌘ N 新会话").FontSize(11).TextColor(c.Theme().TextMuted)
			})
		})
	})
}
func (a *preview) workbench(c *ui.Context) {
	if a.Live != nil {
		a.liveWorkbench(c)
		return
	}
	ui.Column(c).Width(270).Background(c.Theme().Surface).BorderWidth(0, 0, 0, 1).BorderColor(c.Theme().Border).Children(func() {
		ui.Row(c).Height(52).Padding(0, 14).AlignItems(ui.Center).Children(func() {
			a.text(c, "工作台").Bold().FontSize(13)
			ui.Spacer(c)
			iconButton(c, "x", a.tr("关闭工作台")).OnClick(func() { a.Workbench = false })
		})
		ui.Column(c).Padding(14).Gap(13).Children(func() {
			a.text(c, "文件 · 演示目录").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Row(c).Gap(6).Children(func() { icon(c, "chevron-down"); icon(c, "folder"); ui.Text(c, "desktop-native").FontSize(12) })
			for _, name := range []string{"main.go", "view.go", "state.go", "markdown.go"} {
				ui.Row(c).Padding(0, 20).Gap(7).Children(func() { icon(c, "file"); ui.Text(c, name).FontSize(12) })
			}
			ui.Divider(c)
			a.text(c, "此页先验证会话体验。").FontSize(12).TextColor(c.Theme().TextMuted)
			a.text(c, "文件编辑、终端和真实服务连接留待确认迁移后接入。").FontSize(12).LineHeight(1.6).TextColor(c.Theme().TextMuted)
		})
	})
}

func (a *preview) conversationMessage(key *ui.Context, s *session, i int, m *message) {
	c := key
	if m.User {
		ui.Row(key).Children(func() {
			ui.Spacer(key)
			ui.Column(key).MaxWidthPercent(85).MinWidth(0).Gap(6).Children(func() {
				ui.Text(key, m.Text).Padding(12, 18).Background(c.Theme().Accent.Alpha(.12)).Radius(12).LineHeight(1.75)
				if a.Live != nil {
					a.messageImages(key, m)
					ui.Row(key).Gap(3).Children(func() {
						iconButton(key, "copy", "复制用户消息").OnClick(func() { c.WriteClipboard(m.Text) })
						iconButton(key, "git-branch", "编辑并创建分支").Disabled(s.Running).OnClick(func() { a.forkUserMessage(i) })
					})
				}
			})
		})
	} else if m.Kind == "error" {
		ui.Text(key, m.Text).TextColor(c.Theme().Danger).FontSize(13).Role(ui.RoleStatus)
	} else if m.Tool != "" {
		a.tool(key, m)
	} else {
		ui.Column(key).Gap(10).MinWidth(0).Children(func() {
			ui.Row(key).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(key, "ea·").Label("回复作者").TextColor(c.Theme().Accent).FontSize(19).FontWeight(600)
				ui.Text(key, "EasyAgent").FontSize(11).TextColor(c.Theme().TextMuted)
			})
			if m.Text == "" {
				a.text(key, "正在生成…").TextColor(c.Theme().TextMuted)
			} else {
				a.renderSessionMarkdown(key, &m.Markdown, m.Text, s, "", s.Running && i == len(s.Messages)-1)
			}
			if !(s.Running && i == len(s.Messages)-1) {
				ui.Row(key).Gap(3).Children(func() {
					iconButton(key, "copy", fmt.Sprintf("复制回复 %d", i)).OnClick(func() { c.WriteClipboard(m.Text); c.Toast("已复制回复") })
					iconButton(key, "git-branch", fmt.Sprintf("从回复 %d 创建分支", i)).Disabled(a.Live != nil && (m.EntryID == "" || s.Running)).OnClick(func() { a.fork(i) })
				})
			}
		})
	}
}

func selectedButton(c *ui.Context, el ui.Element, selected bool) ui.Element {
	el.Selected(selected)
	if selected {
		el.Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent)
	}
	return el
}
func tabButton(c *ui.Context, label string, selected bool) ui.Element {
	return selectedButton(c, flatButton(c, "", label), selected)
}
