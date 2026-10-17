package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type nativeModelChoice struct{ ID, Name, Provider string }

func (a *preview) modelChoices(query string) []nativeModelChoice {
	all := map[string]nativeModelChoice{}
	for _, raw := range list(object(a.Live.Data["models"])["models"]) {
		item := object(raw)
		id := str(item["id"])
		if id == "" {
			id = str(item["modelId"])
		}
		if id == "" {
			continue
		}
		name := str(item["name"])
		if name == "" {
			name = id
		}
		all[id] = nativeModelChoice{id, name, str(item["provider"])}
	}
	var out []nativeModelChoice
	for _, id := range a.Live.Models {
		choice := all[id]
		if choice.ID == "" {
			choice = nativeModelChoice{ID: id, Name: id}
		}
		if strings.Contains(strings.ToLower(choice.ID+" "+choice.Name+" "+choice.Provider), strings.ToLower(query)) {
			out = append(out, choice)
		}
	}
	return out
}
func (a *preview) liveModelPicker(c *ui.Context, anchor ui.Element) {
	l, s := a.Live, a.current()
	popup := a.menuPanel(c, anchor, &a.ModelMenu, func() {
		menuCaption(c, "选择模型 · ↑↓ 切换 · Enter 确认")
		input := ui.TextInput(c, &a.ModelSearch).Label("搜索模型").Placeholder("搜索模型…").AutoFocus().Padding(7).Background(c.Theme().Surface).Border(1, c.Theme().Border)
		if input.Changed() {
			l.ModelIndex = 0
			l.ModelJump = true
		}
		composing := input.Composing()
		input.OnTextKey(func(k ui.TextKey) bool {
			if composing || a.current() != s {
				return false
			}
			a.ModelSearch = k.Text()
			choices := a.modelChoices(a.ModelSearch)
			if len(choices) > 0 {
				if k.Key == ui.KeyUp || k.Key == ui.KeyDown {
					step := 1
					if k.Key == ui.KeyUp {
						step = -1
					}
					l.ModelIndex = (l.ModelIndex + step + len(choices)) % len(choices)
					l.ModelJump = true
					return true
				}
				if k.Key == ui.KeyEnter {
					a.switchLiveModel(choices[(l.ModelIndex%len(choices)+len(choices))%len(choices)].ID)
					return true
				}
			}
			return false
		})
		if l.Loading["model-list"] {
			a.note(c, "正在刷新模型目录…")
		}
		if err := l.ResourceErrors["model-list"]; err != "" {
			a.note(c, err)
			a.action(c, "重试模型目录").OnClick(a.refreshModels)
		}
		choices := a.modelChoices(a.ModelSearch)
		if len(choices) == 0 && !l.Loading["model-list"] {
			a.note(c, "没有匹配的模型")
		}
		ui.Scroll(c).Label("模型列表").MaxHeight(260).FocusGroup(ui.Vertical).Children(func() {
			for i, choice := range choices {
				button := ui.ButtonBase(c.Key(choice.ID)).TextColor(c.Theme().Text).FillWidth().MinWidth(0).Padding(8, 10).Gap(8).Radius(5).Selected(s.Model == choice.ID).Label("选择模型：" + choice.ID).Disabled(l.Loading["model/"+s.RemoteID]).OnClick(func() { a.switchLiveModel(choice.ID) })
				if s.Model == choice.ID {
					button.Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent)
				}
				if i == l.ModelIndex {
					button.Background(c.Theme().Text.Alpha(.08))
					if l.ModelJump {
						button.ScrollIntoView()
					}
				}
				button.Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, choice.Name).FontSize(12).SingleLine().MinWidth(0)
						if choice.Name != choice.ID || choice.Provider != "" {
							label := choice.ID
							if choice.Provider != "" {
								label = choice.Provider + " · " + label
							}
							ui.Text(c, label).FontSize(10).TextColor(c.Theme().TextMuted).SingleLine().MinWidth(0)
						}
					})
					if s.Model == choice.ID {
						icon(c, "check")
					}
				})
			}
		})
		l.ModelJump = false
		if str(object(l.Data["models"])["source"]) == "configured" {
			a.note(c, "仅显示已配置模型，连接尚未验证")
		}
		if notice := str(object(l.Data["models"])["notice"]); notice != "" {
			a.note(c, notice)
		}
		a.action(c, "模型连接设置").AlignSelf(ui.Start).OnClick(func() { a.ModelMenu = false; a.openSettings("models") })
	})
	if popup.Valid() {
		popup.AttachTo(anchor, ui.AnchorTopLeft, ui.AnchorBottomLeft).Width(328).Margin(0, 0, 8, 0)
	}
}
