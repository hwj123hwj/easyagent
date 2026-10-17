package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
)

type fileReference struct {
	Path      string `json:"path"`
	Workspace string `json:"workspace"`
	Host      string `json:"-"`
}

var fileMention = regexp.MustCompile(`(?:^|\s)@([^\s@]*)$`)

func mentionQuery(text string) (string, bool) {
	m := fileMention.FindStringSubmatch(text)
	if len(m) == 0 {
		return "", false
	}
	return m[1], true
}
func (a *preview) addFileReference(s *session, value fileReference) {
	if value.Host != a.Live.Config.ActiveHost || value.Workspace != s.Workspace {
		a.Live.Error = "这个文件来自另一个主机或项目，请在对应会话中引用。"
		return
	}
	if s.Sending || a.Live.Loading["queue-add/"+s.RemoteID] {
		a.Live.Error = "正在提交消息，请等待确认后添加引用"
		return
	}
	for _, item := range s.FileReferences {
		if item.Path == value.Path && item.Workspace == value.Workspace {
			return
		}
	}
	if len(s.FileReferences) >= 12 {
		a.Live.Error = "最多引用 12 个文件"
		return
	}
	s.FileReferences = append(s.FileReferences, value)
	a.ReturnToComposer = true
}
func (a *preview) mentionMatches(s *session) []string {
	query, open := mentionQuery(s.Draft)
	if !open || s.MentionDismissed == s.Draft || s.RemoteID == "" || s.Workspace == "" {
		return nil
	}
	key := "mention/" + s.RemoteID
	if a.Live.Data[key] == nil && !a.Live.Loading[key] && a.Live.ResourceErrors[key] == "" {
		a.resource(key, a.workspacePath("search-files", s.Workspace, s.RemoteID))
	}
	var matches []string
	for _, raw := range list(a.Live.Data[key]) {
		value := str(raw)
		if strings.Contains(strings.ToLower(value), strings.ToLower(query)) {
			matches = append(matches, value)
			if len(matches) == 12 {
				break
			}
		}
	}
	return matches
}
func (a *preview) completeMention(s *session) {
	matches := a.mentionMatches(s)
	if len(matches) == 0 {
		return
	}
	selected := matches[(s.MentionIndex%len(matches)+len(matches))%len(matches)]
	a.addFileReference(s, fileReference{Path: path.Join(strings.ReplaceAll(s.Workspace, "\\", "/"), selected), Workspace: s.Workspace, Host: a.Live.Config.ActiveHost})
	if at := strings.LastIndex(s.Draft, "@"); at >= 0 {
		s.Draft = s.Draft[:at]
	}
}
func (a *preview) promptFileMentions(c *ui.Context, anchor ui.Element, s *session) {
	_, open := mentionQuery(s.Draft)
	open = open && s.MentionDismissed != s.Draft && s.RemoteID != "" && s.Workspace != ""
	matches := a.mentionMatches(s)
	popup := a.menuPanel(c, anchor, &open, func() {
		menuCaption(c, "引用工作区文件 · Enter/Tab 选择")
		key := "mention/" + s.RemoteID
		if err := a.Live.ResourceErrors[key]; err != "" {
			a.note(c, err)
			a.action(c, "重试文件引用").OnClick(func() { delete(a.Live.ResourceErrors, key); delete(a.Live.Data, key) })
		}
		if a.Live.Loading[key] {
			a.note(c, "正在读取文件…")
		}
		if len(matches) == 0 && !a.Live.Loading[key] && a.Live.ResourceErrors[key] == "" {
			a.note(c, "没有匹配的文件")
		}
		ui.Scroll(c).Label("文件引用列表").MaxHeight(260).Children(func() {
			for i, value := range matches {
				row := menuOption(c.Key(value), "file", value, i == s.MentionIndex%len(matches)).OnClick(func() { s.MentionIndex = i; a.completeMention(s) })
				if s.MentionJump && i == s.MentionIndex%len(matches) {
					row.ScrollIntoView()
				}
			}
		})
	})
	s.MentionJump = false
	if popup.Valid() {
		popup.AttachTo(anchor, ui.AnchorTopLeft, ui.AnchorBottomLeft).Width(max(260, anchor.Bounds().W)).Margin(0, 0, 8, 0)
	}
	if !open {
		if _, active := mentionQuery(s.Draft); active {
			s.MentionDismissed = s.Draft
		}
	}
}
func (a *preview) inputReferences(c *ui.Context, s *session) {
	for i, item := range s.FileReferences {
		ui.Row(c.Key(fmt.Sprintf("reference/%d", i))).Gap(6).MinWidth(0).Children(func() {
			icon(c, "file")
			ui.Text(c, portableBase(item.Path)).FontSize(11).MinWidth(0).SingleLine().Tooltip(item.Path)
			a.note(c, "工作区引用")
			iconButton(c, "x", "移除引用："+item.Path).Disabled(s.Sending || a.Live.Loading["queue-add/"+s.RemoteID]).Size(22, 22).OnClick(func() { s.FileReferences = append(s.FileReferences[:i], s.FileReferences[i+1:]...) })
		})
	}
}
func promptInputMap(s *session, attachments []map[string]any) map[string]any {
	inputs := map[string]any{}
	ids := []string{}
	for _, att := range attachments {
		if id := str(att["id"]); id != "" {
			ids = append(ids, id)
		}
	}
	inputs["attachments"] = ids
	files := []map[string]string{}
	for _, ref := range s.FileReferences {
		files = append(files, map[string]string{"path": ref.Path, "workspace": ref.Workspace})
	}
	inputs["files"] = files
	return inputs
}
