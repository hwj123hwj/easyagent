package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type nativeMarketState struct {
	Section, ConfirmName, Error string
	Page                        int
	Jobs                        map[string]map[string]any
	Images                      map[string]*ui.Bitmap
	ImageErrors                 map[string]string
	Lightbox                    bool
	PreviewIndex                int
	CopiedUntil                 time.Time
}

func (a *preview) marketState() *nativeMarketState {
	m := &a.Live.Market
	if m.Jobs == nil {
		m.Jobs = map[string]map[string]any{}
	}
	if m.Images == nil {
		m.Images = map[string]*ui.Bitmap{}
		m.ImageErrors = map[string]string{}
	}
	return m
}

// Keep marketplace failures in the panel and ignore superseded searches/details.
func (a *preview) marketRead(key, path string, done func(any)) {
	l := a.Live
	l.Versions[key]++
	version, epoch, ctx, b := l.Versions[key], l.epoch, l.hostCtx, l.Client
	l.Loading[key] = true
	a.marketState().Error = ""
	go func() {
		var value any
		var err error
		if b == nil {
			err = errors.New("尚未连接运行主机")
		} else {
			err = b.request(ctx, "GET", path, nil, &value)
		}
		a.post(epoch, func() {
			if l.Versions[key] != version {
				return
			}
			l.Loading[key] = false
			if err != nil {
				l.Market.Error = err.Error()
				return
			}
			if done == nil {
				l.Data[key] = value
			} else {
				done(value)
			}
		})
	}()
}

func (a *preview) marketSearchPath(page int) string {
	s, m := &a.Settings, a.marketState()
	sort := "featured"
	if s.SkillFilter == "热门" {
		sort = "installs"
	}
	if s.SkillFilter == "名称" {
		sort = "name"
	}
	return "/skills/market/search" + query(map[string]string{"q": strings.TrimSpace(s.SkillQuery), "sort": sort, "section": m.Section, "page": strconv.Itoa(page), "locale": map[bool]string{true: "en", false: "zh"}[s.Preferences.Language == "English"]})
}

func (a *preview) searchMarket() { a.searchMarketPage(1) }
func (a *preview) searchMarketPage(page int) {
	l, m := a.Live, a.marketState()
	if page > 1 && l.Loading["market"] {
		return
	}
	a.marketRead("market", a.marketSearchPath(page), func(v any) {
		res := object(v)
		if page > 1 {
			items := append([]any{}, list(object(l.Data["market"])["skills"])...)
			seen := map[string]bool{}
			for _, item := range items {
				seen[idString(object(item)["id"])] = true
			}
			for _, item := range list(res["skills"]) {
				id := idString(object(item)["id"])
				if !seen[id] {
					items = append(items, item)
					seen[id] = true
				}
			}
			res["skills"] = items
		}
		l.Data["market"], m.Page = res, page
	})
}

func marketName(m map[string]any) string {
	if name := str(m["display_name"]); name != "" {
		return name
	}
	return str(m["name"])
}
func marketBytes(v any) string {
	n := number(v)
	if n <= 0 {
		return ""
	}
	if n < 1024 {
		return fmt.Sprintf("%.0f B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", n/1024)
	}
	return fmt.Sprintf("%.1f MB", n/(1024*1024))
}
func marketInitials(name string) string {
	chars := []rune(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, name))
	if len(chars) == 0 {
		return "?"
	}
	if chars[0] >= 0x3000 && chars[0] <= 0x9fff {
		return string(chars[:min(2, len(chars))])
	}
	var latin []rune
	for _, r := range chars {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			latin = append(latin, r)
		}
	}
	if len(latin) > 0 {
		return string(latin[:min(2, len(latin))])
	}
	return string(chars[:1])
}
func marketHue(name string) int {
	h := 0
	for _, r := range name {
		h = (h*31 + int(r)) % 360
	}
	return h
}
func marketHSL(h int, s, l float64) ui.Color {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(float64(h)/60, 2)-1))
	var r, g, b float64
	switch h / 60 {
	case 0:
		r, g = c, x
	case 1:
		r, g = x, c
	case 2:
		g, b = c, x
	case 3:
		g, b = x, c
	case 4:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := l - c/2
	return ui.RGB(uint8((r+m)*255), uint8((g+m)*255), uint8((b+m)*255))
}

// Public marketplace assets never receive the gateway's bearer token.
func fetchMarketImage(ctx context.Context, src string) (*ui.Bitmap, error) {
	u, err := url.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("图片地址无效")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", src, nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("图片请求失败（HTTP %d）", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, errors.New("图片超过 16 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > 24_000_000 {
		return nil, errors.New("图片尺寸过大")
	}
	return ui.DecodeBitmap(data)
}
func (a *preview) marketImage(src string) *ui.Bitmap {
	m := a.marketState()
	if src == "" {
		return nil
	}
	if img, known := m.Images[src]; known {
		return img
	}
	m.Images[src] = nil
	l := a.Live
	epoch, ctx := l.epoch, l.hostCtx
	go func() {
		img, err := fetchMarketImage(ctx, src)
		a.post(epoch, func() {
			if err != nil {
				m.ImageErrors[src] = "图片加载失败"
			} else {
				m.Images[src] = img
			}
		})
	}()
	return nil
}
func (a *preview) marketTile(c *ui.Context, skill map[string]any, large bool) {
	size, radius, font := float32(38), float32(9), float32(12.5)
	if large {
		size, radius, font = 52, 12, 17
	}
	name := marketName(skill)
	ui.Box(c).Size(size, size).Radius(radius).Clip().Center().Children(func() {
		if img := a.marketImage(str(skill["icon_url"])); img != nil {
			ui.Image(c, img).Size(size, size).Fit(ui.Cover)
			return
		}
		h := marketHue(name)
		ui.Box(c).Size(size, size).Gradient(marketHSL(h, .46, .52), marketHSL((h+42)%360, .52, .38), 135).Center().Children(func() {
			ui.Text(c, marketInitials(name)).FontSize(font).FontWeight(700).TextColor(ui.RGB(255, 255, 255))
		})
	})
}
func marketButton(c *ui.Context, label string, selected bool) ui.Element {
	e := flatButton(c, "", label).Padding(6, 12).FontSize(12.5).Radius(6).Border(1, c.Theme().Border).TextColor(c.Theme().Text).Selected(selected)
	if selected {
		e.Background(c.Theme().Accent).BorderColor(c.Theme().Accent).TextColor(c.Theme().AccentText)
	}
	return e
}
func (a *preview) marketInstalled(name string) bool {
	for _, raw := range list(object(a.Live.Data["installed"])["installed"]) {
		if str(object(raw)["name"]) == name {
			return true
		}
	}
	return false
}

func (a *preview) liveMarketSettings(c *ui.Context) {
	l, s, m := a.Live, &a.Settings, a.marketState()
	if s.SkillDetail != "" {
		a.marketDetail(c, object(l.Data["skill-detail"]))
		return
	}
	w, _ := c.Size()
	available := min(float32(780), w-240-64)
	ui.Row(c).Wrap().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).MinWidth(220).Gap(10).AlignItems(ui.Center).Children(func() {
			icon(c, "search")
			ui.TextInput(c, &s.SkillQuery).Label("搜索技能").Placeholder("搜索技能名称或描述…").Grow(1).MinWidth(0).FontSize(13).Padding(8, 12).Background(c.Theme().Surface).Border(1, c.Theme().Border).OnSubmit(func() {
				if s.SkillFilter == "已安装" {
					s.SkillFilter = "推荐"
				}
				a.searchMarket()
			})
		})
		ui.Row(c).Gap(4).Children(func() {
			for _, filter := range []string{"推荐", "热门", "名称", "已安装"} {
				selected := s.SkillFilter == filter || filter == "推荐" && s.SkillFilter == "精选"
				marketButton(c, filter, selected).OnClick(func() {
					s.SkillFilter = filter
					m.ConfirmName = ""
					if filter == "已安装" {
						a.marketRead("installed", "/skills/market/installed", nil)
					} else {
						a.searchMarket()
					}
				})
			}
		})
	})
	if s.SkillFilter != "已安装" {
		sections := list(object(l.Data["sections"])["sections"])
		if len(sections) > 0 {
			ui.Row(c).Wrap().Gap(4).Margin(0, 0, 4, 0).Children(func() {
				marketButton(c, "全部", m.Section == "").OnClick(func() { m.Section = ""; a.searchMarket() })
				for _, raw := range sections {
					section := object(raw)
					id := idString(section["id"])
					marketButton(c, str(section["name"]), m.Section == id).OnClick(func() { m.Section = id; a.searchMarket() })
				}
			})
		}
	}
	a.marketError(c)
	items := list(object(l.Data["market"])["skills"])
	loading := l.Loading["market"]
	if s.SkillFilter == "已安装" {
		items = list(object(l.Data["installed"])["installed"])
		loading = l.Loading["installed"]
	}
	columns := max(1, int((available+12)/272))
	if w <= 640 {
		columns = 1
	}
	if len(items) == 0 && loading {
		a.marketSkeleton(c, columns)
		return
	}
	ui.Grid(c).Columns(columns).Gap(12).AlignItems(ui.Stretch).Children(func() {
		for _, raw := range items {
			a.marketCard(c, object(raw), s.SkillFilter == "已安装", loading)
		}
	})
	if len(items) == 0 && !loading && m.Error == "" {
		text := "没有匹配的技能，换个关键词试试。"
		if s.SkillFilter == "已安装" {
			text = "还没有已安装的技能，去市场逛逛吧。"
		}
		ui.Text(c, text).FillWidth().Padding(48, 0).TextAlign(ui.Center).TextColor(c.Theme().TextMuted).FontSize(13)
	}
	if s.SkillFilter != "已安装" && len(items) > 0 && float64(len(items)) < number(object(l.Data["market"])["total"]) {
		ui.Row(c).Justify(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			label := "加载更多"
			if loading {
				label = "加载中…"
			}
			a.action(c, label).Padding(8, 24).Disabled(loading).OnClick(func() { a.searchMarketPage(max(1, m.Page) + 1) })
		})
	}
}
func (a *preview) marketError(c *ui.Context) {
	m := a.marketState()
	if m.Error == "" {
		return
	}
	ui.Row(c).Gap(10).Padding(12).Background(c.Theme().Danger.Alpha(.1)).Radius(6).Children(func() {
		icon(c, "alert")
		ui.Text(c, m.Error).Grow(1).MinWidth(0).FontSize(12).TextColor(c.Theme().Danger).Role(ui.RoleStatus)
		flatButton(c, "x", "关闭市场错误").OnClick(func() { m.Error = "" })
	})
}
func (a *preview) marketSkeleton(c *ui.Context, columns int) {
	ui.Grid(c).Columns(columns).Gap(12).Children(func() {
		for i := 0; i < 8; i++ {
			ui.Column(c.Key(i)).Padding(14).Gap(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Radius(8).Children(func() {
				ui.Row(c).Gap(10).Children(func() {
					ui.Box(c).Size(38, 38).Radius(9).Background(c.Theme().SurfaceHover)
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Box(c).Height(12).FillWidth().Background(c.Theme().SurfaceHover).Radius(4)
						ui.Box(c).Size(70, 11).Background(c.Theme().SurfaceHover).Radius(4)
					})
				})
				ui.Box(c).Height(38).FillWidth().Background(c.Theme().SurfaceHover).Radius(4)
				ui.Box(c).Height(28).Width(72).AlignSelf(ui.End).Background(c.Theme().SurfaceHover).Radius(14)
			})
		}
	})
}
func (a *preview) marketCard(c *ui.Context, skill map[string]any, installedView, refreshing bool) {
	name := str(skill["name"])
	var focus ui.Handle
	card := ui.Column(c.Key(name)).Label("技能卡片："+name).MinWidth(0).Padding(14).Gap(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(8)
	if !installedView {
		card.Role(ui.RoleGroup).Bind(&focus).Focusable().OnClick(func() {
			if !refreshing {
				a.openMarketDetail(skill)
			}
		}).HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputKeyDown && (ev.Key == ui.KeyEnter || ev.Key == ui.KeySpace) && focus.Focused(c) {
				if !refreshing {
					a.openMarketDetail(skill)
				}
				return true
			}
			return false
		})
	}
	if boolean(skill["featured"]) {
		card.Background(c.Theme().Accent.Mix(c.Theme().Surface, .95)).BorderColor(c.Theme().Accent.Alpha(.45))
	}
	if card.Hovered() || card.FocusVisible() {
		card.BorderColor(c.Theme().Accent.Alpha(.55))
	}
	card.Children(func() {
		if boolean(skill["featured"]) {
			ui.Text(c, "★ 精选").Absolute().Top(8).Right(8).Padding(2, 8).Radius(20).Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent).FontSize(10.5)
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.marketTile(c, skill, false)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, marketName(skill)).FontSize(13.5).FontWeight(600).SingleLine().Tooltip(marketName(skill))
				if v := str(skill["version"]); v != "" {
					ui.Text(c, "v"+v).FontSize(11).TextColor(c.Theme().TextMuted)
				}
			})

		})
		if !installedView {
			ui.Text(c, str(skill["description"])).FontSize(12).LineHeight(1.55).TextColor(c.Theme().TextMuted).MaxLines(2).Tooltip(str(skill["description"]))
			a.marketTags(c, skill, 3)
		}
		job := a.marketState().Jobs[idString(skill["id"])]
		if str(job["state"]) == "failed" {
			ui.Text(c, str(job["error"])).FontSize(12).TextColor(c.Theme().Danger)
		}
		if str(job["state"]) == "succeeded" && !installedView {
			ui.Text(c, "已安装，新会话即可使用。").FontSize(12).TextColor(c.Theme().Accent)
		}
		ui.Spacer(c)
		ui.Row(c).Wrap().Gap(8).MinHeight(30).AlignItems(ui.Center).Children(func() {
			if installedView {
				a.marketBadge(c)
			} else {
				ui.Row(c).Gap(10).TextColor(c.Theme().TextMuted).FontSize(11.5).Children(func() {
					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						icon(c, "download")
						ui.Text(c, fmt.Sprintf("%.0f", number(skill["install_count"]))).Label(fmt.Sprintf("%.0f 次安装", number(skill["install_count"])))
					})
					if size := marketBytes(skill["size_bytes"]); size != "" {
						ui.Text(c, size).SingleLine()
					}
				})
			}
			ui.Spacer(c)
			a.marketActions(c, skill, installedView, refreshing)
		})
	})
}
func (a *preview) marketBadge(c *ui.Context) {
	ui.Row(c).Gap(5).FontSize(12.5).TextColor(c.Theme().Accent).AlignItems(ui.Center).Children(func() { icon(c, "check"); ui.Text(c, "已安装") })
}
func (a *preview) marketTags(c *ui.Context, skill map[string]any, limit int) {
	tags := list(skill["tags"])
	if len(tags) == 0 {
		return
	}
	ui.Row(c).Wrap().Gap(4).Children(func() {
		for _, tag := range tags[:min(limit, len(tags))] {
			ui.Text(c, "#"+str(tag)).FontSize(10.5).Padding(1, 7).Radius(20).Background(c.Theme().SurfaceHover).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *preview) openMarketDetail(skill map[string]any) {
	a.Settings.SkillDetail = idString(skill["id"])
	m := a.marketState()
	m.ConfirmName = ""
	m.CopiedUntil = time.Time{}
	a.Live.Data["skill-detail"] = skill
	id := a.Settings.SkillDetail
	a.marketRead("skill-detail", "/skills/market/skills/"+pathSegment(id), func(v any) {
		if a.Settings.SkillDetail != id {
			return
		}
		merged := map[string]any{}
		for k, v := range skill {
			merged[k] = v
		}
		for k, v := range object(v) {
			merged[k] = v
		}
		a.Live.Data["skill-detail"] = merged
	})
}
func (a *preview) marketActions(c *ui.Context, skill map[string]any, installedView, disabled bool) {
	m := a.marketState()
	id, name := idString(skill["id"]), str(skill["name"])
	job := m.Jobs[id]
	if str(job["state"]) == "running" {
		a.marketProgress(c, id, job)
		return
	}
	if m.ConfirmName == name {
		ui.Row(c).Gap(6).Children(func() {
			marketButton(c, "确认卸载", false).TextColor(c.Theme().Danger).Disabled(a.Live.Loading["uninstall/"+name]).OnClick(func() { a.uninstallMarketSkill(name, id) })
			marketButton(c, "取消", false).OnClick(func() { m.ConfirmName = "" })
		})
		return
	}
	if a.marketInstalled(name) || str(job["state"]) == "succeeded" {
		if !installedView {
			a.marketBadge(c)
		}
		label := "卸载：" + name
		if installedView {
			label = "卸载"
		}
		if installedView {
			flatButton(c, "delete", "卸载").FontSize(12.5).Padding(6, 8).TextColor(c.Theme().TextMuted).Disabled(disabled).OnClick(func() { m.ConfirmName = name })
		} else {
			iconButton(c, "delete", label).TextColor(c.Theme().TextMuted).Disabled(disabled).OnClick(func() { m.ConfirmName = name })
		}
		return
	}
	flatButton(c, "download", "安装").Padding(6, 14).Radius(30).FontSize(12.5).FontWeight(600).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Disabled(disabled).OnClick(func() { a.installSkill(skill) })
}
func (a *preview) marketProgress(c *ui.Context, id string, job map[string]any) {
	phase := map[string]string{"resolving": "获取信息", "downloading": "下载中", "verifying": "校验中", "extracting": "解压安装", "done": "完成"}[str(job["phase"])]
	if phase == "" {
		phase = "安装中…"
	}
	pct := .4
	if total := number(job["bytes_total"]); total > 0 {
		pct = min(1, number(job["bytes_done"])/total)
		phase += fmt.Sprintf(" %.0f%%", pct*100)
	}
	ui.Column(c).MinWidth(100).MaxWidth(220).Grow(1).Gap(4).Children(func() {
		ui.Progress(c, pct).FillWidth().Height(6)
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, phase).FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
			iconButton(c, "x", "取消安装："+id).Size(20, 20).Disabled(str(job["job_id"]) == "").OnClick(func() { a.cancelMarketInstall(id, str(job["job_id"])) })
		})
	})
}
func (a *preview) marketDetail(c *ui.Context, skill map[string]any) {
	m := a.marketState()
	flatButton(c, "arrow-left", "返回").AlignSelf(ui.Start).Padding(6, 0).OnClick(func() { a.Settings.SkillDetail = ""; m.ConfirmName = "" })
	a.marketError(c)
	ui.Row(c).Wrap().Gap(14).AlignItems(ui.Center).Children(func() {
		a.marketTile(c, skill, true)
		ui.Row(c).Wrap().Gap(10).Grow(1).MinWidth(100).AlignItems(ui.Center).Children(func() {
			ui.Text(c, marketName(skill)).FontSize(20).FontWeight(700)
			if v := str(skill["version"]); v != "" {
				ui.Text(c, "v"+v).FontSize(13).TextColor(c.Theme().TextMuted)
			}
			if boolean(skill["featured"]) {
				ui.Text(c, "★ 精选").FontSize(11).TextColor(c.Theme().Accent)
			}
		})
		if (a.marketInstalled(str(skill["name"])) || str(m.Jobs[idString(skill["id"])]["state"]) == "succeeded") && m.ConfirmName != str(skill["name"]) {
			a.marketBadge(c)
		} else {
			a.marketActions(c, skill, false, false)
		}
	})
	if a.Live.Loading["skill-detail"] {
		ui.Text(c, "正在加载详情…").FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if job := m.Jobs[idString(skill["id"])]; str(job["state"]) == "failed" {
		ui.Text(c, str(job["error"])).TextColor(c.Theme().Danger).FontSize(12)
	}
	ui.Text(c, str(skill["description"])).Selectable().FontSize(14).LineHeight(1.75)
	a.marketTags(c, skill, len(list(skill["tags"])))
	ui.Row(c).Wrap().Gap(16).TextColor(c.Theme().TextMuted).FontSize(12.5).Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() { icon(c, "download"); ui.Text(c, fmt.Sprintf("%.0f 次安装", number(skill["install_count"]))) })
		if size := marketBytes(skill["size_bytes"]); size != "" {
			ui.Text(c, size).SingleLine()
		}
		if section := str(skill["section_name"]); section != "" {
			ui.Text(c, section)
		}
		if a.marketInstalled(str(skill["name"])) && m.ConfirmName != str(skill["name"]) {
			ui.Spacer(c)
			flatButton(c, "delete", "卸载").FontSize(12.5).TextColor(c.Theme().TextMuted).OnClick(func() { m.ConfirmName = str(skill["name"]) })
		}
	})
	if example := strings.TrimSpace(str(skill["usage_example"])); example != "" {
		a.marketDetailSection(c, "copy", "使用示例", "复制这段提示词，直接开始使用这个技能", func() {
			label := "复制提示词"
			if time.Now().Before(m.CopiedUntil) {
				label = "已复制"
			}
			flatButton(c, "copy", label).Padding(6, 12).FontSize(12.5).OnClick(func() { c.WriteClipboard(example); m.CopiedUntil = time.Now().Add(1600 * time.Millisecond) })
		}, func() {
			ui.Text(c, example).Selectable().FontSize(13).LineHeight(1.65).FillWidth().Padding(12, 14).Background(searchSurface(c)).Border(1, c.Theme().Border).Radius(6)
		})
	}
	previews := list(skill["preview_images"])
	if len(previews) > 0 {
		a.marketDetailSection(c, "image", "产出效果", "查看这个技能的实际产出效果", func() {
			ui.Text(c, fmt.Sprintf("%d 张预览图", len(previews))).FontSize(11).Padding(2, 10).Radius(20).Background(c.Theme().SurfaceHover).TextColor(c.Theme().TextMuted)
		}, func() {
			ui.Grid(c).Columns(min(3, len(previews))).Gap(10).Children(func() {
				for i, raw := range previews {
					src := str(raw)
					img := a.marketImage(src)
					h := float32(190)
					if len(previews) == 1 {
						h = 280
					}
					ui.ButtonBase(c.Key(src)).Label(fmt.Sprintf("查看产出预览 %d", i+1)).Padding(0).MinWidth(0).Height(h).Radius(6).Clip().Background(searchSurface(c)).OnClick(func() { m.PreviewIndex = i; m.Lightbox = true }).Children(func() {
						if img != nil {
							fit := ui.Cover
							if len(previews) == 1 {
								fit = ui.Contain
							}
							ui.Image(c, img).FillWidth().Height(h).Fit(fit)
						} else {
							label := "图片加载中…"
							if m.ImageErrors[src] != "" {
								label = m.ImageErrors[src]
							}
							ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
						}
					})
				}
			})
		})
	}
	if files := list(skill["example_files"]); len(files) > 0 {
		a.marketDetailSection(c, "file", "示例文件", "这个技能附带的示例产物", nil, func() {
			ui.Column(c).Gap(8).Children(func() {
				for _, raw := range files {
					f := object(raw)
					ui.ButtonBase(c).Label("下载示例："+str(f["name"])).Gap(10).Padding(10, 12).Border(1, c.Theme().Border).Radius(6).OnClick(func() {
						if u, err := url.Parse(str(f["url"])); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
							go mygo.Shell.OpenExternal(u.String())
						}
					}).Children(func() {
						icon(c, "file")
						ui.Text(c, str(f["name"])).FontSize(13).SingleLine().Grow(1).MinWidth(0)
						ui.Text(c, marketBytes(f["size"])).FontSize(12).TextColor(c.Theme().TextMuted)
						ui.Row(c).Gap(4).FontSize(12.5).TextColor(c.Theme().Accent).Children(func() { icon(c, "download"); ui.Text(c, "下载") })
					})
				}
			})
		})
	}
	if body := strings.TrimSpace(str(skill["rich_description"])); body != "" {
		ui.Text(c, body).Selectable().FontSize(13).LineHeight(1.75).TextColor(c.Theme().TextMuted).Padding(4, 0)
	}
}
func (a *preview) marketDetailSection(c *ui.Context, glyph, title, subtitle string, action, body func()) {
	ui.Column(c).Gap(12).Padding(14).Border(1, c.Theme().Border).Radius(8).Background(c.Theme().Surface).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Size(30, 30).Radius(7).Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent).Center().Children(func() { icon(c, glyph) })
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(14).FontWeight(600)
				ui.Text(c, subtitle).FontSize(12).TextColor(c.Theme().TextMuted)
			})
			if action != nil {
				action()
			}
		})
		body()
	})
}
func (a *preview) marketLightbox(c *ui.Context) {
	m := a.marketState()
	if !a.Settings.Open || a.Settings.SkillDetail == "" {
		m.Lightbox = false
	}
	ui.Modal(c, &m.Lightbox, func() {
		previews := list(object(a.Live.Data["skill-detail"])["preview_images"])
		if len(previews) == 0 {
			return
		}
		m.PreviewIndex = (m.PreviewIndex + len(previews)) % len(previews)
		w, h := c.Size()
		panel := ui.Column(c).Width(min(1000, w-80)).Gap(12)
		if panel.OverlayShortcut(0, ui.KeyLeft) {
			m.PreviewIndex = (m.PreviewIndex - 1 + len(previews)) % len(previews)
		}
		if panel.OverlayShortcut(0, ui.KeyRight) {
			m.PreviewIndex = (m.PreviewIndex + 1) % len(previews)
		}
		panel.Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("产出预览 %d / %d", m.PreviewIndex+1, len(previews))).FontSize(13)
				ui.Spacer(c)
				flatButton(c, "x", "关闭预览").OnClick(func() { m.Lightbox = false })
			})
			src := str(previews[m.PreviewIndex])
			if img := a.marketImage(src); img != nil {
				ui.Image(c, img).FillWidth().Height(max(100, h-220)).Fit(ui.Contain)
			} else {
				ui.Text(c, "图片加载中…").Padding(48)
			}
			if len(previews) > 1 {
				ui.Row(c).Justify(ui.Center).Gap(12).Children(func() {
					flatButton(c, "arrow-left", "上一张").OnClick(func() { m.PreviewIndex = (m.PreviewIndex - 1 + len(previews)) % len(previews) })
					flatButton(c, "chevron-right", "下一张").OnClick(func() { m.PreviewIndex = (m.PreviewIndex + 1) % len(previews) })
				})
			}
		})
	})
}

func (a *preview) installSkill(skill map[string]any) {
	l, m := a.Live, a.marketState()
	id := idString(skill["id"])
	if str(m.Jobs[id]["state"]) == "running" {
		return
	}
	m.Jobs[id] = map[string]any{"state": "running", "phase": "resolving"}
	epoch, ctx, b := l.epoch, l.hostCtx, l.Client
	go func() {
		var res map[string]any
		var err error
		if b == nil {
			err = errors.New("尚未连接运行主机")
		} else {
			err = b.request(ctx, "POST", "/skills/market/install", map[string]any{"id": number(skill["id"])}, &res)
		}
		a.post(epoch, func() {
			if err != nil {
				m.Jobs[id] = map[string]any{"state": "failed", "error": err.Error()}
				return
			}
			jobID := str(res["job_id"])
			if jobID == "" {
				m.Jobs[id] = map[string]any{"state": "failed", "error": "安装任务未返回编号"}
				return
			}
			m.Jobs[id]["job_id"] = jobID
			a.observeMarketJob(id, jobID)
		})
	}()
}
func (a *preview) observeMarketJob(skillID, jobID string) {
	l, m := a.Live, a.marketState()
	epoch, ctx, b := l.epoch, l.hostCtx, l.Client
	go func() {
		for {
			var v map[string]any
			err := b.request(ctx, "GET", "/skills/market/jobs/"+pathSegment(jobID), nil, &v)
			terminal := err != nil || str(v["state"]) != "running"
			a.post(epoch, func() {
				if str(m.Jobs[skillID]["job_id"]) != jobID {
					return
				}
				if err != nil {
					m.Jobs[skillID] = map[string]any{"state": "failed", "error": err.Error()}
					return
				}
				v["job_id"] = jobID
				m.Jobs[skillID] = v
				if str(v["state"]) != "running" {
					a.marketRead("installed", "/skills/market/installed", nil)
				}
			})
			if terminal {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
}
func (a *preview) cancelMarketInstall(skillID, jobID string) {
	a.operation("cancel-install/"+skillID, "POST", "/skills/market/jobs/"+pathSegment(jobID)+"/cancel", map[string]any{}, nil)
}
func (a *preview) uninstallMarketSkill(name, id string) {
	a.operation("uninstall/"+name, "DELETE", "/skills/market/installed/"+pathSegment(name), nil, func(any) {
		m := a.marketState()
		m.ConfirmName = ""
		delete(m.Jobs, id)
		a.marketRead("installed", "/skills/market/installed", nil)
	})
}
