package main

import (
	"github.com/egoist/mygo/ui"
	"math"
)

type kbCloudTag struct {
	name, count string
	size, width float32
}
type kbTagCloud struct {
	source []any
	font   string
	tags   []kbCloudTag
	rows   [][]int
	width  float32
}

// Measure once per payload/font; resizing only repacks cached widths.
// Only visible rows create UI elements, even for thousands of tags.
func (cloud *kbTagCloud) layout(c *ui.Context, source []any, width float32) {
	changed := len(source) != len(cloud.source) || len(source) > 0 && &source[0] != &cloud.source[0] || cloud.font != c.Theme().Font
	if changed {
		cloud.source = source
		cloud.font = c.Theme().Font
		maxCount := float64(1)
		for _, raw := range source {
			maxCount = max(maxCount, number(object(raw)["count"]))
		}
		cloud.tags = make([]kbCloudTag, len(source))
		for i, raw := range source {
			tag := object(raw)
			name := str(tag["name"])
			count := formatCount(tag["count"])
			size := float32(12 + math.Round(number(tag["count"])/maxCount*10))
			tw, _ := c.MeasureText(0, ui.Span{Text: "#" + name, Font: cloud.font, Size: size, Weight: 500})
			cw, _ := c.MeasureText(0, ui.Span{Text: count, Font: cloud.font, Size: 10, Weight: 600})
			cloud.tags[i] = kbCloudTag{name, count, size, float32(math.Ceil(float64(tw+cw+3))) + 2}
		}
	}
	width = max(1, width)
	if !changed && width == cloud.width {
		return
	}
	cloud.width = width
	cloud.rows = nil
	var row []int
	used := float32(0)
	for i, tag := range cloud.tags {
		w := min(width, tag.width)
		if len(row) > 0 && used+12+w > width {
			cloud.rows = append(cloud.rows, row)
			row = nil
			used = 0
		}
		if len(row) > 0 {
			used += 12
		}
		row = append(row, i)
		used += w
	}
	if len(row) > 0 {
		cloud.rows = append(cloud.rows, row)
	}
}

func (a *preview) kbTagsView(c *ui.Context) {
	l, w := a.Live, &a.Live.Workbench
	if w.KBActiveTag != "" {
		entries := list(object(l.Data["kb-tag-entries"])["entries"])
		ui.Row(c).Padding(6, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			flatButton(c, "arrow-left", "返回标签云").OnClick(func() { w.KBActiveTag = "" })
			ui.Text(c, "#"+w.KBActiveTag).SingleLine().Grow(1).MinWidth(0).FontSize(13)
			ui.Text(c, formatCount(len(entries))).FontSize(11).TextColor(c.Theme().TextMuted)
		})
		if a.resourceFeedback(c, "kb-tag-entries") {
			return
		}
		if len(entries) == 0 {
			a.note(c, "这个标签暂无知识条目。")
			return
		}
		ui.List(c, nil, len(entries)).Grow(1).MinHeight(0).Padding(10).ItemKey(func(i int) any { return str(object(entries[i])["rel_path"]) }).Rows(func(row ui.ListRow) {
			c := row.Context
			entry := object(entries[row.Index])
			ui.ButtonBase(c).Label("知识条目：" + str(entry["title"])).FillWidth().MinWidth(0).Padding(10).OnClick(func() { a.openKBEntry(entry) }).Children(func() {
				ui.Column(c).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, str(entry["title"])).SingleLine().FontSize(13)
					ui.Text(c, str(entry["category"])).SingleLine().FontSize(10).TextColor(c.Theme().TextMuted)
				})
			})
		})
		return
	}
	if a.resourceFeedback(c, "kb-tags") {
		return
	}
	tags := list(object(l.Data["kb-tags"])["tags"])
	if len(tags) == 0 {
		a.note(c, "暂无标签。")
		return
	}
	width := w.KBPaneWidth
	if width <= 0 {
		width = 400
	}
	w.KBCloud.layout(c, tags, width-32)
	cloud := &w.KBCloud
	ui.List(c, &w.KBTags, len(cloud.rows)).Grow(1).MinHeight(0).Padding(16).Gap(8).Rows(func(row ui.ListRow) {
		c := row.Context
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			for _, i := range cloud.rows[row.Index] {
				tag := cloud.tags[i]
				btn := ui.ButtonBase(c.Key(tag.name)).Label("标签：" + tag.name).Tooltip(tag.name + " (" + tag.count + ")").Width(min(cloud.width, tag.width)).MinWidth(0).Padding(0).Gap(3).
					OnClick(func() { w.KBActiveTag = tag.name; a.refreshWorkbench() })
				color := c.Theme().TextMuted
				if btn.Hovered() {
					color = c.Theme().Accent
				}
				btn.Children(func() {
					ui.Text(c, "#"+tag.name).SingleLine().MinWidth(0).FontSize(tag.size).FontWeight(500).TextColor(color)
					ui.Text(c, tag.count).SingleLine().FontSize(10).FontWeight(600).TextColor(c.Theme().TextMuted)
				})
			}
		})
	})
}
