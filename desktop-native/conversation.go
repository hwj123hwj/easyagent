package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type conversationMatch struct {
	Index, Offset int
	Snippet       string
}

func conversationText(m *message) string {
	if m.Tool != "" {
		return strings.Join([]string{m.Tool, pretty(m.Args), m.Text, m.Result, pretty(m.Details)}, "\n")
	}
	return m.Text
}
func conversationMatches(messages []*message, query string) []conversationMatch {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	needle := []rune(strings.Map(unicode.ToLower, query))
	var matches []conversationMatch
	for index, m := range messages {
		text := []rune(conversationText(m))
		folded := []rune(strings.Map(unicode.ToLower, string(text)))
		for at := 0; at+len(needle) <= len(folded); {
			found := true
			for j, r := range needle {
				if folded[at+j] != r {
					found = false
					break
				}
			}
			if !found {
				at++
				continue
			}
			matches = append(matches, conversationMatch{index, at, string(text[max(0, at-40):min(len(text), at+len(needle)+100)])})
			if len(matches) >= 5000 {
				return matches
			}
			at += len(needle)
		}
	}
	return matches
}
func copyNativeConversation(s *session) string {
	var parts []string
	for _, m := range s.Messages {
		name := "EasyAgent"
		body := m.Text
		switch {
		case m.User:
			name = "你"
		case m.Tool != "":
			name = "工具 · " + m.Tool
			if m.Args != nil {
				body = pretty(m.Args)
			}
			if m.Result != "" {
				body += "\n" + m.Result
			}
		case m.Kind != "":
			name = m.Kind
		}
		parts = append(parts, name+"\n"+body)
	}
	return strings.Join(parts, "\n\n")
}
func (a *preview) conversationFind(c *ui.Context, s *session) {
	editorFocused := false
	if f := a.Live.Workbench.Files[a.Live.Workbench.ActiveFile]; f != nil {
		editorFocused = (f.Code != nil && f.Code.Focused()) || (f.Markdown.source != nil && f.Markdown.source.Focused())
	}
	for _, m := range s.Messages {
		if m.Markdown.source != nil && m.Markdown.source.Focused() {
			editorFocused = true
		}
	}
	if !editorFocused && c.Shortcut(ui.Cmd, ui.KeyF) {
		s.FindOpen = true
		s.FindFocus = true
	}

	if !s.FindOpen {
		return
	}
	matches := conversationMatches(s.Messages, s.FindQuery)
	if len(matches) > 0 {
		s.FindIndex = (s.FindIndex + len(matches)) % len(matches)
	} else {
		s.FindIndex = 0
	}
	ui.Row(c.Key("conversation-find/"+s.RemoteID)).Gap(8).Padding(8, 20).Background(c.Theme().Surface).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		icon(c, "search")
		input := ui.TextInput(c, &s.FindQuery).Label("搜索当前会话内容").Placeholder("搜索消息、工具参数和输出…").Grow(1).MinWidth(0)
		if s.FindFocus {
			input.AutoFocus()
			s.FindFocus = false
		}
		if input.Changed() {
			s.FindIndex = 0
			s.FindJump = true
		}
		if input.FocusWithin() && input.Shortcut(ui.Shift, ui.KeyEnter) {
			s.FindIndex--
			s.FindJump = true
		} else if input.Submitted() {
			s.FindIndex++
			s.FindJump = true
		}
		label := "0 / 0"
		if len(matches) > 0 {
			label = fmt.Sprintf("%d / %d", s.FindIndex%len(matches)+1, len(matches))
		}
		ui.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted)
		iconButton(c, "chevron-up", "上一个匹配").Disabled(len(matches) == 0).OnClick(func() { s.FindIndex--; s.FindJump = true })
		iconButton(c, "chevron-down", "下一个匹配").Disabled(len(matches) == 0).OnClick(func() { s.FindIndex++; s.FindJump = true })
		iconButton(c, "x", "关闭会话搜索").OnClick(func() { s.FindOpen = false; s.FindQuery = ""; a.ReturnToComposer = true })
	})
	if c.Shortcut(0, ui.KeyEscape) {
		s.FindOpen = false
		s.FindQuery = ""
		a.ReturnToComposer = true
	}
}
func (a *preview) conversationMessages(c *ui.Context, s *session) {
	selected := -1
	if s.FindOpen {
		matches := conversationMatches(s.Messages, s.FindQuery)
		if len(matches) > 0 {
			selected = matches[(s.FindIndex%len(matches)+len(matches))%len(matches)].Index
		}
	}
	for i := 0; i < len(s.Messages); {
		if a.Live != nil && s.Messages[i].Kind == "tool" {
			end := i + 1
			for end < len(s.Messages) && s.Messages[end].Kind == "tool" {
				end++
			}
			a.toolGroup(c.Key(i), s, i, end, selected)
			i = end
			continue
		}
		a.conversationRow(c.Key(i), s, i, selected)
		i++
	}
}
func (a *preview) conversationRow(c *ui.Context, s *session, index, selected int) {
	row := ui.Column(c).MinWidth(0).Gap(8)
	if index == selected {
		row.Background(c.Theme().Accent.Alpha(.07)).Radius(6).Label("当前搜索匹配")
		if s.FindJump {
			row.ScrollIntoView()
			s.FindJump = false
		}
	}
	row.Children(func() { a.conversationMessage(c, s, index, s.Messages[index]) })
}

type toolGroupState struct {
	Open, Touched, Running, Failed bool
	Shown                          int
	Density                        string
}

func (a *preview) toolGroup(c *ui.Context, s *session, start, end, selected int) {
	tools := s.Messages[start:end]
	running, failed, success, declined, commands := false, false, 0, 0, 0
	files := map[string]bool{}
	duration := 0.0
	for _, m := range tools {
		running = running || m.Status == "执行中"
		failed = failed || m.Status == "失败" || m.Status == "未完成"
		if m.Status == "完成" {
			success++
		}
		if m.Status == "已拒绝" {
			declined++
		}
		if str(object(m.Args)["command"]) != "" {
			commands++
		}
		if path := toolFilePath(m); path != "" {
			files[path] = true
		}
		duration += m.DurationMS
	}
	root := ui.Column(c).MinWidth(0).Gap(5)
	state := ui.Local(root, "group", func() toolGroupState {
		return toolGroupState{Open: running || failed, Running: running, Failed: failed, Shown: 25, Density: s.Density}
	})
	if state.Density != s.Density {
		state.Density = s.Density
		state.Touched = false
		state.Open = running || failed || s.Density == "verbose"
		state.Failed = failed
	}
	// 失败出现时自动展开一次；用户手动收起后不再强制打开，
	// 只更新 Failed 标记，下次新失败再自动展开。
	if failed && !state.Failed {
		state.Open = true
		state.Touched = true
	}
	state.Failed = failed
	if state.Running && !running && !state.Touched {
		state.Open = false
	}
	state.Running = running
	if selected >= start && selected < end {
		state.Open = true
		state.Shown = max(state.Shown, selected-start+1)
	}
	title := fmt.Sprintf("%d 个工具调用", len(tools))
	if commands > 0 {
		title += fmt.Sprintf(" · %d 条命令", commands)
	}
	if len(files) > 0 {
		title += fmt.Sprintf(" · %d 个文件", len(files))
	}
	status := fmt.Sprintf("%d 成功", success)
	if failed {
		status += " · 有失败"
	}
	if declined > 0 {
		status += fmt.Sprintf(" · %d 已拒绝", declined)
	}
	if running {
		status = "执行中 · " + status
	}
	if duration > 0 {
		status += fmt.Sprintf(" · %.1fs", duration/1000)
	}
	root.Children(func() {
		head := ui.ButtonBase(c).Label("工具组："+title).FillWidth().MinWidth(0).Padding(9, 10).Gap(8).Radius(7).Background(c.Theme().Surface).Expanded(state.Open).OnClick(func() { state.Open = !state.Open; state.Touched = true })
		head.Children(func() {
			glyph := "chevron-right"
			if state.Open {
				glyph = "chevron-down"
			}
			icon(c, glyph)
			icon(c, "wrench")
			ui.Text(c, title).FontSize(12).Grow(1).MinWidth(0).SingleLine()
			ui.Text(c, status).FontSize(10).TextColor(c.Theme().TextMuted)
		})
		if !state.Open {
			return
		}
		for i := start; i < min(end, start+state.Shown); i++ {
			m := s.Messages[i]
			// 搜索命中行展开一次即可；不每帧强制覆盖用户的手动折叠。
			if selected == i && !m.ExpansionTouched {
				m.Expanded = true
				m.ExpansionTouched = true
			}
			a.conversationRow(c.Key(i), s, i, selected)
		}
		if state.Shown < len(tools) {
			a.action(c, fmt.Sprintf("显示更多调用（还剩 %d 个）", len(tools)-state.Shown)).AlignSelf(ui.Start).OnClick(func() { state.Shown += 25 })
		}
	})
}
func toolFilePath(m *message) string {
	for _, key := range []string{"file_path", "absolute_path", "path", "filename"} {
		if path := str(object(m.Args)[key]); path != "" {
			return path
		}
	}
	return ""
}
func toolDescription(m *message) string {
	args := object(m.Args)
	for _, key := range []string{"command", "path", "file_path", "pattern", "query", "url"} {
		if text := str(args[key]); text != "" {
			return text
		}
	}
	return ""
}

var toolReadLines = regexp.MustCompile(`(?i)\((\d+)\s+lines?\)|read\s+lines:\s*(\d+-\d+)`)
var toolExitCode = regexp.MustCompile(`(?i)exit\s*code:?\s*(-?\d+)`)
var ansiSequence = regexp.MustCompile("\\x1b\\[[0-?]*[ -/]*[@-~]")

func toolSummary(m *message) string {
	if m.Status != "完成" {
		return ""
	}
	if match := toolExitCode.FindStringSubmatch(m.Result); len(match) > 1 {
		return "退出码 " + match[1]
	}
	if match := toolReadLines.FindStringSubmatch(m.Result); len(match) > 1 {
		if match[1] != "" {
			return "读取 " + match[1] + " 行"
		}
		return "读取行 " + match[2]
	}
	if path := toolFilePath(m); path != "" {
		return portableBase(path)
	}
	return ""
}
func (a *preview) nativeTool(c *ui.Context, m *message) {
	ui.Column(c).Gap(6).MinWidth(0).Children(func() {
		label := "展开工具结果"
		if m.Expanded {
			label = "收起工具结果"
		}
		head := ui.ButtonBase(c).Label(label).FillWidth().MinWidth(0).Padding(7, 9).Gap(8).Radius(6).Background(c.Theme().Surface).Expanded(m.Expanded).OnClick(func() { m.Expanded = !m.Expanded; m.ExpansionTouched = true })
		head.Children(func() {
			glyph := "chevron-right"
			if m.Expanded {
				glyph = "chevron-down"
			}
			icon(c, glyph)
			mark := "check"
			color := c.Theme().TextMuted
			if m.Status == "执行中" || m.Kind == "thought" {
				mark = "activity"
			}
			if m.Status == "失败" || m.Status == "未完成" {
				mark = "alert-circle"
				color = c.Theme().Danger
			}
			if m.Status == "已拒绝" {
				mark = "shield"
				color = c.Theme().Warning
			}
			ui.Row(c).TextColor(color).Children(func() { icon(c, mark) })
			ui.Text(c, m.Tool).FontSize(12).FontWeight(500)
			ui.Text(c, toolDescription(m)).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine()
			if summary := toolSummary(m); summary != "" {
				ui.Text(c, summary).FontSize(10).TextColor(c.Theme().TextMuted).MaxWidth(110).SingleLine()
			}
			ui.Text(c, m.Status).FontSize(10).TextColor(color)
		})
		if m.Status == "完成" {
			a.musicCard(c, m)
		}
		if !m.Expanded {
			return
		}
		ui.Column(c).Padding(8, 10).Gap(8).MinWidth(0).Children(func() {
			if m.Args != nil {
				nativeDisclosure(c, "arguments", "调用参数", func() { nativePre(c, pretty(m.Args)) })
			}
			if len(m.Details) > 0 {
				nativeDisclosure(c, "details", "执行详情", func() { nativePre(c, pretty(m.Details)) })
			}
			if path := toolFilePath(m); path != "" {
				flatButton(c, "file", path).AlignSelf(ui.Start).FontSize(11).MinWidth(0).Label("工具文件：" + path).OnClick(func() { a.openResource("files"); a.openNativeFile(path) })
			}
			if m.Kind == "thought" || m.Kind == "compaction" {
				a.renderSessionMarkdown(c, &m.Markdown, m.Text, a.current(), "", false)
				return
			}
			args := object(m.Args)
			old, next := "", ""
			for _, key := range []string{"old_string", "old_text", "oldText"} {
				if text := str(args[key]); text != "" {
					old = text
					break
				}
			}
			for _, key := range []string{"new_string", "new_text", "newText"} {
				if text := str(args[key]); text != "" {
					next = text
					break
				}
			}
			if old != "" && next != "" {
				nativeDisclosure(c, "inline-diff", "修改预览", func() {
					root := ui.Column(c).Height(230).MinWidth(0)
					diff := *ui.Local(root, "diff", func() *editor.DiffState {
						return &editor.DiffState{Old: editor.NewBuffer(old), New: editor.NewBuffer(next), Wrap: true}
					})
					root.Children(func() { editor.DiffView(c, diff).Fill() })
				})
			}
			result := m.Result
			if result == "" && m.Args == nil {
				result = m.Text
			}
			if result == "" && m.Status == "未完成" {
				result = "此工具调用未完成或已中断"
			}
			nativePre(c, ansiSequence.ReplaceAllString(result, ""))
			a.action(c, "复制输出").AlignSelf(ui.Start).OnClick(func() { c.WriteClipboard(result); c.Toast("已复制输出") })
		})
	})
}
