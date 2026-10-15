package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"math"
	"strings"
)

type message struct {
	Args                                         any
	ExpansionTouched                             bool
	DurationMS                                   float64
	Details                                      map[string]any
	Images                                       []messageImage
	EntryID, CallID, RunID, Kind, Status, Result string
	User                                         bool
	Text                                         string
	Tool                                         string
	Expanded                                     bool
	Markdown                                     markdownDocument
}
type nativePlanEntry struct{ Content, Status string }
type session struct {
	FileReferences                               []fileReference
	MentionIndex                                 int
	MentionDismissed                             string
	Density                                      string
	FindOpen, FindFocus, FindJump                bool
	FindQuery                                    string
	FindIndex, Unread                            int
	LastNotifiedRun                              string
	UpdatedAt                                    string
	Plan                                         []nativePlanEntry
	RemoteID, Workspace, Application, AccessMode string
	Pinned, Sending                              bool
	Run                                          *runInfo
	InfoVersion                                  uint64
	Seq                                          uint64
	Confirmations                                []confirmation
	Queue                                        messageQueue
	Usage                                        map[string]any
	Attachments                                  []map[string]any
	SentDraft, PendingRequest                    string
	ID                                           int
	Title, Project, Draft, Model                 string
	Messages                                     []*message
	Archived                                     bool
	Scroll                                       ui.ScrollState
	Running                                      bool
	Pending                                      bool
	Reply                                        []rune
	Position                                     int
}
type preview struct {
	Window                                                                         *mygo.Window
	Live                                                                           *liveState
	Settings                                                                       settingsState
	Sessions                                                                       []*session
	Active                                                                         int
	NextID                                                                         int
	Search                                                                         string
	Archived                                                                       bool
	Sidebar                                                                        bool
	Workbench                                                                      bool
	ModelMenu, PermissionMenu, MoreMenu, HostMenu, SlashMenu, NewMenu, ContextMenu bool
	ModelSearch                                                                    string
	Permission                                                                     string
	Composer                                                                       ui.Handle
	ReturnToComposer                                                               bool
}

const demoIntro = "可以。主会话页已经用 **Go + MyGo 原生控件**复刻，继续沿用青夜配色和现在的布局。\n\n这次先把最常用的交互打通：\n\n- **会话与草稿**：搜索、切换、新建、归档和从回复分支。\n- **消息与工具**：原生 Markdown、代码复制、流式输出与停止。\n- **输入与审批**：模型选择、斜杠命令、允许一次与拒绝。\n\n输入 `/approval` 可以试一次工具审批；也可以直接发消息，体验模拟流式回复。\n\n这是独立预览，消息和工具使用演示数据。关闭后不保存，不会调用模型或操作你的文件。"

func newPreview() *preview {
	return &preview{
		Active: 1, NextID: 5, Sidebar: true, Permission: "每次询问", Settings: newSettings(),
		Sessions: []*session{
			{ID: 1, Title: "青夜界面试验", Project: "easyagent", Model: "coding", Messages: []*message{
				{User: true, Text: "先把主会话页做出来，看看纯 Go 的效果。"},
				{Tool: "读取界面样式 · 3 个文件（演示）", Text: "desktop/src/styles/personal.css\ndesktop/src/components/PromptBar.tsx\ndesktop/src/components/Sidebar.tsx"},
				{Text: demoIntro},
			}},
			{ID: 2, Title: "原生 Markdown 与代码", Project: "easyagent", Model: "coding", Messages: []*message{
				{User: true, Text: "代码块、表格也能用原生控件显示吗？"},
				{Text: "可以，Markdown 直接解析成原生文本和布局。\n\n```go\nui.Column(c).Gap(12).Children(func() {\n    ui.Text(c, \"青夜 · EasyAgent\")\n})\n```\n\n| 区域 | 实现 |\n| --- | --- |\n| 消息 | 原生 RichText |\n| 输入 | 原生 TextArea |\n| 代码 | 横向滚动与复制 |\n\n阅读 [MyGo 文档](https://github.com/egoist/mygo/tree/main/docs)。"},
			}},
			{ID: 3, Title: "工具审批演示", Project: "hwj", Model: "coding", Pending: true, Messages: []*message{
				{User: true, Text: "演示一次工具审批。"},
				{Text: "准备演示 `go test ./...` 的审批流程。请在下方选择允许一次或拒绝。这里不会执行任何命令。"},
			}},
			{ID: 4, Title: "已归档的会话", Project: "hwj", Model: "coding", Archived: true, Messages: []*message{{Text: "这是一条归档演示会话，可以恢复后继续输入。"}}},
		},
	}
}
func (a *preview) current() *session {
	for _, s := range a.Sessions {
		if s.ID == a.Active {
			return s
		}
	}
	return a.Sessions[0]
}
func (a *preview) selectSession(id int) {
	if a.Live != nil && a.Active != id {
		a.Live.Workbench.close()
	}
	a.Active = id
	a.current().Unread = 0
	if a.Live != nil {
		a.Live.EditQueueID = ""
		a.Live.EditQueueOwner = nil
	}
	a.ModelMenu = false
	a.PermissionMenu = false
	a.MoreMenu = false
	a.SlashMenu = false
	a.NewMenu = false
	if a.Live != nil {
		a.loadSession(a.current())
	}
}
func (a *preview) newSession() {
	if a.Live != nil {
		a.createLiveSession("")
		return
	}
	s := &session{ID: a.NextID, Title: "新会话", Project: "easyagent", Model: a.current().Model}
	a.NextID++
	a.Sessions = append([]*session{s}, a.Sessions...)
	a.Archived = false
	a.Search = ""
	a.selectSession(s.ID)
	a.Composer.Focus()
}
func (a *preview) fork(index int) {
	if a.Live != nil {
		a.forkLive(index)
		return
	}
	old := a.current()
	s := &session{ID: a.NextID, Title: old.Title + " · 分支", Project: old.Project, Model: old.Model}
	a.NextID++
	for _, m := range old.Messages[:index+1] {
		s.Messages = append(s.Messages, &message{User: m.User, Text: m.Text, Tool: m.Tool})
	}
	a.Sessions = append([]*session{s}, a.Sessions...)
	a.Archived = false
	a.Search = ""
	a.selectSession(s.ID)
}
func follow(s *session) { s.Scroll.Y = math.MaxFloat32 }
func (a *preview) submit() {
	if a.Live != nil {
		a.submitLive()
		return
	}
	s := a.current()
	text := strings.TrimSpace(s.Draft)
	if text == "" || s.Running || s.Pending || s.Archived {
		return
	}
	s.Messages = append(s.Messages, &message{User: true, Text: text})
	s.Draft = ""
	a.SlashMenu = false
	if strings.HasPrefix(text, "/approval") {
		s.Messages = append(s.Messages, &message{Text: "准备演示 `go test ./...` 的审批流程。不会执行真实命令，请选择允许一次或拒绝。"})
		s.Pending = true
		follow(s)
		return
	}
	reply := "收到。这是一条 **本地模拟回复**，用于检查原生界面的流式显示和输入体验。\n\n你发送的内容是：\n\n> " + strings.ReplaceAll(text, "\n", "\n> ") + "\n\n切换会话后，这条演示回复会继续；点击停止可以中断输出。草稿保留在各自的会话中。"
	a.start(s, reply)
}
func (a *preview) start(s *session, reply string) {
	s.Messages = append(s.Messages, &message{})
	s.Running = true
	s.Reply = []rune(reply)
	s.Position = 0
	follow(s)
}
func (a *preview) tick() {
	if a.Live != nil {
		a.liveTick()
		return
	}
	for _, s := range a.Sessions {
		if !s.Running {
			continue
		}
		following := s.Scroll.Y >= s.Scroll.MaxY-12
		s.Position = min(s.Position+3, len(s.Reply))
		s.Messages[len(s.Messages)-1].Text = string(s.Reply[:s.Position])
		if following {
			follow(s)
		}
		if s.Position == len(s.Reply) {
			s.Running = false
			s.Reply = nil
		}
	}
}
func (a *preview) stop() {
	if a.Live != nil {
		a.cancelLive()
		return
	}
	s := a.current()
	if !s.Running {
		return
	}
	s.Running = false
	s.Reply = nil
	s.Messages[len(s.Messages)-1].Text += "\n\n*已停止输出。*"
}
func (a *preview) approve(allow bool) {
	if a.Live != nil {
		a.confirmLive(allow)
		return
	}
	s := a.current()
	if !s.Pending {
		return
	}
	s.Pending = false
	if allow {
		s.Messages = append(s.Messages, &message{Tool: "go test ./... · 演示结果", Text: "允许一次。这里仅展示模拟输出，没有启动进程。\nok  example/native-preview  (demo)"})
		a.start(s, "**审批完成。** 已显示模拟测试结果，没有执行真实工具。你可以继续发消息，或从这条回复创建分支。")
	} else {
		s.Messages = append(s.Messages, &message{Text: "**已拒绝。** 演示工具未执行，你可以继续对话。"})
		follow(s)
	}
}

// Starting a demo from the toolbar never consumes the composer's unsent draft.
func (a *preview) requestApproval() {
	s := a.current()
	if s.Running || s.Pending || s.Archived {
		return
	}
	s.Messages = append(s.Messages,
		&message{User: true, Text: "演示一次工具审批。"},
		&message{Text: "准备演示 `go test ./...` 的审批流程。不会执行真实命令，请选择允许一次或拒绝。"},
	)
	s.Pending = true
	follow(s)
}
func (a *preview) completeCommand(s *session) {
	if strings.HasPrefix(s.Draft, "/") && !strings.ContainsAny(s.Draft, " \n") {
		s.Draft = "/approval "
	} else {
		s.Draft = "/approval " + s.Draft
	}
	a.SlashMenu = false
}

func (a *preview) toggleMenu(open *bool) {
	next := !*open
	a.ModelMenu, a.PermissionMenu, a.MoreMenu, a.HostMenu, a.SlashMenu, a.NewMenu, a.ContextMenu = false, false, false, false, false, false, false
	*open = next
}
