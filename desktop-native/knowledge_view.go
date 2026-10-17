package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *preview) profileWorkbench(c *ui.Context) {
	l := a.Live
	if a.resourceFeedback(c, "profile") {
		return
	}
	m := object(l.Data["profile"])
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(14).Children(func() {
		ui.Column(c).Gap(12).MinWidth(0).Children(func() {
			a.note(c, formatCount(m["total_facts"])+" 条记忆")
			if summary := str(m["summary"]); summary != "" {
				ui.Text(c, "注入 Agent 的摘要").FontSize(11).TextColor(c.Theme().TextMuted)
				renderMarkdown(c, summary)
			}
			categories := list(m["categories"])
			if len(categories) == 0 {
				a.note(c, "还没有记忆。你可以在对话中告诉 Agent 需要记住的偏好。")
			}
			for _, raw := range categories {
				category := object(raw)
				name := str(category["name"])
				box := ui.Column(c.Key(name)).Gap(8)
				open := ui.Local(box, "open", func() bool { return true })
				box.Children(func() {
					glyph := "chevron-right"
					if *open {
						glyph = "chevron-down"
					}
					flatButton(c, glyph, fmt.Sprintf("%s · %d", str(category["label"]), len(list(category["facts"])))).Justify(ui.Start).FillWidth().FontSize(12).Expanded(*open).OnClick(func() { *open = !*open })
					if !*open {
						return
					}
					for _, raw := range list(category["facts"]) {
						fact := object(raw)
						ui.Column(c.Key(str(fact["key"]))).Padding(10, 0).Gap(6).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
							ui.Row(c).Gap(8).Children(func() {
								ui.Text(c, str(fact["key"])).FontSize(12).FontWeight(600).Grow(1).MinWidth(0)
								iconButton(c, "delete", "移除记忆："+str(fact["key"])).Size(24, 24).OnClick(func() {
									l.Confirm = &nativeConfirmation{"移除这条记忆？", str(fact["value"]), func() {
										a.operation("profile-delete", "DELETE", "/profile", map[string]any{"category": name, "key": fact["key"]}, func(any) { a.resource("profile", "/profile") })
									}}
								})
							})
							ui.Text(c, str(fact["value"])).FontSize(12).Selectable().LineHeight(1.65)
							a.note(c, str(fact["source"])+" · "+str(fact["updated"]))
						})
					}
				})
			}
		})
	})
}
func (a *preview) openKBEntry(entry map[string]any) {
	w := &a.Live.Workbench
	path := str(entry["rel_path"])
	if path == "" {
		path = str(entry["path"])
	}
	w.KBPath = path
	w.KBEntry = entry
	a.resource("kb-read", "/kb/read"+query(map[string]string{"path": path}))
}
func (a *preview) kbWorkbench(c *ui.Context) {
	l, w := a.Live, &a.Live.Workbench
	stats := object(l.Data["kb-stats"])
	totalCount := formatCount(stats["total_entries"])
	catCount := formatCount(stats["categories"])
	tagCount := formatCount(stats["tags"])

	ui.Column(c).Padding(10, 12).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		ui.Row(c).Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(4).Children(func() {
				for _, tab := range []struct{ id, label, icon string }{
					{"", "浏览", "book"},
					{"tags", "标签", "tag"},
					{"health", "健康", "activity"},
				} {
					active := w.KBView == tab.id
					btn := ui.ButtonBase(c).Label(tab.label).Gap(5).Padding(5, 10).Radius(6).FontSize(12)
					if active {
						btn.Background(c.Theme().Accent.Alpha(.14)).TextColor(c.Theme().Accent)
					} else {
						btn.TextColor(c.Theme().TextMuted)
						if btn.Hovered() {
							btn.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
						}
					}
					btn.OnClick(func() {
						w.KBView = tab.id
						w.KBPath = ""
						w.KBEntry = nil
						a.refreshWorkbench()
					}).Children(func() {
						icon(c, tab.icon)
						ui.Text(c, tab.label)
					})
				}
			})
			ui.Spacer(c)
			ui.Row(c).Gap(4).Children(func() {
				ui.Text(c, totalCount+" 条目").SingleLine().FontSize(10).FontWeight(600).
					TextColor(c.Theme().TextMuted).Background(c.Theme().SurfaceHover).
					Radius(4).Padding(2, 6)
				ui.Text(c, catCount+" 分类").SingleLine().FontSize(10).FontWeight(600).
					TextColor(c.Theme().TextMuted).Background(c.Theme().SurfaceHover).
					Radius(4).Padding(2, 6)
				ui.Text(c, tagCount+" 标签").SingleLine().FontSize(10).FontWeight(600).
					TextColor(c.Theme().TextMuted).Background(c.Theme().SurfaceHover).
					Radius(4).Padding(2, 6)
			})
		})
	})
	if w.KBPath != "" {
		ui.Column(c).Padding(10, 14).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				flatButton(c, "arrow-left", "返回").Padding(4, 8).OnClick(func() {
					w.KBPath = ""
					w.KBEntry = nil
				})
				title := w.KBPath
				if w.KBEntry != nil && str(w.KBEntry["title"]) != "" {
					title = str(w.KBEntry["title"])
				}
				ui.Text(c, title).FontSize(14).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			})
			if w.KBEntry != nil {
				category := str(w.KBEntry["category"])
				tags := list(w.KBEntry["tags"])
				modified := str(w.KBEntry["modified"])
				ui.Row(c).Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
					if category != "" {
						ui.Text(c, category).FontSize(10).FontWeight(500).
							TextColor(c.Theme().TextMuted).Padding(2, 6).
							Background(c.Theme().SurfaceHover).Radius(4)
					}
					for _, rawTag := range tags {
						tagStr := str(rawTag)
						ui.ButtonBase(c).Label("标签："+tagStr).Gap(2).Padding(2, 6).Radius(4).
							Background(c.Theme().Accent.Alpha(.14)).
							OnClick(func() {
								w.KBTag = tagStr
								w.KBPath = ""
								w.KBEntry = nil
								w.KBView = ""
								a.refreshWorkbench()
							}).
							Children(func() {
								ui.Text(c, "#"+tagStr).FontSize(10).FontWeight(500).TextColor(c.Theme().Accent)
							})
					}
					if modified != "" {
						ui.Spacer(c)
						ui.Text(c, modified).FontSize(10).TextColor(c.Theme().TextMuted)
					}
				})
			}
		})
		if a.resourceFeedback(c, "kb-read") {
			return
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(14).Children(func() {
			renderMarkdown(c, str(object(l.Data["kb-read"])["content"]))
		})
		return
	}
	if w.KBView == "tags" {
		if a.resourceFeedback(c, "kb-tags") {
			return
		}
		tags := list(object(l.Data["kb-tags"])["tags"])
		if len(tags) == 0 {
			a.note(c, "暂无标签。")
		}
		w.KBTags.Key = func(i int) any { return str(object(tags[i])["name"]) }
		ui.GridView(c, &w.KBTags, len(tags), 170, 38, func(i int) {
			tag := object(tags[i])
			name := str(tag["name"])
			count := int(number(tag["count"]))
			btn := ui.ButtonBase(c.Key("tag-"+name)).Label("标签："+name).Gap(5).Padding(6, 12).Radius(14).
				Background(c.Theme().Surface).Border(1, c.Theme().Border).
				OnClick(func() {
					w.KBTag = name
					w.KBView = ""
					a.refreshWorkbench()
				})
			if btn.Hovered() {
				btn.Background(c.Theme().SurfaceHover).Border(1, c.Theme().Accent)
			}
			btn.Children(func() {
				ui.Text(c, "#"+name).SingleLine().Grow(1).MinWidth(0).FontSize(12).FontWeight(500).TextColor(c.Theme().Accent)
				ui.Text(c, formatCount(count)).FontSize(10).FontWeight(600).
					TextColor(c.Theme().TextMuted).Background(c.Theme().SurfaceHover).
					Radius(8).Padding(1, 6)
			})
		}).Grow(1).MinHeight(0).Padding(14).Gap(8)
		return
	}
	if w.KBView == "health" {
		a.kbHealthView(c)
		return
	}
	ui.Column(c).Padding(10, 12, 6, 12).Gap(8).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			input := ui.TextInput(c, &w.KBQuery).Label("搜索知识库").Placeholder("搜索条目…").FontSize(12).Padding(6, 8).Grow(1).MinWidth(0).OnSubmit(func() {
				w.KBSearchAt = time.Time{}
				a.refreshWorkbench()
			})
			if input.Changed() {
				w.KBSearchAt = time.Now().Add(250 * time.Millisecond)
			}

			if w.KBQuery != "" {
				iconButton(c, "x", "清除知识库搜索").Size(24, 24).OnClick(func() {
					w.KBQuery = ""
					w.KBSearchAt = time.Time{}
					a.refreshWorkbench()
				})
			}
			iconButton(c, "search", "搜索知识条目").OnClick(a.refreshWorkbench)
		})
		ui.Row(c).Wrap().Gap(5).Children(func() {
			allActive := w.KBCategory == ""
			allChip := ui.ButtonBase(c).Label("全部分类").Gap(4).Padding(3, 8).Radius(6).FontSize(11)
			if allActive {
				allChip.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
			} else {
				allChip.Background(c.Theme().Surface).Border(1, c.Theme().Border).TextColor(c.Theme().TextMuted)
				if allChip.Hovered() {
					allChip.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
				}
			}
			allChip.OnClick(func() { w.KBCategory = ""; a.refreshWorkbench() }).Children(func() {
				ui.Text(c, "全部")
			})

			for _, raw := range list(object(l.Data["kb-categories"])["categories"]) {
				category := object(raw)
				name := str(category["name"])
				count := int(number(category["count"]))
				active := w.KBCategory == name
				chip := ui.ButtonBase(c).Label("分类："+name).Gap(4).Padding(3, 8).Radius(6).FontSize(11)
				if active {
					chip.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
				} else {
					chip.Background(c.Theme().Surface).Border(1, c.Theme().Border).TextColor(c.Theme().TextMuted)
					if chip.Hovered() {
						chip.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
					}
				}
				chip.OnClick(func() {
					if w.KBCategory == name {
						w.KBCategory = ""
					} else {
						w.KBCategory = name
					}
					a.refreshWorkbench()
				}).Children(func() {
					ui.Text(c, name)
					if count > 0 {
						ui.Text(c, formatCount(count)).FontSize(9).Opacity(.75)
					}
				})
			}
			if w.KBTag != "" {
				flatButton(c, "x", "#"+w.KBTag).FontSize(11).Padding(3, 8).Radius(6).
					Background(c.Theme().Accent.Alpha(.14)).TextColor(c.Theme().Accent).
					OnClick(func() { w.KBTag = ""; a.refreshWorkbench() })
			}
		})
	})
	if a.resourceFeedback(c, "kb") {
		return
	}
	entries := list(object(l.Data["kb"])["entries"])
	if len(entries) == 0 {
		a.note(c, "没有符合条件的知识条目。")
	}
	ui.List(c, &w.KBList, len(entries)).Grow(1).MinHeight(0).Padding(10, 12).Gap(8).
		ItemKey(func(i int) any { return str(object(entries[i])["path"]) }).Rows(func(row ui.ListRow) {
		c := row.Context
		entry := object(entries[row.Index])
		title := str(entry["title"])
		if title == "" {
			title = filepath.Base(str(entry["rel_path"]))
		}
		tags := list(entry["tags"])
		category := str(entry["category"])
		summary := str(entry["summary"])
		modified := str(entry["modified"])

		card := ui.ButtonBase(c.Key(str(entry["path"]))).
			Label("知识条目："+title).
			FillWidth().MinWidth(0).Justify(ui.Start).
			Padding(10, 12).Radius(8).
			Background(c.Theme().Surface).
			Border(1, c.Theme().Border).
			OnClick(func() { a.openKBEntry(entry) })
		if card.Hovered() {
			card.Background(c.Theme().SurfaceHover)
		}
		card.Children(func() {
			ui.Column(c).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(600).SingleLine()
				if summary != "" {
					ui.Text(c, summary).FontSize(11).MaxLines(2).TextColor(c.Theme().TextMuted).LineHeight(1.5)
				}
				ui.Row(c).Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
					if category != "" {
						ui.Text(c, category).FontSize(10).FontWeight(500).TextColor(c.Theme().TextMuted)
					}
					if len(tags) > 0 {
						limit := min(3, len(tags))
						for _, rawTag := range tags[:limit] {
							tagStr := str(rawTag)
							ui.Text(c, tagStr).FontSize(9).FontWeight(500).
								TextColor(c.Theme().Accent).
								Background(c.Theme().Accent.Alpha(.14)).
								Radius(3).Padding(1, 5)
						}
						if len(tags) > 3 {
							ui.Text(c, fmt.Sprintf("+%d", len(tags)-3)).FontSize(9).TextColor(c.Theme().TextMuted)
						}
					}
					if modified != "" {
						ui.Spacer(c)
						ui.Text(c, modified).FontSize(10).TextColor(c.Theme().TextMuted)
					}
				})
			})
		})
	})
}
func (a *preview) kbHealthView(c *ui.Context) {
	if a.resourceFeedback(c, "kb-health") {
		return
	}
	m := object(a.Live.Data["kb-health"])
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(14).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			totalEntries := int(number(m["total_entries"]))
			categories := int(number(m["categories"]))
			tagsCount := int(number(m["tags"]))
			missingTitle := len(list(m["entries_missing_title"]))
			missingSummary := len(list(m["entries_missing_summary"]))
			missingTags := len(list(m["entries_missing_tags"]))
			duplicateGroups := len(list(m["duplicate_groups"]))
			tagClusters := len(list(m["tag_clusters"]))
			totalIssues := missingTitle + missingSummary + missingTags + duplicateGroups + tagClusters

			ui.Row(c).Gap(6).Children(func() {
				for _, stat := range []struct {
					label string
					value int
					warn  bool
				}{
					{"条目", totalEntries, false},
					{"分类", categories, false},
					{"标签", tagsCount, false},
					{"待优化", totalIssues, totalIssues > 0},
				} {
					card := ui.Column(c).Grow(1).Padding(8, 10).Gap(2).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border)
					card.Children(func() {
						val := ui.Text(c, formatCount(stat.value)).FontSize(15).FontWeight(600)
						if stat.warn {
							val.TextColor(c.Theme().Danger)
						}
						ui.Text(c, stat.label).FontSize(10).TextColor(c.Theme().TextMuted)
					})
				}
			})

			for _, group := range []struct{ key, label string }{
				{"entries_missing_title", "缺少标题"},
				{"entries_missing_summary", "缺少摘要"},
				{"entries_missing_tags", "缺少标签"},
			} {
				entries := list(m[group.key])
				nativeDisclosure(c, group.key, fmt.Sprintf("%s · %d", group.label, len(entries)), func() {
					if len(entries) == 0 {
						a.note(c, "没有发现问题。")
					}
					a.kbHealthEntries(c, entries)
				})
			}
			groups := list(m["duplicate_groups"])
			nativeDisclosure(c, "duplicates", fmt.Sprintf("重复标题 · %d 组", len(groups)), func() {
				if len(groups) == 0 {
					a.note(c, "没有发现重复标题。")
				}
				ui.List(c, nil, len(groups)).Height(float32(min(320, len(groups)*100))).Rows(func(row ui.ListRow) {
					c := row.Context
					group := object(groups[row.Index])
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, str(group["canonical_title"])).SingleLine().FontSize(12).FontWeight(600)
						a.kbHealthEntries(c, list(group["entries"]))
					})
				})
			})
			clusters := list(m["tag_clusters"])
			nativeDisclosure(c, "clusters", fmt.Sprintf("相似标签 · %d 组", len(clusters)), func() {
				if len(clusters) == 0 {
					a.note(c, "标签命名一致。")
				}
				ui.List(c, nil, len(clusters)).Height(float32(min(320, len(clusters)*36))).Rows(func(row ui.ListRow) {
					cluster := object(clusters[row.Index])
					ui.Text(row.Context, str(cluster["canonical"])+" · "+displayText(cluster["variants"])).SingleLine().FontSize(12)
				})
			})
		})
	})
}

// Expanded health findings can contain the entire library too.
func (a *preview) kbHealthEntries(c *ui.Context, entries []any) {
	ui.List(c, nil, len(entries)).Height(float32(min(260, len(entries)*36))).
		ItemKey(func(i int) any { return str(object(entries[i])["rel_path"]) }).Rows(func(row ui.ListRow) {
		c := row.Context
		entry := object(entries[row.Index])
		label := str(entry["title"]) + " " + str(entry["rel_path"])
		ui.ButtonBase(c).Label(label).FillWidth().MinWidth(0).Padding(8).OnClick(func() { a.openKBEntry(entry) }).Children(func() {
			ui.Text(c, label).SingleLine().FontSize(12)
		})
	})
}
