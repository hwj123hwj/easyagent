package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	qrcode "github.com/skip2/go-qrcode"
)

func newModelRequest(ctx context.Context, b *backendClient, key string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.URL+modelCatalogPath(b.URL), nil)
	if err == nil {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	return req, err
}
func (a *preview) liveMCPSettings(c *ui.Context) {
	if a.resourceFeedback(c, "mcp", a.loadSettingsPage) {
		return
	}
	s := &a.Settings
	l := a.Live
	m := object(l.Data["mcp"])
	workspace := s.Workspace
	apply := func(v any) {
		if a.Settings.Workspace == workspace {
			l.Data["mcp"] = v
		} else {
			a.loadSettingsPage()
		}
	}
	ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.input(c, "项目工作区", &s.Workspace, "当前主机上的绝对路径", false) })
		a.action(c, "读取").OnClick(a.loadSettingsPage)
	})
	trusted := boolean(m["project_trusted"])
	label := "信任此项目"
	if trusted {
		label = "撤销信任"
	}
	a.action(c, label).AlignSelf(ui.Start).OnClick(func() {
		a.operation("mcp-trust", "PUT", "/mcp/project-trust", map[string]any{"workspace": s.Workspace, "trusted": !trusted}, apply)
	})
	for _, raw := range list(m["issues"]) {
		a.note(c, str(object(raw)["message"]))
	}
	for _, raw := range list(m["servers"]) {
		server := object(raw)
		name, scope := str(server["name"]), str(server["scope"])
		params := query(map[string]string{"workspace": s.Workspace, "scope": scope})
		path := "/mcp/servers/" + pathSegment(name)
		ui.Column(c.Key(scope+name)).Padding(12).Gap(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(8).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, name).Bold().Grow(1)
				a.note(c, scope+" · "+str(server["transport"])+" · "+str(server["state"])+" · "+formatCount(server["tool_count"])+" 个工具")
			})
			if err := str(server["error"]); err != "" {
				ui.Text(c, err).FontSize(12).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(6).Wrap().Children(func() {
				a.action(c, "修改配置").OnClick(func() {
					s.MCPName = name
					s.Scope = "个人"
					if scope == "project" {
						s.Scope = "项目"
					}
					s.Advanced = true
					s.JSON = "{}"
					s.MCPBase = nil
					l.Notice = "填写需要修改的字段；省略的凭据会保留。"
				})
				for _, act := range []struct{ id, label string }{{"reconnect", "重新连接"}, {"refresh", "刷新工具"}, {"enable", "启用"}, {"disable", "停用"}, {"trust", "信任服务"}, {"untrust", "撤销信任"}, {"login", "授权登录"}, {"logout", "退出授权"}} {
					if act.id == "enable" && boolean(server["enabled"]) || act.id == "disable" && !boolean(server["enabled"]) || act.id == "trust" && boolean(server["trusted"]) || act.id == "untrust" && !boolean(server["trusted"]) {
						continue
					}
					if (act.id == "login" || act.id == "logout") && str(server["transport"]) == "stdio" {
						continue
					}
					a.action(c, act.label).Disabled(l.Loading["mcp-action"] || l.Loading["mcp-login"]).OnClick(func() {
						if act.id == "login" {
							a.startMCPLogin(name, scope, workspace)
							return
						}
						a.operation("mcp-action", "POST", path+"/"+act.id+params, map[string]any{}, apply)
					})
				}
				a.action(c, "删除").Disabled(l.Loading["mcp-action"]).TextColor(c.Theme().Danger).OnClick(func() {
					l.Confirm = &nativeConfirmation{"删除 MCP 服务？", name + " · " + scope + " · " + s.Workspace, func() { a.operation("mcp-action", "DELETE", path+params, nil, apply) }}
				})
			})
			for _, toolRaw := range list(server["tools"]) {
				tool := object(toolRaw)
				enabled := boolean(tool["enabled"])
				ui.Row(c).Gap(8).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Children(func() { ui.Text(c, str(tool["name"])).FontSize(12); a.note(c, str(tool["description"])) })
					if ui.Checkbox(c.Key(str(tool["name"])), &enabled, "启用工具："+str(tool["name"])).Disabled(l.Loading["mcp-tool"]).Changed() {
						a.operation("mcp-tool", "POST", path+"/tools/"+pathSegment(str(tool["name"]))+params, map[string]any{"enabled": enabled}, apply)
					}
				})
			}
		})
	}
	if l.LoginURL != "" {
		a.note(c, "正在等待 MCP 授权回调…")
		a.action(c, "打开授权页面").OnClick(func() { go mygo.Shell.OpenExternal(l.LoginURL) })
		a.action(c, "取消授权").OnClick(a.cancelMCPLogin)
	}
	a.heading(c, "添加或修改 MCP 服务")
	a.dropdown(c, "配置范围", &s.Scope, []string{"个人", "项目"})
	a.input(c, "服务名称", &s.MCPName, "例如 filesystem", false)
	ui.Row(c).Children(func() {
		a.note(c, "工具连接配置")
		ui.Spacer(c)
		label := "高级 JSON"
		if s.Advanced {
			label = "返回表单"
		}
		a.action(c, label).OnClick(s.toggleAdvanced)
	})
	if s.Advanced {
		a.multiline(c, "标准 MCP 配置", &s.JSON, 8, 16)
	} else {
		a.dropdown(c, "连接方式", &s.Transport, []string{"stdio", "http"})
		if s.Transport == "stdio" {
			a.input(c, "启动命令", &s.Command, "npx", false)
			a.multiline(c, "命令参数 · 每行一个", &s.Args, 2, 5)
		} else {
			a.input(c, "服务 URL", &s.URL, "https://example.com/mcp", false)
		}
		a.multiline(c, "环境变量 / 请求头 · KEY=VALUE", &s.Variables, 2, 5)
		a.input(c, "超时（毫秒）", &s.Timeout, "30000", false)
		ui.Checkbox(c, &s.Trust, "信任此服务")
	}
	a.note(c, "保存后会实际连接受信任的服务；项目配置需先信任项目。修改时省略的凭据会保留。")
	a.primary(c, "保存配置").AlignSelf(ui.Start).Disabled(l.Loading["mcp-save"] || strings.TrimSpace(s.MCPName) == "").OnClick(func() {
		var config map[string]any
		var err error
		if s.Advanced {
			err = json.Unmarshal([]byte(s.JSON), &config)
			if config == nil && err == nil {
				err = errors.New("配置必须是 JSON 对象")
			}
		} else {
			config, err = s.mcpConfig()
		}
		if err != nil {
			l.Error = err.Error()
			return
		}
		scope := "user"
		if s.Scope == "项目" {
			scope = "project"
		}
		path := "/mcp/servers/" + pathSegment(strings.TrimSpace(s.MCPName)) + query(map[string]string{"workspace": s.Workspace, "scope": scope})
		a.operation("mcp-save", "PUT", path, config, func(v any) { apply(v); l.Notice = "MCP 配置已保存。" })
	})
}
func (a *preview) startMCPLogin(name, scope, workspace string) {
	l := a.Live
	if l.Loading["mcp-login"] {
		return
	}
	epoch, b := l.epoch, l.Client
	a.cancelMCPLogin()
	ctx, cancel := context.WithCancel(l.hostCtx)
	l.LoginCancel = cancel
	generation := l.LoginGeneration
	if b == nil {
		return
	}
	if l.Loading["mcp-login"] {
		return
	}
	l.Loading["mcp-login"] = true
	post := func(fn func()) {
		a.post(epoch, func() {
			if generation == l.LoginGeneration {
				fn()
			}
		})
	}
	go func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			post(func() { l.Loading["mcp-login"] = false; l.Error = err.Error() })
			return
		}
		defer listener.Close()
		redirect := "http://" + listener.Addr().String() + "/callback"
		path := "/mcp/servers/" + pathSegment(name)
		params := query(map[string]string{"workspace": workspace, "scope": scope})
		var info map[string]any
		err = b.request(ctx, "POST", path+"/login"+params, map[string]any{"redirect_url": redirect}, &info)
		if err != nil {
			post(func() { l.Loading["mcp-login"] = false; l.Error = err.Error() })
			return
		}
		state := str(info["state"])
		authURL := str(info["authorization_url"])
		u, parseErr := url.Parse(authURL)
		if parseErr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
			post(func() { l.Loading["mcp-login"] = false; l.Error = "服务返回了无效的授权地址" })
			return
		}
		codes := make(chan string, 1)
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" || r.URL.Query().Get("state") != state || state == "" || r.URL.Query().Get("code") == "" {
				http.Error(w, "Invalid authorization callback", 400)
				return
			}
			select {
			case codes <- r.URL.Query().Get("code"):
				fmt.Fprint(w, "授权已收到，可以返回 EasyAgent。")
			default:
				http.Error(w, "Already received", 409)
			}
		})}
		go server.Serve(listener)
		defer server.Close()
		post(func() {
			if generation != l.LoginGeneration {
				return
			}
			l.LoginURL = authURL
			l.Notice = "请在浏览器完成 MCP 授权。"
		})
		if ctx.Err() != nil {
			return
		}
		_ = mygo.Shell.OpenExternal(authURL)
		select {
		case code := <-codes:
			err = b.request(ctx, "POST", path+"/login/complete"+params, map[string]any{"state": state, "code": code}, nil)
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
			err = errors.New("MCP 授权等待超时，请重试")
		}
		post(func() {
			if generation != l.LoginGeneration {
				return
			}
			l.Loading["mcp-login"] = false
			l.LoginURL = ""
			if err != nil {
				l.Error = err.Error()
				return
			}
			l.Notice = "MCP 授权完成。"
			a.loadSettingsPage()
		})
	}()
}
func renderMarkdown(c *ui.Context, text string) { var doc markdownDocument; doc.render(c, text) }
func (a *preview) liveFeishuSettings(c *ui.Context) {
	l := a.Live
	s := &a.Settings
	if a.resourceFeedback(c, "feishu", a.loadSettingsPage) {
		return
	}
	if l.Data["feishu"] == nil {
		a.note(c, "飞书配置暂不可用。")
		a.action(c, "重试").OnClick(a.loadSettingsPage)
		return
	}
	m := object(l.Data["feishu"])
	managed := boolean(m["managed"])
	if !managed {
		a.heading(c, "扫码绑定飞书机器人")
		a.note(c, "使用飞书扫码授权应用；完成后确认保存凭据，再启动本机桥接。")
		if len(l.QRImage) > 0 {
			ui.Image(c, l.QRBitmap).Size(200, 200)
			if !time.Now().Before(l.QRExpires) {
				ui.Text(c, "二维码已过期，请重新生成").TextColor(c.Theme().Warning).FontSize(12)
			} else {
				a.note(c, "有效期至 "+l.QRExpires.Local().Format("15:04:05"))
			}
			if l.QRUserCode != "" {
				a.note(c, l.QRUserCode)
			}
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			a.action(c, "生成授权二维码").Disabled(l.Loading["qr-begin"]).OnClick(a.beginQR)
			if l.QRURL != "" {
				a.action(c, "打开授权页面").OnClick(func() {
					if safeMarkdownLink(l.QRURL) {
						go mygo.Shell.OpenExternal(l.QRURL)
					}
				})
				a.action(c, "复制链接").OnClick(func() { c.WriteClipboard(l.QRURL); c.Toast("已复制链接") })
			}
			if l.QRCode != "" {
				a.action(c, "取消扫码").OnClick(a.cancelQR)
			}
			if l.QRAuthorized {
				a.primary(c, "确认保存授权").Disabled(l.Loading["qr-confirm"] || !time.Now().Before(l.QRExpires)).OnClick(func() {
					code, generation := l.QRCode, l.QRGeneration
					a.operation("qr-confirm", "POST", "/settings/feishu/qr/confirm", map[string]any{"device_code": code}, func(any) {
						if generation != l.QRGeneration {
							return
						}
						l.QRCode = ""
						l.QRImage = nil
						l.QRAuthorized = false
						a.loadSettingsPage()
						l.Notice = "飞书凭据已保存。"
					})
				})
			}
		})
	}
	a.heading(c, "桥接状态")
	bridge := object(m["bridge"])
	a.preferenceRow(c, "当前应用", str(m["app_id"]), func() {})
	a.preferenceRow(c, "应用密钥", map[bool]string{true: "已配置", false: "未配置"}[boolean(m["secret_configured"])], func() {})
	status := "已停止"
	if boolean(bridge["running"]) {
		status = "进程运行中"
	}
	if managed {
		status = "由运行主机管理"
	}
	a.note(c, "当前状态："+status)
	if started := str(bridge["started_at"]); started != "" {
		a.note(c, "启动时间："+started)
	}
	if boolean(m["paired"]) {
		a.note(c, "已完成用户配对")
	} else if boolean(m["pairing_unavailable"]) {
		a.note(c, "当前运行主机无法生成配对码")
	} else {
		a.note(c, "尚未完成用户配对")
	}
	for _, name := range sortedKeys(object(m["services"])) {
		service := object(object(m["services"])[name])
		a.preferenceRow(c, name, feishuServiceState(str(service["state"]))+" · "+feishuAutostart(str(service["autostart"])), func() {})
	}
	if linger := str(m["linger"]); linger != "" {
		a.note(c, "用户常驻服务："+linger)
	}
	if err := str(bridge["error"]); err != "" {
		ui.Text(c, err).TextColor(c.Theme().Danger)
	}
	if !managed {
		a.primary(c, "启动 / 重启桥接").AlignSelf(ui.Start).Disabled(l.Loading["bridge"] || !boolean(m["secret_configured"])).OnClick(func() {
			a.operation("bridge", "POST", "/settings/feishu/bridge/restart", map[string]any{}, func(v any) { l.Data["feishu"] = v })
		})
	}
	a.heading(c, "自建应用凭据")
	if managed && str(m["app_id"]) != "" {
		s.AppID = str(m["app_id"])
	}
	idField := a.input(c, "App ID", &s.AppID, str(m["app_id"]), false)
	if managed && str(m["app_id"]) != "" {
		idField.Disabled(true)
	}
	a.input(c, "App Secret", &s.AppSecret, "填写后保存；留空保留服务端凭据", true)
	a.primary(c, "保存并重启桥接").AlignSelf(ui.Start).Disabled(l.Loading["feishu-save"]).OnClick(func() {
		id := s.AppID
		if id == "" || managed && str(m["app_id"]) != "" {
			id = str(m["app_id"])
		}
		if managed {
			a.operation("feishu-save", "PUT", "/settings/feishu", map[string]any{"app_id": id, "app_secret": s.AppSecret}, func(v any) { s.AppSecret = ""; l.Data["feishu"] = v })
		} else {
			a.saveLocalFeishu(id, s.AppSecret)
		}
	})
	if managed {
		a.action(c, "获取配对码").AlignSelf(ui.Start).Disabled(l.Loading["pairing"] || boolean(m["paired"]) || boolean(m["pairing_unavailable"])).OnClick(func() { a.resource("pairing", "/settings/feishu/pairing") })
		if command := str(object(l.Data["pairing"])["command"]); command != "" {
			a.note(c, command)
			valid := validFeishuPairing(object(l.Data["pairing"]), time.Now())
			if expiry, err := time.Parse(time.RFC3339, str(object(l.Data["pairing"])["expires"])); err == nil {
				a.note(c, "有效期至 "+expiry.Local().Format("01-02 15:04:05"))
			}
			if !valid {
				ui.Text(c, "配对码已过期，请重新获取").FontSize(12).TextColor(c.Theme().Warning)
			}
			a.action(c, "复制配对命令").AlignSelf(ui.Start).Disabled(!valid).OnClick(func() {
				if validFeishuPairing(object(l.Data["pairing"]), time.Now()) {
					c.WriteClipboard(command)
				}
			})
			a.action(c, "隐藏配对码").OnClick(func() { delete(l.Data, "pairing") })
		}
	}
}
func (a *preview) beginQR() {
	l := a.Live
	a.cancelQR()
	generation := l.QRGeneration
	a.operation("qr-begin", "POST", "/settings/feishu/qr/begin", map[string]any{}, func(v any) {
		if generation != l.QRGeneration {
			return
		}
		m := object(v)
		l.QRCode = str(m["device_code"])
		l.QRURL = str(m["qr_url"])
		l.QRUserCode = str(m["user_code"])
		l.QRInterval = time.Duration(max(3, number(m["interval"]))) * time.Second
		l.QRNext = time.Now().Add(l.QRInterval)
		l.QRExpires = time.Now().Add(time.Duration(number(m["expire_in"])) * time.Second)
		data, err := qrcode.Encode(l.QRURL, qrcode.Medium, 400)
		if err != nil {
			l.Error = err.Error()
		} else {
			l.QRImage = data
			l.QRBitmap, _ = ui.DecodeBitmap(data)
		}
	})
}
func (a *preview) pollQR() {
	l := a.Live
	code, generation := l.QRCode, l.QRGeneration
	a.operation("qr-poll", "POST", "/settings/feishu/qr/poll", map[string]any{"device_code": code}, func(v any) {
		if generation == l.QRGeneration && code == l.QRCode && time.Now().Before(l.QRExpires) {
			l.QRAuthorized = boolean(object(v)["authorized"])
		}
	})
}
func (a *preview) saveLocalFeishu(id, secret string) {
	l := a.Live
	if !a.Settings.host().Local {
		l.Error = "此远程核心未开放手动凭据保存，请使用该主机提供的扫码流程"
		return
	}
	if id == "" {
		l.Error = "请输入 App ID"
		return
	}
	if strings.ContainsAny(id+secret, "\r\n") {
		l.Error = "凭据格式无效"
		return
	}
	// This is the same core credential schema, scoped by this app's EA_HOME.
	path := filepath.Join(l.Dir, "core-home", "feishu-credentials.json")
	a.run("feishu-save", func(context.Context, *backendClient) (any, error) {
		var old map[string]any
		if data, err := os.ReadFile(path); err == nil {
			if json.Unmarshal(data, &old) != nil {
				return nil, errors.New("已有飞书凭据无法读取，未覆盖")
			}
		}
		if old == nil {
			old = map[string]any{}
		}
		if oldID := str(old["app_id"]); oldID != "" && oldID != id {
			return nil, errors.New("更换 App ID 需要重新扫码绑定；原授权已保留")
		}
		if secret == "" {
			secret = str(old["app_secret"])
		}
		if secret == "" {
			return nil, errors.New("首次配置需要 App Secret")
		}
		old["app_id"] = id
		old["app_secret"] = secret
		if str(old["platform"]) == "" {
			old["platform"] = "feishu"
		}
		return nil, atomicJSON(path, old)
	}, func(any) {
		a.Settings.AppSecret = ""
		a.operation("bridge", "POST", "/settings/feishu/bridge/restart", map[string]any{}, func(v any) { l.Data["feishu"] = v })
	})
}

func modelCatalogPath(base string) string {
	if strings.HasSuffix(base, "/v1") {
		return "/models"
	}
	return "/v1/models"
}

var feishuPairingCommand = regexp.MustCompile(`^/pair [A-Za-z0-9_-]+$`)

func validFeishuPairing(pairing map[string]any, now time.Time) bool {
	expiry, err := time.Parse(time.RFC3339, str(pairing["expires"]))
	return err == nil && now.Before(expiry) && feishuPairingCommand.MatchString(str(pairing["command"]))
}
func feishuServiceState(value string) string {
	if s := map[string]string{"active": "进程运行中", "activating": "正在启动", "inactive": "已停止", "failed": "启动失败", "deactivating": "正在停止"}[value]; s != "" {
		return s
	}
	return "状态未知"
}
func feishuAutostart(value string) string {
	if s := map[string]string{"enabled": "已启用", "disabled": "未启用", "masked": "已禁用", "static": "由其他服务启动", "enabled-runtime": "仅本次运行启用"}[value]; s != "" {
		return s
	}
	return "状态未知"
}
func (a *preview) cancelQR() {
	l := a.Live
	l.QRGeneration++
	l.QRCode = ""
	l.QRImage = nil
	l.QRBitmap = nil
	l.QRURL = ""
	l.QRUserCode = ""
	l.QRAuthorized = false
}
func (a *preview) cancelMCPLogin() {
	l := a.Live
	l.LoginGeneration++
	if l.LoginCancel != nil {
		l.LoginCancel()
		l.LoginCancel = nil
	}
	l.LoginURL = ""
	l.Loading["mcp-login"] = false
}
func (a *preview) cancelSettingsTransient() {
	if a.Settings.Tab == "models" {
		a.clearModelCheck()
	}
	a.cancelQR()
	a.cancelMCPLogin()
	delete(a.Live.Data, "pairing")
	a.Settings.AppSecret = ""
}
