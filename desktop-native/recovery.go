package main

import (
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
)

type nativeRecovery struct{ Kind, Message, Settings, Action string }

var authenticationError = regexp.MustCompile(`(?i)认证|unauthori[sz]ed|invalid.*(?:key|token)|\b40[13]\b|authentication`)
var limitError = regexp.MustCompile(`(?i)\b429\b|rate.?limit|限流|quota|配额`)
var connectionError = regexp.MustCompile(`(?i)network|fetch failed|ECONN|timeout|timed? ?out|断线|连接|超时|socket`)

func classifyRecovery(text string) nativeRecovery {
	if authenticationError.MatchString(text) {
		tab := "models"
		if regexp.MustCompile(`(?i)服务认证|连接令牌|bearer`).MatchString(text) {
			tab = "connections"
		}
		return nativeRecovery{"authentication", "凭据未被服务接受。检查连接令牌或模型密钥后重新发送。", tab, "检查凭据"}
	}
	if limitError.MatchString(text) {
		return nativeRecovery{"rate_limit", "模型服务暂时限流或配额不足。检查额度，稍后再发送；任务不会自动重试。", "models", "检查模型服务"}
	}
	if connectionError.MatchString(text) {
		return nativeRecovery{"network", "连接中断或请求超时。重新连接后先核对执行记录，再决定是否重发。", "connections", "检查连接"}
	}
	return nativeRecovery{"other", "任务未正常完成。请展开工具结果和错误详情，核对已执行的操作。", "", "查看详情"}
}
func (a *preview) recoveryView(c *ui.Context, s *session) {
	if s.Run == nil || s.Run.active() || s.Run.State != "failed" && s.Run.State != "interrupted" {
		return
	}
	recovery := classifyRecovery(s.Run.Error)
	ui.Column(c.Key("recovery/"+s.runID())).Gap(10).Padding(12).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Role(ui.RoleStatus).Children(func() {
		ui.Text(c, recovery.Message).FontSize(12).LineHeight(1.6)
		if s.Run.Error != "" {
			nativeDisclosure(c, "error", "错误详情", func() { nativePre(c, s.Run.Error) })
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if recovery.Settings != "" {
				a.action(c, recovery.Action).OnClick(func() { a.openSettings(recovery.Settings) })
			}
			if recovery.Kind == "network" {
				a.action(c, "重新连接").Disabled(a.Live.Connecting).OnClick(a.connectLive)
			}
			if strings.TrimSpace(s.Run.Prompt) != "" {
				a.action(c, "检查后重新发送").Disabled(s.Sending || a.Live.configBusy() || !a.Live.Connected).OnClick(func() {
					run := *s.Run
					a.Live.Confirm = &nativeConfirmation{"重新发送这条任务？", "可能重复已经完成的操作。请先检查工具记录，确认可以再次执行。", func() {
						a.sendLivePrompt(s, run.Prompt, run.Prompt, run.Attachments, run.Inputs)
					}}
				})
			}
		})
	})
}
