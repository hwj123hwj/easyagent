package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *preview) liveNotices(c *ui.Context) {
	l := a.Live
	if l.Error == "" && l.Notice == "" {
		return
	}
	ui.Overlay(c, func() {
		ui.Row(c).Absolute().Top(48).Left(0).Right(0).Gap(12).Padding(7, 20).Background(c.Theme().Surface).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
			text := l.Notice
			color := c.Theme().Accent
			if l.Error != "" {
				text = l.Error
				color = c.Theme().Danger
			}
			ui.Text(c, text).FontSize(12).TextColor(color).Grow(1).MinWidth(0).Role(ui.RoleStatus)
			if !l.Connected {
				a.action(c, "重连").Disabled(l.Connecting).OnClick(a.connectLive)
			}
			iconButton(c, "x", "关闭提示").OnClick(func() { l.Error = ""; l.Notice = "" })
		})
	})
}
func (a *preview) liveChrome(c *ui.Context) {
	l := a.Live
	bar := c.TitleBar()
	viewportWidth, _ := c.Size()
	sidebarVisible := a.Sidebar && (!a.Workbench || viewportWidth >= 1100)
	ui.Row(c).Height(48).Padding(1, max(bar.Right, 14)+6, 0, max(bar.Left, 76)+6).Gap(10).AlignItems(ui.Center).DragWindow().BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		if !a.Settings.Open {
			iconButton(c, "panel", "切换会话侧栏").OnClick(func() {
				if a.Workbench && viewportWidth < 1100 {
					a.Workbench = false
					a.Sidebar = true
				} else {
					a.Sidebar = !a.Sidebar
				}
			})
		}
		ui.Text(c, "ea·").Label("品牌标记").Top(-3).FontSize(24).FontWeight(600).LineHeight(1).TextColor(c.Theme().Accent)
		ui.Text(c, "EasyAgent").Label("应用名称").Top(-1).FontSize(13).FontWeight(600).LineHeight(1)
		ui.Box(c).Label("标题栏分隔线").Height(18).Width(1).Background(c.Theme().Border).Margin(0, 4)
		host := ui.ButtonBase(c).Label(a.Settings.host().Name).Gap(8).Padding(6, 8).Radius(6).FontSize(12).TextColor(c.Theme().Text).MaxWidth(180).MinWidth(60).Disabled(l.Connecting).OnClick(func() { a.toggleMenu(&a.HostMenu) })
		if host.Hovered() {
			host.Background(c.Theme().SurfaceHover)
		}
		host.Children(func() {
			dot := c.Theme().TextMuted
			if l.Connected {
				dot = c.Theme().Accent
			}
			ui.Text(c, "●").FontSize(9).TextColor(dot)
			ui.Text(c, a.Settings.host().Name).SingleLine().MinWidth(0)
			ui.Icon(c, icons["chevron-down"]).FontSize(12)
		})
		popup := a.menuPanel(c, host, &a.HostMenu, func() {
			menuCaption(c, "运行主机")
			ui.Column(c).FocusGroup(ui.Vertical).Gap(2).Children(func() {
				for i, h := range l.Config.Hosts {
					selected := l.Config.ActiveHost == h.ID
					option := menuOption(c.Key(h.ID), "laptop", h.Name, selected)
					if selected {
						option.AutoFocus()
					}
					option.OnClick(func() { a.HostMenu = false; a.selectLiveHost(i) })
				}
				ui.Divider(c)
				menuOption(c, "settings", "管理运行主机", false).OnClick(func() { a.HostMenu = false; a.openSettings("connections") })
			})
		})
		if popup.Valid() {
			popup.Label("运行主机").Width(272)
		}
		caption := a.connectionLabel()
		if l.Connected {
			caption = "本地已连接"
			if !a.Settings.host().Local {
				caption = "远程已连接"
			}
		}
		ui.Text(c, caption).FontSize(11).TextColor(c.Theme().TextMuted)

		ui.Spacer(c)
		if !a.Settings.Open {
			iconButton(c, "terminal", "打开终端").OnClick(a.toggleTerminal)
			iconButton(c, "panel-right", "切换工作台").OnClick(func() {
				a.Workbench = !a.Workbench
				if a.Workbench {
					a.openResource("files")
				}
			})
			if !sidebarVisible {
				iconButton(c, "settings", "设置").OnClick(func() { a.openSettings("general") })
			}
		}
	})
}
func (a *preview) liveSidebar(c *ui.Context) {
	l := a.Live
	ui.Column(c).Width(260).Background(c.Theme().Surface).BorderWidth(0, 1, 0, 0).BorderColor(c.Theme().Border).Children(func() {
		ui.Column(c).Padding(14).Gap(12).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				a.text(c, "对话").Bold()
				a.note(c, fmt.Sprint(len(a.Sessions)))
				ui.Spacer(c)
				iconButton(c, "refresh", "刷新会话").OnClick(func() { a.refreshLiveSessions("") })
			})
			ui.Row(c).Gap(6).Children(func() {
				group := ui.Row(c).Grow(1).Gap(0).AlignItems(ui.Stretch)
				group.Children(func() {
					flatButton(c, "plus", "新建会话").Grow(1).Height(36).Radius(6, 0, 0, 6).Padding(8, 10).TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.07)).Border(1, c.Theme().Accent.Alpha(.28)).Disabled(!l.Connected || l.configBusy() || l.Loading["create-session"]).OnClick(a.newSession)
					flatButton(c, "chevron-down", "").Label("选择模式").Tooltip("选择模式").Width(26).Height(36).Padding(8, 6).Radius(0, 6, 6, 0).BorderWidth(1, 1, 1, 0).BorderColor(c.Theme().Border).Background(c.Theme().Surface).Disabled(!l.Connected || l.configBusy() || l.Loading["create-session"]).OnClick(func() { a.toggleMenu(&a.NewMenu) })
				})
				popup := a.menuPanel(c, group, &a.NewMenu, func() {
					for _, item := range []struct{ Name, Icon, Application string }{{"编程", "cpu", ""}, {"音乐", "sparkle", "music"}, {"知识库", "search", "kb"}} {
						menuOption(c, item.Icon, item.Name, false).OnClick(func() { a.NewMenu = false; a.createLiveSessionIn("", a.current().Workspace, item.Application) })
					}
				})
				if popup.Valid() {
					popup.Width(group.Bounds().W)
				}
				iconButton(c, "folder", "选择项目目录").Bind(&l.FolderButton).Disabled(!l.Connected || l.configBusy() || l.Loading["create-session"] || l.Loading["folder-picker"]).OnClick(a.openWorkspacePicker)
			})
			ui.Row(c).Height(32).Gap(7).Padding(0, 9).Background(searchSurface(c)).Border(1, c.Theme().Border).Radius(6).AlignItems(ui.Center).Children(func() {
				icon(c, "search")
				ui.TextInputBase(c, &a.Search).Label("搜索会话").Placeholder("搜索会话…").FontSize(12).Grow(1).MinWidth(0)
			})
			ui.Row(c).Gap(14).Children(func() {
				flatButton(c, "", "最近").TextColor(func() ui.Color {
					if !a.Archived {
						return c.Theme().Accent
					}
					return c.Theme().TextMuted
				}()).OnClick(func() { a.Archived = false })
				flatButton(c, "", "已归档").TextColor(func() ui.Color {
					if a.Archived {
						return c.Theme().Accent
					}
					return c.Theme().TextMuted
				}()).OnClick(func() { a.Archived = true })
			})
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, 10).Children(func() {
			groups := []string{}
			byGroup := map[string][]*session{}
			for _, s := range a.Sessions {
				if s.Archived != a.Archived {
					continue
				}
				text := s.Title
				for _, m := range s.Messages {
					text += "\n" + m.Text
				}
				if !strings.Contains(strings.ToLower(text), strings.ToLower(a.Search)) {
					continue
				}
				group := s.Workspace
				if group == "" {
					group = localHostName()
				}
				if _, ok := byGroup[group]; !ok {
					groups = append(groups, group)
				}
				byGroup[group] = append(byGroup[group], s)
			}
			for _, group := range groups {
				groupKey := l.Config.ActiveHost + "/" + group
				collapsed := l.CollapsedProjects[groupKey]
				ui.Row(c.Key(groupKey)).MinWidth(0).Children(func() {
					ui.ButtonBase(c).Grow(1).MinWidth(0).Label("折叠项目："+filepath.Base(group)).Gap(6).Padding(10, 7, 6, 7).Justify(ui.Start).OnClick(func() { l.CollapsedProjects[groupKey] = !l.CollapsedProjects[groupKey] }).Children(func() {
						if collapsed {
							icon(c, "chevron-right")
						} else {
							icon(c, "chevron-down")
						}
						icon(c, "folder")
						title := filepath.Base(group)
						ui.Text(c, title).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).MaxLines(1)
						ui.Text(c, fmt.Sprint(len(byGroup[group]))).FontSize(11).Padding(1, 5).Radius(8).Background(c.Theme().SurfaceHover).TextColor(c.Theme().TextMuted)
					})
					if group != localHostName() {
						iconButton(c, "plus", "在项目中新建对话："+filepath.Base(group)).Size(24, 28).Disabled(!l.Connected || l.Loading["create-session"]).OnClick(func() { a.createLiveSessionIn("", group, a.current().Application) })
					}
				})
				if collapsed {
					continue
				}
				for _, s := range byGroup[group] {
					a.sessionRow(c, s)
				}
			}
			if len(groups) == 0 {
				a.note(c, "没有匹配的会话")
			}
		})
		ui.Column(c).Padding(12).Gap(6).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
			ui.Row(c).Gap(10).Children(func() {
				flatButton(c, "settings", "设置").Justify(ui.Start).OnClick(func() { a.openSettings("general") })
				flatButton(c, "chat", "飞书").OnClick(func() { a.openSettings("feishu") })
			})

		})
	})
}
func (a *preview) liveToolbar(c *ui.Context) {
	s := a.current()
	l := a.Live
	ui.Row(c).Height(52).Padding(0, 20).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.Title).FontSize(14).FontWeight(500).MinWidth(0).SingleLine()
			ui.Row(c).MaxWidth(160).MinWidth(0).Gap(5).AlignItems(ui.Center).TextColor(c.Theme().TextMuted).Children(func() {
				icon(c, "folder")
				ui.Text(c, s.Project).FontSize(11).SingleLine().MinWidth(0).Tooltip(s.Workspace)
			})
		})
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "●").FontSize(9).TextColor(c.Theme().Accent)
			ui.Text(c, s.statusText()).FontSize(11).TextColor(c.Theme().TextMuted)
		})
		more := iconButton(c, "menu", "会话操作").OnClick(func() { a.toggleMenu(&a.MoreMenu) })
		a.menuPanel(c, more, &a.MoreMenu, func() {
			menuOption(c, "", "重命名会话", false).Disabled(s.RemoteID == "").OnClick(func() { a.renameNativeSession(s) })
			label := "置顶会话"
			if s.Pinned {
				label = "取消置顶"
			}
			menuOption(c, "pin", label, false).Disabled(s.RemoteID == "").OnClick(func() { a.patchLiveSession(map[string]any{"pinned": !s.Pinned}) })
			label = "归档会话"
			if s.Archived {
				label = "恢复会话"
			}
			menuOption(c, "archive", label, false).Disabled(s.RemoteID == "" || s.Running).OnClick(func() { a.patchLiveSession(map[string]any{"archived": !s.Archived}) })
			menuOption(c, "copy", "复制会话文本", false).OnClick(func() {
				c.WriteClipboard(copyNativeConversation(s))
				a.MoreMenu = false
			})
			menuCaption(c, "显示密度")
			for _, choice := range []struct{ id, label string }{{"summary", "简洁"}, {"normal", "标准"}, {"verbose", "详细"}} {
				menuOption(c.Key(choice.id), "", choice.label, s.Density == choice.id || s.Density == "" && choice.id == "normal").OnClick(func() { s.Density = choice.id; a.MoreMenu = false })
			}
			menuOption(c, "search", "搜索当前会话", false).OnClick(func() { s.FindOpen = true; s.FindFocus = true; a.MoreMenu = false })
			menuOption(c, "activity", "检查上下文", false).Disabled(s.RemoteID == "").OnClick(func() { a.openResource("context"); a.MoreMenu = false })
			menuOption(c, "", "删除会话", false).Disabled(s.RemoteID == "" || s.Running || s.Pending).OnClick(func() {
				id := s.RemoteID
				l.Confirm = &nativeConfirmation{"删除会话？", s.Title + "；这会永久删除当前主机上的会话记录。", func() {
					a.operation("delete-session", "DELETE", sessionPath(id), nil, func(any) { a.refreshLiveSessions("") })
				}}
				a.MoreMenu = false
			})
		})
	})
}
func (a *preview) liveComposer(c *ui.Context) {
	s := a.current()
	l := a.Live
	ui.Column(c).Padding(8, 20, 14, 20).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(776).MinWidth(0).Margin(0, ui.Auto).Gap(9).Children(func() {

			if len(s.Confirmations) > 0 {
				a.liveApproval(c, s)
			}
			if len(s.Queue.Items) > 0 {
				ui.Column(c).Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
					ui.Row(c).Children(func() {
						label := "当前任务结束后执行"
						if s.Queue.Paused {
							label = "待发队列已暂停"
						}
						a.note(c, fmt.Sprintf(label+" · %d", len(s.Queue.Items)))
						ui.Spacer(c)
						label, act := "暂停队列", "pause"
						if s.Queue.Paused {
							label, act = "继续队列", "resume"
						}
						a.action(c, label).Disabled(l.Loading["queue/"+s.RemoteID]).OnClick(func() { a.queueAction(s, act, "", "") })
					})
					if err := l.ResourceErrors["queue/"+s.RemoteID]; err != "" {
						ui.Text(c, err).FontSize(11).TextColor(c.Theme().Danger)
					}
					for index, item := range s.Queue.Items {
						ui.Row(c.Key(item.ID)).Gap(6).Children(func() {
							ui.Text(c, fmt.Sprint(index+1)).FontSize(11).TextColor(c.Theme().TextMuted)
							if l.EditQueueOwner == s && l.EditQueueID == item.ID {
								id := item.ID
								ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
									ui.TextArea(c, &l.QueueDraft).Lines(2, 5).Label("编辑待发消息").AutoFocus().Disabled(l.Loading["queue/"+s.RemoteID])
									ui.Row(c).Gap(6).Children(func() {
										a.primary(c, "保存待发消息").Disabled(strings.TrimSpace(l.QueueDraft) == "" || l.Loading["queue/"+s.RemoteID]).OnClick(func() {
											a.queueAction(s, "edit", id, l.QueueDraft, func() {
												if l.EditQueueOwner == s && l.EditQueueID == id {
													l.EditQueueID = ""
													l.EditQueueOwner = nil
												}
											})
										})
										a.action(c, "取消编辑待发消息").Disabled(l.Loading["queue/"+s.RemoteID]).OnClick(func() { l.EditQueueID = ""; l.EditQueueOwner = nil })
									})
								})
								return
							}
							ui.Text(c, item.Prompt).Tooltip(item.Prompt).FontSize(12).MaxLines(2).Grow(1).MinWidth(0)
							if count := len(list(item.Inputs["attachments"])) + len(list(item.Inputs["files"])); count > 0 {
								a.note(c, fmt.Sprintf("%d 个上下文", count))
							}
							a.action(c, "编辑").Disabled(l.Loading["queue/"+s.RemoteID]).OnClick(func() { l.EditQueueID = item.ID; l.QueueDraft = item.Prompt; l.EditQueueOwner = s })
							a.action(c, "立即发送").Disabled(l.Loading["queue/"+s.RemoteID]).OnClick(func() { a.queueAction(s, "send_now", item.ID, "") })
							iconButton(c, "x", "移出队列").Disabled(l.Loading["queue/"+s.RemoteID]).OnClick(func() { a.queueAction(s, "remove", item.ID, "") })
						})
					}
				})
			}
			box := ui.Column(c).Label("输入面板").Padding(13, 14, 10, 14).Gap(6).Radius(12).Background(composerSurface(c)).Border(1, c.Theme().Accent.Alpha(.32))
			box.Children(func() {
				a.inputReferences(c, s)
				a.attachmentChips(c, s)
				input := ui.TextAreaBase(c.Key(fmt.Sprintf("draft-%d", s.ID)), &s.Draft).Bind(&a.Composer).Lines(2, 6).FontSize(14).LineHeight(1.75).MinWidth(0).Label("消息输入").Placeholder("描述你想完成的事…").Disabled(s.Archived)
				services := c.Services()
				input.OnPaste(func(string) bool { return a.pasteAttachments(services, s) })
				composing := input.Composing()
				if input.Changed() && !composing {
					l.CommandIndex = 0
					s.MentionIndex = 0
					a.SlashMenu = strings.HasPrefix(s.Draft, "/") && !strings.ContainsAny(s.Draft, " \n") && len(a.commandMatches(s.Draft)) > 0
				}
				input.OnTextKey(func(ev ui.TextKey) bool {
					if composing || a.current() != s || s.Archived {
						return false
					}
					s.Draft = ev.Text()
					if _, mention := mentionQuery(s.Draft); mention && s.MentionDismissed != s.Draft {
						matches := a.mentionMatches(s)
						if ev.Key == ui.KeyEscape {
							s.MentionDismissed = s.Draft
							return true
						}
						if len(matches) > 0 {
							if ev.Key == ui.KeyUp || ev.Key == ui.KeyDown {
								step := 1
								if ev.Key == ui.KeyUp {
									step = -1
								}
								s.MentionIndex = (s.MentionIndex + step + len(matches)) % len(matches)
								return true
							}
							if ev.Key == ui.KeyTab || ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0 {
								a.completeMention(s)
								ev.SetText(s.Draft)
								return true
							}
						}
					}
					if a.SlashMenu && (ev.Key == ui.KeyDown || ev.Key == ui.KeyUp) {
						count := len(a.commandMatches(s.Draft))
						if count > 0 {
							if ev.Key == ui.KeyDown {
								l.CommandIndex = (l.CommandIndex + 1) % count
							} else {
								l.CommandIndex = (l.CommandIndex + count - 1) % count
							}
						}
						return true
					}
					if a.SlashMenu && (ev.Key == ui.KeyTab || ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0) {
						a.completeLiveCommand()
						ev.SetText(s.Draft)
						return true
					}
					if ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0 {
						a.submit()
						ev.SetText(s.Draft)
						return true
					}
					return false
				})
				ui.Row(c).Gap(3).AlignItems(ui.Center).Wrap().Children(func() {
					iconButton(c, "plus", "添加附件").Disabled(!l.Connected || s.Archived || s.Sending || l.Loading["create-session"] || l.Loading["attachments/"+s.RemoteID]).OnClick(a.pickAttachments)
					permission := flatButton(c, "shield", a.livePermission()).Disabled(s.Running || s.Pending || s.Sending || l.configBusy()).OnClick(func() { a.toggleMenu(&a.PermissionMenu) })
					if a.livePermission() == "完全权限" {
						permission.TextColor(c.Theme().Warning)
					}
					a.menuPanel(c, permission, &a.PermissionMenu, func() {
						menuCaption(c, "工具权限")
						ui.Column(c).FocusGroup(ui.Vertical).Gap(2).Children(func() {
							for _, mode := range []string{"每次询问", "完全权限"} {
								selected := a.livePermission() == mode
								option := menuOption(c.Key(mode), "shield", mode, selected).Label("权限：" + mode).Disabled(l.Loading["permission/"+s.RemoteID])
								if selected {
									option.AutoFocus()
								}
								option.OnClick(func() { a.switchLivePermission(mode) })
								text := "有风险的操作执行前由你确认"
								if mode == "完全权限" {
									text = "自动批准普通工具的修改与命令"
								}
								ui.Text(c, text).Padding(0, 8, 6, 8).FontSize(11).TextColor(c.Theme().TextMuted)
							}
							a.note(c, "只影响当前会话。MCP 独立批准规则及工作区访问限制仍然生效；服务重启后恢复启动配置。")
						})
					})
					usage := "上下文"
					if window := number(s.Usage["context_window"]); window > 0 {
						usage = fmt.Sprintf("约 %.1f%%", 100*number(s.Usage["estimated_tokens"])/window)
					}
					usageButton := flatButton(c, "activity", usage).Disabled(len(s.Usage) == 0).OnClick(func() { a.toggleMenu(&a.ContextMenu) })
					usagePopup := a.menuPanel(c, usageButton, &a.ContextMenu, func() { a.contextUsagePanel(c, s) })
					if usagePopup.Valid() {
						usagePopup.Width(332).Padding(16)
					}
					name := s.Model
					if name == "" {
						name = "选择模型"
					}
					model := flatButton(c, "cpu", name).MaxWidth(180).MinWidth(60).Clip().Tooltip(name).Disabled(s.Running || s.Pending || s.Sending || l.configBusy()).OnClick(func() { a.toggleMenu(&a.ModelMenu); a.ModelSearch = ""; a.refreshModels() })
					a.liveModelPicker(c, model)
					flatButton(c, "", "/").Label("斜杠命令").OnClick(func() { a.toggleMenu(&a.SlashMenu) })
					ui.Spacer(c)
					if l.Voice != nil {
						ui.Text(c, fmt.Sprintf("录音 %ds", int(time.Since(l.VoiceStarted).Seconds()))).FontSize(11).TextColor(c.Theme().Danger)
						iconButton(c, "x", "取消录音").OnClick(a.cancelVoice)
						iconButton(c, "stop", "结束录音并转写").OnClick(a.startVoice)
					} else {
						iconButton(c, "mic", "语音输入").Disabled(!l.Connected || s.Archived || l.Loading["voice"]).OnClick(a.startVoice)
					}
					if s.Running {
						iconButton(c, "stop", "停止任务").Size(34, 34).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).OnClick(a.stop)
					}
					label := "发送消息"
					if s.Running || s.Pending {
						label = "加入发送队列"
					}
					iconButton(c, "send", label).Size(36, 36).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Disabled(!l.Connected || strings.TrimSpace(s.Draft) == "" || s.Sending || s.Archived || l.Loading["queue-add/"+s.RemoteID] || l.Loading["attachments/"+s.RemoteID] || l.Loading["voice"] || l.Voice != nil).OnClick(a.submit)
				})
			})
			if value, ok := ui.Drop[fileReference](box); ok {
				a.addFileReference(s, value)
			}
			a.promptFileMentions(c, box, s)
			if paths := box.DroppedFiles(); len(paths) > 0 {
				a.attachNativeFiles(s, paths)
			}
			popup := a.menuPanel(c, box, &a.SlashMenu, func() {
				menuCaption(c, "命令 · Tab 补全 · Esc 关闭")
				ui.Scroll(c).MaxHeight(300).Children(func() {
					for index, cmd := range a.commandMatches(s.Draft) {
						menuOption(c.Key(cmd.Name), "", cmd.Name+" · "+cmd.Description, l.CommandIndex == index).OnClick(func() { s.Draft = cmd.Name + " "; a.SlashMenu = false; a.ReturnToComposer = true })
					}
				})
			})
			if popup.Valid() {
				popup.Label("命令菜单").AttachTo(box, ui.AnchorTopLeft, ui.AnchorBottomLeft).Width(max(240, box.Bounds().W)).Margin(0, 0, 8, 0)
			}
			ui.Row(c).Gap(10).Children(func() {
				ui.Text(c, "Enter 发送 · Shift+Enter 换行").FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Spacer(c)
				ui.Text(c, "⌘ N 新会话").FontSize(11).TextColor(c.Theme().TextMuted)
			})
		})
	})
}
func (a *preview) queueAction(s *session, action, id, text string, done ...func()) {
	if s == nil || a.Live.configBusy() {
		a.Live.Error = "正在应用连接配置，请稍后操作队列"
		return
	}
	a.operation("queue/"+s.RemoteID, "POST", sessionPath(s.RemoteID)+"/queue", map[string]any{"action": action, "id": id, "prompt": text}, func(v any) {
		raw, _ := json.Marshal(v)
		var next messageQueue
		if json.Unmarshal(raw, &next) == nil && next.Revision >= s.Queue.Revision {
			s.Queue = next
		}
		if len(done) > 0 {
			done[0]()
		}
	})
}
func (a *preview) liveApproval(c *ui.Context, s *session) {
	value := s.Confirmations[0]
	l := a.Live
	ui.Column(c).Padding(12, 14).Gap(8).Background(c.Theme().Surface).Border(1, c.Theme().Accent.Alpha(.4)).Radius(8).Children(func() {
		_, hasQuestions := object(value.Args)["questions"]
		if hasQuestions || value.ToolName == "ask_user_question" {
			a.text(c, "需要你回答的问题").Bold()
			a.questionFields(c, s, value)
		} else {
			a.text(c, "工具需要你的批准").Bold()
			a.note(c, value.ToolName)
			a.note(c, value.Description)
			if value.Args != nil {
				ui.Text(c, pretty(value.Args)).Font("monospace").FontSize(12).MaxLines(5)
			}
			ui.Row(c).Gap(8).Children(func() {
				a.primary(c, "允许一次").Disabled(l.Loading["confirm/"+s.RemoteID]).OnClick(func() { a.confirmLive(true) })
				a.action(c, "拒绝").Disabled(l.Loading["confirm/"+s.RemoteID]).OnClick(func() { a.confirmLive(false) })
			})
		}
	})
}
func (a *preview) liveModals(c *ui.Context) {
	a.marketLightbox(c)
	a.imageLightbox(c)
	l := a.Live
	confirm := l.Confirm != nil
	ui.Modal(c, &confirm, func() {
		if l.Confirm == nil {
			return
		}
		value := l.Confirm
		ui.Text(c, value.Title).FontSize(18).Bold()
		a.note(c, value.Description)
		ui.Row(c).Gap(10).Children(func() {
			a.action(c, "取消").OnClick(func() { l.Confirm = nil })
			a.primary(c, "确认").OnClick(func() { l.Confirm = nil; value.Action() })
		})
	})
	if !confirm {
		l.Confirm = nil
	}
	ui.Modal(c, &l.Rename, func() {
		a.heading(c, "重命名会话")
		field := ui.TextInput(c, &l.TitleInput).Label("会话名称").Placeholder("输入会话名称").AutoFocus()
		if field.Submitted() {
			a.saveRename()
		}
		ui.Row(c).Gap(8).Children(func() {
			a.action(c, "取消").OnClick(func() { l.Rename = false })
			a.primary(c, "保存").Disabled(strings.TrimSpace(l.TitleInput) == "").OnClick(a.saveRename)
		})
	})
	a.workspacePathDialog(c)
}

type nativeCommand struct{ Name, Description string }

var nativeCommands = []nativeCommand{{"/help", "使用帮助"}, {"/new", "新建会话"}, {"/model", "切换模型"}, {"/sessions", "历史会话"}, {"/tools", "可用工具"}, {"/context", "检查上下文"}, {"/compact", "压缩上下文"}, {"/stop", "停止任务"}, {"/mcp", "MCP 工具"}, {"/settings", "工作区设置"}}

func (a *preview) commandMatches(text string) []nativeCommand {
	if !strings.HasPrefix(text, "/") {
		return nativeCommands
	}
	var found []nativeCommand
	for _, cmd := range nativeCommands {
		if strings.Contains(cmd.Name+cmd.Description, text) {
			found = append(found, cmd)
		}
	}
	return found
}
func (a *preview) completeLiveCommand() {
	matches := a.commandMatches(a.current().Draft)
	if len(matches) > 0 {
		a.current().Draft = matches[min(a.Live.CommandIndex, len(matches)-1)].Name + " "
	}
	a.SlashMenu = false
	a.ReturnToComposer = true
}
func (a *preview) executeLiveCommand(text string) bool {
	parts := strings.SplitN(text, " ", 2)
	name := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	s := a.current()
	l := a.Live
	switch name {
	case "/settings":
		a.openSettings("general")
	case "/mcp":
		a.Settings.Workspace = s.Workspace
		a.openSettings("mcp")
	case "/new":
		a.newSession()
	case "/sessions":
		a.Sidebar = !a.Sidebar
	case "/stop":
		a.cancelLive()
	case "/context":
		a.openResource("context")
	case "/tools":
		a.openResource("tools")
	case "/model":
		if args != "" {
			a.switchLiveModel(args)
		} else {
			a.ModelMenu = true
		}
	case "/compact":
		a.operation("compact", "POST", sessionPath(s.RemoteID)+"/compact", map[string]any{"custom_instructions": args}, func(any) { a.loadSession(s) })
	case "/help":
		l.Notice = "⌘N 新会话 · ⌘, 设置 · ⌘P 工作台 · /model 切换模型 · /compact 压缩上下文"
	default:
		original := s.Draft
		a.operation("command/"+s.RemoteID, "POST", sessionPath(s.RemoteID)+"/command", map[string]any{"command": text}, func(v any) {
			m := object(v)
			if output := str(m["output"]); output != "" {
				s.Messages = append(s.Messages, &message{Text: output})
			}
			if id := str(m["session_id"]); id != "" {
				if a.current() == s {
					a.refreshLiveSessions(id)
				} else {
					a.refreshLiveSessions("")
				}
			}
			if boolean(m["should_query"]) {
				a.sendLivePrompt(s, str(m["query_prompt"]), original, nil)
			} else if s.Draft == original {
				s.Draft = ""
			}
		})
		a.SlashMenu = false
		return true
	}
	if strings.TrimSpace(s.Draft) == text {
		s.Draft = ""
	}
	a.SlashMenu = false
	return true
}
