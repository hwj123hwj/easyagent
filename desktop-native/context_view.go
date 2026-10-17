package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *preview) resourceFeedback(c *ui.Context, key string, retries ...func()) bool {
	retry := a.refreshWorkbench
	if len(retries) > 0 {
		retry = retries[0]
	}
	l := a.Live
	if err := l.ResourceErrors[key]; err != "" {
		ui.Column(c).Padding(16).Gap(10).Children(func() {
			ui.Text(c, err).FontSize(12).TextColor(c.Theme().Danger).Role(ui.RoleStatus)
			a.action(c, "重试").AlignSelf(ui.Start).OnClick(retry)
		})
		return true
	}
	if l.Loading[key] && l.Data[key] == nil {
		ui.Text(c, "正在读取…").Padding(16).FontSize(12).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
		return true
	}
	return false
}

func inspectContext(v any) any {
	switch v := v.(type) {
	case map[string]any:
		next := map[string]any{}
		for k, item := range v {
			if k == "data" {
				if text, ok := item.(string); ok {
					next[k] = fmt.Sprintf("[图片数据：%d 个 Base64 字符，界面省略]", len(text))
					continue
				}
			}
			next[k] = inspectContext(item)
		}
		return next
	case []any:
		next := make([]any, len(v))
		for i, item := range v {
			next[i] = inspectContext(item)
		}
		return next
	default:
		return v
	}
}

func nativeDisclosure(c *ui.Context, key, label string, body func()) {
	box := ui.Column(c.Key(key)).FillWidth().MinWidth(0).Gap(8).Padding(8, 0).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border)
	open := ui.Local(box, "expanded", func() bool { return false })
	box.Children(func() {
		glyph := "chevron-right"
		if *open {
			glyph = "chevron-down"
		}
		flatButton(c, glyph, label).Label(label).Justify(ui.Start).FillWidth().MinWidth(0).FontSize(12).Expanded(*open).OnClick(func() { *open = !*open })
		if *open {
			body()
		}
	})
}
func nativePre(c *ui.Context, text string) {
	if largeRichText(text) {
		nativeSource(c, text, 320, "详细内容源码")
		return
	}
	ui.Scroll(c).MaxHeight(320).MinWidth(0).Children(func() { ui.Text(c, text).Font("monospace").FontSize(11).Selectable().LineHeight(1.6) })
}

func (a *preview) contextWorkbench(c *ui.Context) {
	s := a.current()
	l := a.Live
	w := &l.Workbench
	ui.Scroll(c.Key("context/" + s.RemoteID)).Grow(1).MinHeight(0).Padding(14).Children(func() {
		ui.Column(c).MinWidth(0).Gap(12).Children(func() {
			a.note(c, "当前会话的逻辑输入，包含系统提示词、消息和工具定义。未发送的草稿与队列未计入；图片数据在此省略。")
			if s.Running || s.Pending {
				a.note(c, "任务正在运行，结束后可刷新上下文。")
				return
			}
			if a.resourceFeedback(c, "context") {
				return
			}
			m := object(l.Data["context"])
			usage := object(m["usage"])
			ui.Text(c, str(usage["model"])).FontSize(13).FontWeight(600)
			a.note(c, fmt.Sprintf("%d 条消息 · 估算 %s tokens", len(list(m["messages"])), formatCount(usage["estimated_tokens"])))
			nativeDisclosure(c, "system", "系统提示词 · 估算 "+formatCount(usage["system"])+" tokens", func() { nativePre(c, str(m["system"])) })
			nativeDisclosure(c, "messages", "模型输入消息 · 估算 "+formatCount(usage["messages"])+" tokens", func() {
				for i, raw := range list(m["messages"]) {
					entry := object(raw)
					nativeDisclosure(c, fmt.Sprint(i), fmt.Sprintf("%d · %s", i+1, str(entry["role"])), func() { nativePre(c, pretty(inspectContext(entry["message"]))) })
				}
			})
			nativeDisclosure(c, "tools", fmt.Sprintf("工具定义 · %d 个", len(list(m["tools"]))), func() {
				for _, raw := range list(m["tools"]) {
					tool := object(raw)
					nativeDisclosure(c, str(tool["name"]), str(tool["name"]), func() { nativePre(c, pretty(tool)) })
				}
			})
			records := list(m["compactions"])
			nativeDisclosure(c, "compactions", fmt.Sprintf("压缩记录 · %d 次", len(records)), func() {
				if len(records) == 0 {
					a.note(c, "此会话还没有全量压缩记录。")
				}
				for i := len(records) - 1; i >= 0; i-- {
					record := object(records[i])
					info := object(record["info"])
					nativeDisclosure(c, idString(record["id"]), "上下文摘要 · "+str(info["trigger"]), func() {
						a.note(c, fmt.Sprintf("消息 %s → %s · tokens %s → %s", formatCount(info["messages_before"]), formatCount(info["messages_after"]), formatCount(info["tokens_before"]), formatCount(info["tokens_after"])))
						nativePre(c, str(record["summary"]))
					})
				}
			})
			a.input(c, "手动压缩要求（可选）", &w.CompactInstructions, "保留接口约定、未完成的测试和下一步计划", false)
			a.note(c, "较早消息会归纳成摘要，近期完整消息会保留。")
			label := "压缩上下文"
			if l.Loading["compact/"+s.RemoteID] {
				label = "正在生成摘要…"
			}
			a.primary(c, label).AlignSelf(ui.End).Disabled(l.Loading["compact/"+s.RemoteID] || len(list(m["messages"])) < 2).OnClick(func() { a.compactNativeSession(s, w.CompactInstructions) })
		})
	})
}
func (a *preview) compactNativeSession(s *session, instructions string) {
	if s.Running || s.Pending || a.Live.configBusy() {
		return
	}
	sid := s.RemoteID
	a.run("compact/"+sid, func(ctx context.Context, b *backendClient) (any, error) {
		client := *b.HTTP
		client.Timeout = 5 * time.Minute
		copy := backendClient{URL: b.URL, Token: b.Token, HTTP: &client}
		var result any
		err := copy.request(ctx, http.MethodPost, sessionPath(sid)+"/compact", map[string]any{"custom_instructions": strings.TrimSpace(instructions)}, &result)
		return result, err
	}, func(any) {
		a.loadSession(s)
		if a.current() == s {
			a.Live.Workbench.CompactInstructions = ""
			a.resource("context", sessionPath(sid)+"/context")
		}
		a.Live.Notice = "上下文已压缩。"
	})
}

func (a *preview) contextUsagePanel(c *ui.Context, s *session) {
	u := s.Usage
	ui.Column(c).Gap(10).Children(func() {
		flatButton(c, "activity", "查看上下文与压缩记录").FontSize(12).OnClick(func() { a.ContextMenu = false; a.openResource("context") })
		ui.Text(c, "上下文用量 · 估算").FontSize(13).FontWeight(600)
		ui.Text(c, formatCount(u["estimated_tokens"])+" / "+formatCount(u["context_window"])+" tokens").FontSize(14)
		if window := number(u["context_window"]); window > 0 {
			ui.Progress(c, min(1, number(u["estimated_tokens"])/window)).Height(6).FillWidth()
		}
		for _, row := range []struct{ label, key string }{{"消息", "messages"}, {"系统提示词", "system"}, {"工具定义", "tools"}} {
			ui.Row(c).Children(func() { a.note(c, row.label); ui.Spacer(c); a.note(c, formatCount(u[row.key])) })
		}
		a.note(c, "图片等多模态内容未计入，实际用量以模型返回为准。")
		if !boolean(u["window_known"]) {
			a.note(c, "模型容量尚未登记，分母为运行时默认预算。")
		}
		if last := object(u["last_request"]); number(last["input_tokens"]) > 0 {
			ui.Text(c, "最近一次模型请求 · 实际").FontSize(12).FontWeight(600)
			a.note(c, "输入 "+formatCount(last["input_tokens"])+" · 输出 "+formatCount(last["output_tokens"]))
		}
	})
}
