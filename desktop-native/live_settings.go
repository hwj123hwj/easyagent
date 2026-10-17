package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *preview) connectionLabel() string {
	l := a.Live
	if l.Connecting {
		return "正在连接…"
	}
	if l.Connected {
		return "已连接"
	}
	return "未连接"
}
func (a *preview) operation(key, method, path string, body any, done func(any)) {
	l := a.Live
	if l.Loading[key] {
		return
	}
	l.Loading[key] = true
	delete(l.ResourceErrors, key)
	epoch, ctx, b := l.epoch, l.hostCtx, l.Client
	go func() {
		var v any
		var err error
		if b == nil {
			err = errors.New("尚未连接运行主机")
		} else {
			err = b.request(ctx, method, path, body, &v)
		}
		a.post(epoch, func() {
			l.Loading[key] = false
			if err != nil {
				l.Error = err.Error()
				l.ResourceErrors[key] = err.Error()
				return
			}
			l.Error = ""
			if done != nil {
				done(v)
			}
		})
	}()
}
func (a *preview) loadSettingsPage() {
	l := a.Live
	if l.Client == nil {
		return
	}
	s := &a.Settings
	switch s.Tab {
	case "models":
		a.refreshModels()
	case "usage":
		a.resource("usage", "/usage/summary")
	case "experiments":
		a.resource("computer", "/computer/settings")
	case "mcp":
		a.resource("mcp", "/mcp"+query(map[string]string{"workspace": s.Workspace}))
	case "capabilities":
		if a.current().RemoteID != "" {
			a.resource("capabilities", sessionPath(a.current().RemoteID)+"/capabilities")
		}
	case "feishu":
		a.resource("feishu", "/settings/feishu")
	case "skillMarket":
		a.searchMarket()
		a.marketRead("installed", "/skills/market/installed", nil)
		a.marketRead("sections", "/skills/market/sections", nil)
	}
}
func (a *preview) liveSettingsPage(c *ui.Context) {
	l := a.Live
	tab := a.Settings.Tab
	if tab != "appearance" && tab != "general" && tab != "connections" && tab != "models" && tab != "skillMarket" {
		ui.Row(c).Children(func() {
			a.note(c, a.connectionLabel())
			ui.Spacer(c)
			a.action(c, "刷新").OnClick(a.loadSettingsPage)
		})
	}
	switch tab {
	case "general":
		a.generalSettings(c)
		a.note(c, "会话、草稿、主机和模型配置会保存到此原生应用的数据目录。模型与主机密钥保存在 macOS 钥匙串。")
		a.action(c, "打开应用数据目录").AlignSelf(ui.Start).OnClick(func() { go mygo.Shell.OpenPath(l.Dir) })
	case "appearance":
		a.appearanceSettings(c)
	case "models":
		a.liveModelSettings(c)
	case "connections":
		a.liveConnectionSettings(c)
	case "usage":
		a.liveUsageSettings(c)
	case "experiments":
		a.liveComputerSettings(c)
	case "mcp":
		a.liveMCPSettings(c)
	case "skillMarket":
		a.liveMarketSettings(c)
	case "capabilities":
		a.liveCapabilitySettings(c)
	case "feishu":
		a.liveFeishuSettings(c)
	}
}
func (a *preview) liveModelSettings(c *ui.Context) {
	s := &a.Settings
	l := a.Live
	busy := l.configBusy() || l.Loading["model-check"]
	ui.Row(c).Children(func() {
		a.heading(c, "全局模型配置")
		ui.Spacer(c)
		a.action(c, "刷新模型").Disabled(busy || l.Loading["model-list"]).OnClick(a.refreshModels)
	})
	defaultModel := str(object(object(l.Data["models"])["current"])["id"])
	if defaultModel == "" {
		defaultModel = "尚未读取"
	}
	a.preferenceRow(c, "主机默认模型", defaultModel, func() {})
	a.preferenceRow(c, "当前会话模型", a.current().Model, func() {})
	a.note(c, "新建会话使用主机默认模型；会话中单独选择的模型只影响该会话，并在重启后保留。")
	if !s.host().Local {
		a.note(c, "模型配置属于服务主机。远程默认模型需在对应主机的 EasyAgent 配置中修改；会话模型可在对话输入框切换。")
		a.action(c, "选择运行主机").OnClick(func() { a.openSettings("connections") })
		return
	}
	a.heading(c, "本地模型来源")
	ui.Column(c).Gap(12).Disabled(busy).Children(func() {
		ui.RadioGroup(c, func() {
			ui.Row(c).Gap(20).Wrap().Children(func() {
				for _, mode := range []string{"沿用命令行配置", "在桌面客户端配置"} {
					if ui.Radio(c, &s.Mode, mode, a.tr(mode)).Changed() {
						a.clearModelCheck()
					}
				}
			})
		})
		if s.Mode == "在桌面客户端配置" {
			ui.Row(c).Gap(16).Wrap().Children(func() {
				for index := 0; index < 4; index++ {
					ui.Column(c.Key(index)).Basis(220).Grow(1).MinWidth(180).Children(func() {
						switch index {
						case 0:
							if a.dropdown(c, "模型服务", &s.Provider, []string{"OpenAI 兼容服务 / 网关", "Anthropic"}).Changed() {
								s.APIKey = ""
								a.clearModelCheck()
							}
						case 1:
							placeholder := "http://localhost:4001"
							if s.Provider == "Anthropic" {
								placeholder = "https://api.anthropic.com"
							}
							if a.input(c, "服务地址", &s.BaseURL, placeholder, false).Label("全局模型服务地址").Changed() {
								a.clearModelCheck()
							}
						case 2:
							a.input(c, "默认模型", &s.Model, "填写模型 ID，或检测连接后选择", false).Label("全局默认模型")
						case 3:
							placeholder := "首次配置请输入 API Key"
							provider := "openai"
							if s.Provider == "Anthropic" {
								provider = "anthropic"
							}
							if l.Config.Model.HasKey && l.Config.Model.Provider == provider {
								placeholder = "留空保留已存入钥匙串的密钥"
							}
							if a.input(c, "API Key", &s.APIKey, placeholder, true).Changed() {
								a.clearModelCheck()
							}
						}
					})
				}
			})
			a.note(c, "配置适用于此 Mac 的本地 Agent。密钥保存在 macOS 钥匙串；远程 Agent 的访问令牌在运行主机中配置。")
		} else {
			a.note(c, "读取 ~/.easyagent/config.yaml、~/.easyagent/.env 和当前环境中的模型与工具配置。修改后点击应用即可重新读取。")
			a.action(c, "配置全局模型").OnClick(func() {
				s.Mode = "在桌面客户端配置"
				if s.Model == "" {
					s.Model = str(object(object(l.Data["models"])["current"])["id"])
				}
				a.clearModelCheck()
			})
		}
	})
	running := false
	for _, session := range a.Sessions {
		running = running || session.Running || session.Pending || session.Sending
	}
	missingURL := s.Mode == "在桌面客户端配置" && strings.TrimSpace(s.BaseURL) == ""
	incomplete := missingURL || s.Mode == "在桌面客户端配置" && strings.TrimSpace(s.Model) == ""
	ui.Row(c).Gap(12).Wrap().Margin(12, 0, 0, 0).Children(func() {
		a.action(c, "检测连接").Disabled(busy || missingURL).OnClick(a.checkLiveModel)
		a.primary(c, "应用并重新连接").Disabled(busy || l.Connecting || incomplete || running).OnClick(a.applyLiveModel)
		a.action(c, "撤销未保存修改").Disabled(busy).OnClick(func() { a.syncSettingsConfig(); s.APIKey = ""; a.clearModelCheck() })
	})
	if busy {
		a.note(c, "正在处理模型配置…")
	}
	if running {
		a.note(c, "当前有任务正在执行，完成或停止任务后即可应用配置。")
	}
	a.note(c, "检测只读取模型目录，不发送推理请求。应用后重启本地核心，新会话使用新的默认模型，已有会话的单独选择保留。")
	if catalog := object(l.Data["model-check"]); len(catalog) > 0 {
		a.heading(c, "检测到的模型")
		a.note(c, str(catalog["message"]))
		a.input(c, "搜索检测到的模型", &s.ModelCatalogSearch, "按模型 ID 搜索", false)
		ui.Scroll(c).Label("默认模型目录").MaxHeight(260).Children(func() {
			count := 0
			for _, item := range list(catalog["data"]) {
				name := str(object(item)["id"])
				if !strings.Contains(strings.ToLower(name), strings.ToLower(s.ModelCatalogSearch)) {
					continue
				}
				count++
				menuOption(c.Key(name), "cpu", name, s.Model == name).Label("设为默认模型：" + name).Disabled(busy || s.Mode != "在桌面客户端配置").OnClick(func() { s.Model = name })
			}
			if count == 0 {
				a.note(c, "没有匹配的模型")
			}
		})
	}
	if catalog := object(l.Data["models"]); str(catalog["discovery_error"]) != "" {
		a.note(c, "模型目录读取失败；当前配置的模型仍可选择。")
	}
}
func (a *preview) clearModelCheck() {
	delete(a.Live.Data, "model-check")
	a.Settings.ModelCatalogSearch = ""
	a.Live.Versions["model-check"]++
}

func (a *preview) modelCandidate() (modelConfig, string, error) {
	return a.modelCandidateWithModel(a.Settings.Model)
}
func (a *preview) modelCandidateWithModel(model string) (modelConfig, string, error) {
	s := &a.Settings
	cfg := modelConfig{Mode: "inherit"}
	if s.Mode == "在桌面客户端配置" {
		provider := "openai"
		if s.Provider == "Anthropic" {
			provider = "anthropic"
		}
		cfg = modelConfig{Mode: "override", Provider: provider, BaseURL: s.BaseURL, Model: model, HasKey: true}
	}
	cfg, err := validatedModel(cfg)
	key := strings.TrimSpace(s.APIKey)
	if strings.ContainsAny(key, " \t\r\n") || len(key) > 8192 {
		err = errors.New("模型密钥包含无效字符")
	}
	return cfg, key, err
}

// Catalog discovery needs a connection, but not a model selected in advance.
func (a *preview) modelCandidateForCheck() (modelConfig, string, error) {
	model := a.Settings.Model
	if strings.TrimSpace(model) == "" {
		model = "catalog-discovery"
	}
	return a.modelCandidateWithModel(model)
}

func (a *preview) checkLiveModel() {
	l := a.Live
	if l.configBusy() || l.Loading["model-check"] {
		return
	}
	cfg, key, err := a.modelCandidateForCheck()
	if err != nil {
		l.Error = err.Error()
		return
	}
	if cfg.Mode == "inherit" {
		a.refreshModels()
		l.Notice = "已读取核心当前配置的模型目录；未执行推理请求。"
		return
	}
	a.clearModelCheck()
	version := l.Versions["model-check"]
	l.Loading["model-check"] = true
	epoch, ctx, secrets := l.epoch, l.ctx, l.Secrets
	oldProvider := l.Config.Model.Provider
	go func() {
		var err error
		if key == "" {
			if oldProvider != cfg.Provider {
				err = errors.New("切换服务类型时需要填写密钥")
			} else {
				key, err = secrets.Get("model")
			}
		}
		var result map[string]any
		if err == nil && key == "" {
			err = errors.New("请输入模型密钥")
		}
		if err == nil {
			b, _ := newBackendClient(cfg.BaseURL, key)
			if cfg.Provider == "anthropic" {
				req, _ := newModelRequest(ctx, b, key)
				err = b.execute(req, &result)
			} else {
				err = b.request(ctx, "GET", modelCatalogPath(b.URL), nil, &result)
			}
		}
		if err == nil {
			rows, ok := result["data"].([]any)
			if !ok {
				err = errors.New("模型目录响应格式无效")
			} else {
				clean := []any{}
				for _, raw := range rows {
					item := object(raw)
					id := str(item["id"])
					if id == "" || len(id) > 512 || strings.ContainsAny(id, "\r\n\t") {
						continue
					}
					if key != "" && strings.Contains(id, key) {
						err = errors.New("模型服务返回了无效目录")
						break
					}
					clean = append(clean, map[string]any{"id": id})
				}
				result = map[string]any{"data": clean}
			}
		}
		a.post(epoch, func() {
			l.Loading["model-check"] = false
			if version != l.Versions["model-check"] {
				return
			}
			if err != nil {
				l.Error = err.Error()
				return
			}
			result["message"] = "已连接模型目录，未执行推理请求。"
			l.Data["model-check"] = result
			l.Error = ""
		})
	}()
}
func (a *preview) applyLiveModel() {
	l := a.Live
	if l.configBusy() || l.Loading["model-check"] {
		l.Error = "正在保存配置，请稍后再试"
		return
	}
	cfg, key, err := a.modelCandidate()
	if err != nil {
		l.Error = err.Error()
		return
	}
	for _, s := range a.Sessions {
		if s.Running || s.Sending || s.Pending {
			l.Error = "请先等待或停止正在运行的任务，再应用模型配置"
			return
		}
	}
	l.Loading["model-save"] = true
	epoch, ctx, secrets, dir := l.epoch, l.ctx, l.Secrets, l.Dir
	next := l.Config
	next.Model = cfg
	old := l.Config
	go func() {
		var previousKey string
		var err error
		if err = l.Core.ensureIdle(ctx); err != nil {
			a.post(epoch, func() { l.Loading["model-save"] = false; l.Error = err.Error() })
			return
		}
		if old.Model.HasKey {
			previousKey, err = secrets.Get("model")
		}
		if err == nil && cfg.Mode == "override" {
			if key == "" {
				if old.Model.Provider != cfg.Provider {
					err = errors.New("切换服务类型时需要填写密钥")
				} else {
					key = previousKey
				}
			}
			if key == "" {
				err = errors.New("请输入模型密钥")
			}
			if err == nil {
				err = secrets.Set("model", key)
			}
		}
		if err == nil {
			err = atomicJSON(filepath.Join(dir, "connections.json"), next)
		}
		if err == nil {
			l.Core.stop()
			_, err = l.Core.start(ctx, dir, cfg, secrets)
		}
		if err != nil {
			if previousKey != "" {
				_ = secrets.Set("model", previousKey)
			} else if cfg.Mode == "override" {
				_ = secrets.Delete("model")
			}
			_ = atomicJSON(filepath.Join(dir, "connections.json"), old)
			l.Core.stop()
			_, _ = l.Core.start(ctx, dir, old.Model, secrets)
		}
		a.post(epoch, func() {
			l.Loading["model-save"] = false
			a.Settings.APIKey = ""
			if err != nil {
				a.connectLive()
				l.Error = "应用失败，已恢复原配置：" + err.Error()
				return
			}
			l.Config = next
			a.syncSettingsConfig()
			a.clearModelCheck()
			l.Notice = "全局模型配置已保存，新建会话将使用新的默认模型。"
			a.connectLive()
		})
	}()
}
func (a *preview) liveConnectionSettings(c *ui.Context) {
	l := a.Live
	s := &a.Settings
	a.note(c, "会话、文件、工具和 MCP 均属于所选主机。更换地址或令牌时另存新连接，保留原连接的数据。")
	for index, h := range l.Config.Hosts {
		ui.Row(c.Key(h.ID)).Gap(10).Padding(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(8).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.text(c, h.Name).Bold()
				value := h.URL
				if h.Local {
					value = "应用管理的本地核心"
				}
				a.note(c, value)
			})
			label := "连接"
			if h.ID == l.Config.ActiveHost {
				label = a.connectionLabel()
			}
			a.action(c, label).Disabled(l.Connecting).OnClick(func() { a.selectLiveHost(index) })
			if !h.Local {
				a.action(c, "编辑").OnClick(func() { s.EditHost = h.ID; s.HostName = h.Name; s.HostURL = h.URL; s.HostToken = "" })
				a.action(c, "移除").OnClick(func() {
					l.Confirm = &nativeConfirmation{"移除主机连接？", h.Name + "；不会删除主机上的会话或文件。", func() { a.removeLiveHost(index) }}
				})
			}
		})
	}
	a.heading(c, "添加或修改远程主机")
	a.input(c, "连接名称", &s.HostName, "例如 迷你主机", false)
	a.input(c, "服务地址", &s.HostURL, "http://192.168.5.16:8080", false)
	a.input(c, "访问令牌", &s.HostToken, "留空保留现有令牌", true)
	ui.Row(c).Gap(8).Children(func() {
		a.primary(c, "保存连接").Disabled(l.Loading["host-save"]).OnClick(a.saveLiveHost)
		a.action(c, "新增连接").OnClick(func() { s.EditHost = ""; s.HostName = ""; s.HostURL = ""; s.HostToken = "" })
	})
	a.action(c, "重连当前主机").AlignSelf(ui.Start).Disabled(l.Connecting).OnClick(a.connectLive)
}
func (a *preview) selectLiveHost(index int) {
	l := a.Live
	if l.configBusy() {
		l.Error = "正在保存配置，请稍后切换主机"
		return
	}
	if index < 0 || index >= len(l.Config.Hosts) {
		return
	}
	a.captureDraftInputs()
	for _, s := range a.Sessions {
		l.Drafts[l.Config.ActiveHost+"/"+s.RemoteID] = s.Draft
	}
	l.Config.ActiveHost = l.Config.Hosts[index].ID
	a.Settings.Host = index
	l.Data = map[string]any{}
	l.Loading = map[string]bool{}
	l.ResourceErrors = map[string]string{}
	l.QRCode = ""
	l.QRImage = nil
	l.LoginName = ""
	l.Confirm = nil
	l.Rename = false
	l.RenameOwner = nil
	l.EditQueueID = ""
	l.EditQueueOwner = nil
	a.Settings.APIKey = ""
	a.Settings.AppSecret = ""
	a.Settings.AppID = ""
	a.Settings.MCPName = ""
	a.Settings.Variables = ""
	a.Settings.JSON = "{}"
	a.Settings.MCPBase = nil
	a.Settings.HostToken = ""
	a.Sessions = []*session{{ID: a.NextID, Title: "新会话", Workspace: l.workspace(), Draft: l.Drafts[l.Config.ActiveHost+"/"]}}
	a.Active = a.NextID
	a.NextID++
	a.restoreDraftInputs(a.current())
	a.syncSettingsConfig()
	a.connectLive()
	next := cloneConfig(l.Config)
	version := l.Store.reserve("connections.json")
	a.run("configuration", func(context.Context, *backendClient) (any, error) {
		return nil, l.Store.save("connections.json", version, next)
	}, nil)
}
func (a *preview) saveLiveHost() {
	l := a.Live
	if l.configBusy() {
		l.Error = "正在保存配置，请稍后再试"
		return
	}
	s := &a.Settings
	if strings.TrimSpace(s.HostName) == "" {
		l.Error = "请输入连接名称"
		return
	}
	b, err := newBackendClient(s.HostURL, s.HostToken)
	if err != nil {
		l.Error = err.Error()
		return
	}
	next := l.Config
	next.Hosts = append([]hostConfig{}, next.Hosts...)
	id := s.EditHost
	index := -1
	for i, h := range next.Hosts {
		if h.ID == id && !h.Local {
			index = i
		}
	}
	if index < 0 {
		id = requestID()
		index = len(next.Hosts)
		next.Hosts = append(next.Hosts, hostConfig{ID: id})
	}
	h := next.Hosts[index]
	key := s.HostToken
	// A different endpoint or credential is a distinct connection identity. Keep
	// the old profile (and its drafts/editors) available instead of relabelling it.
	changedIdentity := h.URL != "" && (h.URL != b.URL || key != "")
	wasActive := id == l.Config.ActiveHost
	if changedIdentity {
		id = requestID()
		index = len(next.Hosts)
		h = hostConfig{ID: id}
		next.Hosts = append(next.Hosts, h)
	}
	h.Name = strings.TrimSpace(s.HostName)
	h.URL = b.URL
	h.HasToken = key != "" || h.HasToken
	next.Hosts[index] = h
	l.Loading["host-save"] = true
	epoch := l.epoch
	go func() {
		var err error
		if key != "" {
			err = l.Secrets.Set("host/"+id, key)
		}
		if err == nil {
			err = atomicJSON(filepath.Join(l.Dir, "connections.json"), next)
			if err != nil && key != "" {
				_ = l.Secrets.Delete("host/" + id)
			}
		}
		a.post(epoch, func() {
			l.Loading["host-save"] = false
			if err != nil {
				l.Error = err.Error()
				return
			}
			l.Config = next
			s.EditHost = id
			s.HostToken = ""
			a.syncSettingsConfig()
			l.Notice = "主机连接已保存。"
			if changedIdentity {
				l.Notice = "已另存为新连接，原连接及其草稿仍保留。"
				if wasActive {
					a.selectLiveHost(index)
				}
			}
		})
	}()
}
func (a *preview) removeLiveHost(index int) {
	l := a.Live
	if l.configBusy() {
		l.Error = "正在保存配置，请稍后再试"
		return
	}
	if index < 0 || index >= len(l.Config.Hosts) || l.Config.Hosts[index].Local {
		return
	}
	next := l.Config
	next.Hosts = append([]hostConfig{}, next.Hosts...)
	id := next.Hosts[index].ID
	next.Hosts = append(next.Hosts[:index], next.Hosts[index+1:]...)
	active := next.ActiveHost == id
	if active {
		next.ActiveHost = "local"
	}
	a.run("configuration", func(context.Context, *backendClient) (any, error) {
		return nil, atomicJSON(filepath.Join(l.Dir, "connections.json"), next)
	}, func(any) {
		l.Config = next
		a.syncSettingsConfig()
		if active {
			a.selectLiveHost(0)
		}
	})
}
func (a *preview) liveUsageSettings(c *ui.Context) {
	if a.resourceFeedback(c, "usage", a.loadSettingsPage) {
		return
	}
	m := object(a.Live.Data["usage"])
	total := object(m["total"])
	total["total_tokens"] = number(total["input_tokens"]) + number(total["output_tokens"])
	a.heading(c, "近 30 天用量概览")
	a.note(c, "基于核心持久化的执行收据统计，包含多轮工具调用。")
	ui.Row(c).Gap(12).Children(func() {
		for _, item := range []struct{ label, key string }{{"总 Tokens", "total_tokens"}, {"输入 Tokens", "input_tokens"}, {"输出 Tokens", "output_tokens"}, {"总任务数 (Runs)", "run_count"}} {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(8).Padding(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(8).Children(func() { a.note(c, item.label); ui.Text(c, formatCount(total[item.key])).FontSize(20).Bold() })
		}
	})
	a.heading(c, "会话用量排行")
	for _, raw := range list(m["sessions"]) {
		row := object(raw)
		id := str(row["session_id"])
		title := id
		for _, s := range a.Sessions {
			if s.RemoteID == id {
				title = s.Title
			}
		}
		a.preferenceRow(c, title, fmt.Sprintf("%s 次任务 · 输入 %s · 输出 %s · 合计 %s", formatCount(row["run_count"]), formatCount(row["input_tokens"]), formatCount(row["output_tokens"]), formatCount(number(row["input_tokens"])+number(row["output_tokens"]))), func() {
			a.action(c, "打开会话").OnClick(func() {
				for _, s := range a.Sessions {
					if s.RemoteID == id {
						a.closeSettings()
						a.selectSession(s.ID)
					}
				}
			})
		})
	}
	if len(list(m["sessions"])) == 0 {
		a.note(c, "暂无执行用量。")
	}
}
func (a *preview) liveComputerSettings(c *ui.Context) {
	if a.resourceFeedback(c, "computer", a.loadSettingsPage) {
		return
	}
	l := a.Live
	m := object(l.Data["computer"])
	if l.Data["computer"] == nil {
		a.note(c, "正在读取电脑控制状态，请连接主机后刷新。")
		return
	}
	a.note(c, "开关对新会话生效。实际权限由当前运行主机的系统管理。")
	enabled := boolean(m["enabled"])
	a.preferenceRow(c, "启用电脑控制", "允许新会话使用电脑控制工具。", func() {
		if ui.Switch(c, &enabled).Label("启用电脑控制").Disabled(l.Loading["computer-save"]).Changed() {
			a.operation("computer-save", "POST", "/computer/settings", map[string]any{"enabled": enabled}, func(v any) { l.Data["computer"] = v })
		}
	})
	a.preferenceRow(c, "审批策略", "普通操作可自动执行；高风险操作仍会询问。", func() {
		ui.Row(c).Gap(8).Children(func() {
			for _, choice := range []struct{ key, label string }{{"ask", "逐批询问"}, {"auto", "自动执行"}} {
				selectedButton(c, a.action(c, choice.label), str(m["approval_policy"]) == choice.key).Disabled(l.Loading["computer-save"]).OnClick(func() {
					a.operation("computer-save", "POST", "/computer/settings", map[string]any{"approval_policy": choice.key}, func(v any) { l.Data["computer"] = v })
				})
			}
		})
	})
	availability, permissions, lease := "不可用（需要 macOS 14+ 桌面组件）", "当前主机未提供电脑控制组件", str(m["lease_holder"])
	if lease == "" {
		lease = "空闲"
	}
	if boolean(m["available"]) {
		availability = "组件已就绪，等待系统授权"
		permissions = "缺少系统授权"
		if missing := list(m["missing"]); len(missing) > 0 {
			permissions = "缺少：" + displayText(missing)
		}
	}
	if boolean(m["granted"]) {
		availability = "就绪"
		permissions = "已授予"
	}
	for _, row := range []struct{ label, value string }{{"Provider", str(m["provider"])}, {"可用性", availability}, {"系统权限", permissions}, {"当前占用", lease}, {"已控制应用（审计）", displayText(m["approved_apps"])}} {
		a.preferenceRow(c, row.label, row.value, func() {})
	}
	a.action(c, "请求系统权限").AlignSelf(ui.Start).Disabled(!a.Settings.host().Local || !boolean(m["available"]) || l.Loading["permissions"]).OnClick(func() {
		a.operation("permissions", "POST", "/computer/permissions", map[string]any{}, func(any) { a.loadSettingsPage() })
	})
	a.action(c, "清除应用审计记录").AlignSelf(ui.Start).OnClick(func() {
		l.Confirm = &nativeConfirmation{"清除应用审计记录？", "仅清除审批记录，不会撤销系统权限。", func() {
			a.operation("computer-save", "POST", "/computer/settings", map[string]any{"clear_approved_apps": true}, func(v any) { l.Data["computer"] = v })
		}}
	})
}
func (a *preview) liveCapabilitySettings(c *ui.Context) {
	if a.current().RemoteID == "" {
		a.note(c, "选择一个项目会话后查看实际加载的技能。")
		return
	}
	if a.resourceFeedback(c, "capabilities", a.loadSettingsPage) {
		return
	}
	m := object(a.Live.Data["capabilities"])
	a.heading(c, "当前会话的能力")
	a.note(c, "项目："+str(m["workspace"]))
	a.note(c, "默认读取项目与个人 .agents/skills，兼容 .claude/skills。同名技能以项目为先；文件变更后新建会话加载。")
	skills := list(m["skills"])
	a.heading(c, fmt.Sprintf("已加载技能 · %d", len(skills)))
	if len(skills) == 0 {
		a.note(c, "当前没有加载技能。")
	}
	for _, raw := range skills {
		skill := object(raw)
		scope, invocation := "个人", "仅显式调用"
		if str(skill["scope"]) == "project" {
			scope = "项目"
		}
		if boolean(skill["model_invocation"]) {
			invocation = "可由模型调用"
		}
		nativeDisclosure(c, str(skill["name"]), str(skill["name"])+" · "+scope+" · "+invocation, func() {
			a.note(c, str(skill["description"]))
			nativePre(c, str(skill["source"]))
			a.action(c, "使用技能").AlignSelf(ui.Start).OnClick(func() { a.current().Draft += "\n使用技能 " + str(skill["name"]) + " "; a.closeSettings() })
		})
	}
	a.heading(c, "工作流")
	a.note(c, "工作流功能已下线；历史运行记录可从 archive/workflow 分支版本查看。")
}

// Keep map iteration out of the view so keyed form rows remain stable.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}
func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
func idString(v any) string {
	if s := str(v); s != "" {
		return s
	}
	return strconv.FormatInt(int64(number(v)), 10)
}
func pathSegment(v string) string { return url.PathEscape(v) }
