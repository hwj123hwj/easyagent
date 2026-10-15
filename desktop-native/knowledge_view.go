package main

import (
	"fmt"
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
	a.resource("kb-read", "/kb/read"+query(map[string]string{"path": path}))
}
func (a *preview) kbWorkbench(c *ui.Context) {
	l, w := a.Live, &a.Live.Workbench
	ui.Column(c).Padding(10, 12).Gap(10).Children(func() {
		stats := object(l.Data["kb-stats"])
		a.note(c, formatCount(stats["total_entries"])+" 条目 · "+formatCount(stats["categories"])+" 分类 · "+formatCount(stats["tags"])+" 标签")
		ui.Row(c).Gap(5).Children(func() {
			for _, tab := range []struct{ id, label string }{{"", "浏览"}, {"tags", "标签"}, {"health", "健康"}} {
				tabButton(c, tab.label, w.KBView == tab.id).FontSize(12).OnClick(func() { w.KBView = tab.id; w.KBPath = ""; a.refreshWorkbench() })
			}
		})
	})
	if w.KBPath != "" {
		flatButton(c, "arrow-left", "返回条目列表").Padding(6, 12).AlignSelf(ui.Start).OnClick(func() { w.KBPath = "" })
		if a.resourceFeedback(c, "kb-read") {
			return
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(14).Children(func() { renderMarkdown(c, str(object(l.Data["kb-read"])["content"])) })
		return
	}
	if w.KBView == "tags" {
		if a.resourceFeedback(c, "kb-tags") {
			return
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(12).Children(func() {
			tags := list(object(l.Data["kb-tags"])["tags"])
			if len(tags) == 0 {
				a.note(c, "暂无标签。")
			}
			ui.Row(c).Wrap().Gap(8).Children(func() {
				for _, raw := range tags {
					tag := object(raw)
					flatButton(c, "", str(tag["name"])+" · "+formatCount(tag["count"])).FontSize(12).Padding(6, 10).Radius(20).Background(c.Theme().Surface).Border(1, c.Theme().Border).OnClick(func() { w.KBTag = str(tag["name"]); w.KBView = ""; a.refreshWorkbench() })
				}
			})
		})
		return
	}
	if w.KBView == "health" {
		a.kbHealthView(c)
		return
	}
	ui.Column(c).Padding(0, 12, 10, 12).Gap(8).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			input := ui.TextInput(c, &w.KBQuery).Label("搜索知识库").Placeholder("搜索条目…").FontSize(12).Padding(6, 8).Grow(1).MinWidth(0).OnSubmit(func() { w.KBSearchAt = time.Time{}; a.refreshWorkbench() })
			if input.Changed() {
				w.KBSearchAt = time.Now().Add(200 * time.Millisecond)
			}
			if !w.KBSearchAt.IsZero() && time.Now().After(w.KBSearchAt) {
				w.KBSearchAt = time.Time{}
				a.refreshWorkbench()
			}
			if w.KBQuery != "" {
				iconButton(c, "x", "清除知识库搜索").Size(24, 24).OnClick(func() { w.KBQuery = ""; w.KBSearchAt = time.Time{}; a.refreshWorkbench() })
			}
			iconButton(c, "search", "搜索知识条目").OnClick(a.refreshWorkbench)
		})
		ui.Row(c).Wrap().Gap(4).Children(func() {
			tabButton(c, "全部分类", w.KBCategory == "").FontSize(11).OnClick(func() { w.KBCategory = ""; a.refreshWorkbench() })
			for _, raw := range list(object(l.Data["kb-categories"])["categories"]) {
				category := object(raw)
				name := str(category["name"])
				tabButton(c, name+" · "+formatCount(category["count"]), w.KBCategory == name).FontSize(11).OnClick(func() { w.KBCategory = name; a.refreshWorkbench() })
			}
			if w.KBTag != "" {
				flatButton(c, "x", "标签："+w.KBTag).FontSize(11).OnClick(func() { w.KBTag = ""; a.refreshWorkbench() })
			}
		})
	})
	if a.resourceFeedback(c, "kb") {
		return
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(12).Children(func() {
		entries := list(object(l.Data["kb"])["entries"])
		if len(entries) == 0 {
			a.note(c, "没有符合条件的知识条目。")
		}
		ui.Column(c).Gap(8).Children(func() {
			for _, raw := range entries {
				entry := object(raw)
				ui.ButtonBase(c.Key(str(entry["path"]))).Label("知识条目：" + str(entry["title"])).FillWidth().MinWidth(0).Justify(ui.Start).Padding(10).Radius(6).OnClick(func() { a.openKBEntry(entry) }).Children(func() {
					ui.Column(c).MinWidth(0).Gap(6).Children(func() {
						ui.Text(c, str(entry["title"])).FontSize(13).FontWeight(500)
						ui.Text(c, str(entry["summary"])).FontSize(11).MaxLines(2).TextColor(c.Theme().TextMuted)
						ui.Text(c, str(entry["category"])+" · "+str(entry["modified"])).FontSize(10).TextColor(c.Theme().TextMuted)
					})
				})
			}
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
			for _, group := range []struct{ key, label string }{{"entries_missing_title", "缺少标题"}, {"entries_missing_summary", "缺少摘要"}, {"entries_missing_tags", "缺少标签"}} {
				entries := list(m[group.key])
				nativeDisclosure(c, group.key, fmt.Sprintf("%s · %d", group.label, len(entries)), func() {
					if len(entries) == 0 {
						a.note(c, "没有发现问题。")
					}
					for _, raw := range entries {
						entry := object(raw)
						flatButton(c, "file", str(entry["title"])+" "+str(entry["rel_path"])).FontSize(12).Justify(ui.Start).FillWidth().OnClick(func() { a.openKBEntry(entry) })
					}
				})
			}
			groups := list(m["duplicate_groups"])
			nativeDisclosure(c, "duplicates", fmt.Sprintf("重复标题 · %d 组", len(groups)), func() {
				for _, raw := range groups {
					group := object(raw)
					ui.Text(c, str(group["canonical_title"])).FontSize(12).FontWeight(600)
					for _, raw := range list(group["entries"]) {
						entry := object(raw)
						flatButton(c, "file", str(entry["rel_path"])).FontSize(11).OnClick(func() { a.openKBEntry(entry) })
					}
				}
			})
			clusters := list(m["tag_clusters"])
			nativeDisclosure(c, "clusters", fmt.Sprintf("相似标签 · %d 组", len(clusters)), func() {
				for _, raw := range clusters {
					cluster := object(raw)
					a.note(c, str(cluster["canonical"])+" · "+displayText(cluster["variants"]))
				}
			})
		})
	})
}
