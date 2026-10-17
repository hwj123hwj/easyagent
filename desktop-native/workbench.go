package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/gorilla/websocket"
)

type nativeFile struct {
	RestoredPreview                            *bool
	Kind, Error                                string
	Loading, Editing                           bool
	Data                                       []byte
	SVG                                        *ui.SVG
	Scroll                                     ui.ScrollState
	Markdown                                   markdownDocument
	PDFPage, PDFCount                          int
	Zoom                                       float32
	Path, Original, SessionID, Workspace, Host string
	Code                                       *editor.State
	Preview                                    bool
	Image                                      *ui.Bitmap
}
type nativeWorkbench struct {
	KBSearchAt                                                                       time.Time
	SavedFiles                                                                       map[string]fileWorkspace
	RestoredFiles                                                                    map[string]bool
	PendingTerminalText, TerminalError                                               string
	KBView, KBCategory, KBTag                                                        string
	PendingReviewPath                                                                string
	Scope                                                                            string
	FileScope                                                                        string
	ShowTree, FileOpenMenu, SearchLoaded, SearchLoading                              bool
	Directories                                                                      map[string]*nativeDirectory
	SearchFiles                                                                      []string
	CompactInstructions                                                              string
	Tab, Directory, Search, ActiveFile, SelectedRun, SelectedChange, KBQuery, KBPath string
	KBEntry                                                                          map[string]any
	Files                                                                            map[string]*nativeFile
	Tabs                                                                             []string
	Diff                                                                             *editor.DiffState
	TerminalHost, TerminalSession, TerminalWorkspace                                 string
	TerminalGeneration                                                               uint64
	Term                                                                             *terminal.Terminal
	TerminalOpen, TerminalStarting                                                   bool
}

func newNativeWorkbench() nativeWorkbench {
	return nativeWorkbench{Tab: "files", Files: map[string]*nativeFile{}, SavedFiles: map[string]fileWorkspace{}, RestoredFiles: map[string]bool{}}
}
func (w *nativeWorkbench) matchesTerminal(host, sid string) bool {
	return w.TerminalHost == host && w.TerminalSession == sid
}
func (w *nativeWorkbench) close() {
	w.TerminalGeneration++
	w.TerminalHost, w.TerminalSession, w.TerminalWorkspace = "", "", ""
	if w.Term != nil {
		_ = w.Term.Close()
		w.Term = nil
	}
	w.TerminalOpen = false
	w.TerminalStarting = false
	w.PendingTerminalText = ""
	w.TerminalError = ""
}
func (a *preview) workspacePath(resource, path, sid string) string {
	return "/workspace/" + resource + query(map[string]string{"path": path, "session_id": sid})
}
func (a *preview) openResource(tab string) {
	a.Workbench = true
	a.Live.Workbench.Tab = tab
	a.refreshWorkbench()
}
func (a *preview) refreshWorkbench() {
	a.bindWorkbench()
	l := a.Live
	w := &l.Workbench
	s := a.current()
	if l.Client == nil {
		return
	}
	switch w.Tab {
	case "files":
		a.bindFileScope()
		w.Directories = map[string]*nativeDirectory{}
		w.SearchLoaded = false
		a.readNativeDirectory(s.Workspace)
	case "review":
		if s.RemoteID != "" {
			a.resource("review", sessionPath(s.RemoteID)+"/run-files")
		}
	case "context":
		if s.RemoteID != "" {
			a.resource("context", sessionPath(s.RemoteID)+"/context")
		}
	case "tools":
		a.resource("tools", "/tools")
	case "profile":
		a.resource("profile", "/profile")
	case "kb":
		a.resource("kb", "/kb/entries"+query(map[string]string{"q": w.KBQuery, "category": w.KBCategory, "tag": w.KBTag}))
		if l.Data["kb-stats"] == nil {
			a.resource("kb-stats", "/kb/stats")
		}
		if l.Data["kb-categories"] == nil {
			a.resource("kb-categories", "/kb/categories")
		}
		if w.KBView == "tags" && l.Data["kb-tags"] == nil {
			a.resource("kb-tags", "/kb/tags")
		}
		if w.KBView == "health" && l.Data["kb-health"] == nil {
			a.resource("kb-health", "/kb/health")
		}
	}
}

var workbenchTabs = []struct{ id, label, icon string }{
	{"review", "审查", "git-branch"}, {"files", "文件", "folder"}, {"plan", "计划", "file"}, {"tasks", "任务", "check"}, {"kb", "知识库", "book"}, {"profile", "记忆", "user"}, {"context", "上下文", "activity"}, {"tools", "工具", "wrench"},
}

func (a *preview) liveWorkbench(c *ui.Context) {
	a.bindWorkbench()
	l := a.Live
	w := &l.Workbench
	title := "工作台"
	for _, tab := range workbenchTabs {
		if tab.id == w.Tab {
			title = tab.label
		}
	}
	width, _ := c.Size()
	maxWidth := width - 320
	if a.Sidebar && width >= 1100 {
		maxWidth -= 260
	}
	maxWidth = min(900, maxWidth)
	paneWidth := a.Settings.Preferences.WorkbenchWidth
	if paneWidth < 340 {
		paneWidth = 440
	}
	paneWidth = max(340, min(paneWidth, maxWidth))
	ui.Row(c).Width(paneWidth).MinWidth(0).AlignItems(ui.Stretch).Background(c.Theme().Background).BorderWidth(0, 0, 0, 1).BorderColor(c.Theme().Border).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Height(44).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				a.text(c, title).FontSize(13).FontWeight(500)
				ui.Spacer(c)
				iconButton(c, "refresh", "刷新工作台").OnClick(func() {
					if l != nil && l.Workbench.Tab == "kb" {
						delete(l.Data, "kb-stats")
						delete(l.Data, "kb-categories")
						delete(l.Data, "kb-tags")
						delete(l.Data, "kb-health")
					}
					a.refreshWorkbench()
				})
				iconButton(c, "x", "关闭工作台").OnClick(func() { a.Workbench = false })
			})
			switch w.Tab {
			case "files":
				a.filesWorkbench(c)
			case "review":
				a.reviewWorkbench(c)
			case "tasks", "plan":
				a.tasksWorkbench(c)
			case "context":
				a.contextWorkbench(c)
			case "tools":
				ui.Scroll(c).Grow(1).MinHeight(0).Padding(12).Children(func() { ui.Text(c, pretty(l.Data[w.Tab])).Font("monospace").FontSize(11).LineHeight(1.5).Selectable() })
			case "profile":
				a.profileWorkbench(c)
			case "kb":
				a.kbWorkbench(c)
			}
		})
		ui.Column(c).Width(44).Padding(6, 4).Gap(2).BorderWidth(0, 0, 0, 1).BorderColor(c.Theme().Border).Children(func() {
			for _, tab := range workbenchTabs[:6] {
				button := iconButton(c.Key(tab.id), tab.icon, tab.label).Size(36, 34).OnClick(func() { a.openResource(tab.id) })
				if w.Tab == tab.id {
					button.TextColor(c.Theme().Accent).Background(c.Theme().SurfaceHover)
				}
			}
		})
		a.resizeEdge(c, "调整工作台宽度", false, paneWidth, 340, maxWidth, func(value float32) { a.Settings.Preferences.WorkbenchWidth = value; a.Settings.savePreferences() })
	})
}
func (a *preview) liveTerminal(c *ui.Context) {
	w := &a.Live.Workbench
	if !w.TerminalOpen {
		return
	}
	_, windowHeight := c.Size()
	height := a.Settings.Preferences.TerminalHeight
	if height < 120 {
		height = 280
	}
	height = min(height, max(120, windowHeight-400))
	ui.Column(c).Height(height).MinHeight(120).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
		ui.Row(c).Height(32).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			a.note(c, "终端 · "+filepath.Base(w.TerminalWorkspace))
			ui.Spacer(c)
			iconButton(c, "x", "关闭终端").Tooltip("收起终端（保持 shell 运行）").OnClick(func() { w.TerminalOpen = false })
		})
		if w.Term != nil {
			select {
			case <-w.Term.Done():
				ui.Row(c).Gap(8).Padding(4, 12).Children(func() {
					a.note(c, "终端已断开")
					a.action(c, "重新连接终端").OnClick(func() { w.close(); a.toggleTerminal() })
				})
			default:
			}
			terminal.View(c, w.Term).Grow(1).FillWidth()
		} else {
			if w.TerminalError != "" {
				a.note(c, w.TerminalError)
				a.action(c, "重新连接终端").OnClick(func() { w.close(); a.toggleTerminal() })
			} else {
				a.note(c, "正在连接终端…")
			}
		}
		a.resizeEdge(c, "调整终端高度", true, height, 120, min(720, windowHeight-400), func(value float32) { a.Settings.Preferences.TerminalHeight = value; a.Settings.savePreferences() })
	})
}
func (a *preview) saveNativeFile(f *nativeFile) {
	if f.Host != a.Live.Config.ActiveHost {
		a.Live.Error = "文件属于其他运行主机，未写入"
		return
	}
	l := a.Live
	text := f.Code.Buffer.String()
	original := f.Original
	path := a.workspacePath("read-file", f.Path, f.SessionID)
	write := a.workspacePath("write-file", f.Path, f.SessionID)
	a.run("file-save", func(ctx context.Context, b *backendClient) (any, error) {
		var current map[string]any
		if err := b.request(ctx, "GET", path, nil, &current); err != nil {
			return nil, err
		}
		if str(current["content"]) != original {
			return nil, errors.New("文件已在主机上改变，未覆盖；请关闭后重新打开并比较")
		}
		return nil, b.request(ctx, "PUT", write, map[string]any{"content": text}, nil)
	}, func(any) {
		f.Original = text
		f.Editing = false
		if f.Code.Buffer.String() == text {
			f.Code.Buffer.MarkSaved()
		}
		l.Notice = "文件已保存。"
	})
}
func (a *preview) closeNativeFile(key string) {
	w := &a.Live.Workbench
	delete(w.Files, key)
	for i, k := range w.Tabs {
		if k == key {
			w.Tabs = append(w.Tabs[:i], w.Tabs[i+1:]...)
			break
		}
	}
	w.ActiveFile = ""
	if len(w.Tabs) > 0 {
		for i := len(w.Tabs) - 1; i >= 0; i-- {
			key := w.Tabs[i]
			f := w.Files[key]
			if f.Host == a.Live.Config.ActiveHost && f.Workspace == a.current().Workspace {
				w.ActiveFile = key
				break
			}
		}
	}
}
func (a *preview) exportNativeFile(f *nativeFile) {
	if f.Host != a.Live.Config.ActiveHost {
		a.Live.Error = "文件属于其他运行主机，未导出"
		return
	}
	path := a.workspacePath("file-data", f.Path, f.SessionID)
	a.background(func(ctx context.Context, b *backendClient) (any, error) {
		var m map[string]any
		if err := b.request(ctx, "GET", path, nil, &m); err != nil {
			return nil, err
		}
		data, err := base64.StdEncoding.DecodeString(str(m["data"]))
		if err != nil {
			return nil, err
		}
		target, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "导出文件", DefaultPath: filepath.Base(f.Path)})
		if err != nil || target == "" {
			return nil, err
		}
		return target, os.WriteFile(target, data, 0600)
	}, func(v any) {
		if str(v) != "" {
			a.Live.Notice = "文件导出已完成。"
		}
	})
}
func (a *preview) tasksWorkbench(c *ui.Context) {
	s := a.current()
	plan := a.Live.Workbench.Tab == "plan"
	ui.Scroll(c.Key(a.Live.Workbench.Tab + s.RemoteID)).Grow(1).MinHeight(0).Padding(14).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			if plan {
				done := 0
				for _, entry := range s.Plan {
					if entry.Status == "completed" {
						done++
					}
				}
				a.note(c, fmt.Sprintf("%d / %d 已完成", done, len(s.Plan)))
				if len(s.Plan) == 0 {
					a.note(c, "暂无执行计划。Agent 更新计划后会显示在这里。")
				}
				for _, entry := range s.Plan {
					glyph := "circle"
					color := c.Theme().TextMuted
					if entry.Status == "completed" {
						glyph = "check"
						color = c.Theme().Accent
					} else if entry.Status == "in_progress" {
						glyph = "activity"
						color = c.Theme().Text
					}
					ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
						ui.Box(c).TextColor(color).Children(func() { icon(c, glyph) })
						ui.Text(c, entry.Content).FontSize(12).Grow(1).MinWidth(0)
					})
				}
				return
			}
			running, total := 0, 0
			for _, m := range s.Messages {
				if m.Kind == "tool" {
					total++
					if m.Status == "执行中" || m.Status == "未完成" {
						running++
					}
				}
			}
			a.note(c, fmt.Sprintf("执行中 %d · 共 %d 项", running, total))
			if total == 0 {
				a.note(c, "暂无工具任务。")
			}
			for i := len(s.Messages) - 1; i >= 0; i-- {
				m := s.Messages[i]
				if m.Kind != "tool" {
					continue
				}
				glyph := "activity"
				color := c.Theme().TextMuted
				if m.Status == "完成" {
					glyph = "check"
					color = c.Theme().Accent
				} else if m.Status == "失败" {
					glyph = "alert"
					color = c.Theme().Danger
				}
				ui.Row(c.Key(m.CallID)).Gap(10).Children(func() {
					ui.Box(c).TextColor(color).Children(func() { icon(c, glyph) })
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() { ui.Text(c, m.Tool).FontSize(12).Font("monospace"); a.note(c, m.Status) })
				})
			}
		})
	})
}

// remoteTerminal implements the existing server's framed WS terminal protocol
// for MyGo's native terminal emulator, including output credit acknowledgments.
type remoteTerminal struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	pending []byte
}

func (t *remoteTerminal) writeJSON(v any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return t.conn.WriteJSON(v)
}
func (t *remoteTerminal) Read(p []byte) (int, error) {
	for len(t.pending) == 0 {
		kind, data, err := t.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if kind == websocket.BinaryMessage {
			t.pending = data
		} else {
			var v map[string]any
			_ = json.Unmarshal(data, &v)
			if str(v["type"]) == "error" {
				return 0, errors.New(str(v["message"]))
			}
		}
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	if len(t.pending) == 0 {
		if err := t.writeJSON(map[string]any{"type": "ack"}); err != nil {
			return n, err
		}
	}
	return n, nil
}
func (t *remoteTerminal) Write(p []byte) (int, error) {
	err := t.writeJSON(map[string]any{"type": "input", "data": string(p)})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
func (t *remoteTerminal) Resize(cols, rows int) error {
	return t.writeJSON(map[string]any{"type": "resize", "cols": cols, "rows": rows})
}
func (t *remoteTerminal) Close() error { return t.conn.Close() }
func (a *preview) toggleTerminal() {
	l := a.Live
	w := &l.Workbench
	if w.TerminalOpen {
		w.TerminalOpen = false
		return
	}
	if w.matchesTerminal(l.Config.ActiveHost, a.current().RemoteID) && (w.TerminalStarting || w.Term != nil) {
		w.TerminalOpen = true
		return
	}
	w.close()
	s := a.current()
	if s.RemoteID == "" {
		l.Error = "请先创建会话，再打开终端"
		return
	}
	if l.Client == nil {
		l.Error = "请先连接运行主机"
		return
	}
	w.TerminalOpen = true
	w.TerminalStarting = true
	w.TerminalGeneration++
	generation := w.TerminalGeneration
	w.TerminalHost, w.TerminalSession, w.TerminalWorkspace = l.Config.ActiveHost, s.RemoteID, s.Workspace
	epoch, ctx, b, sid := l.epoch, l.hostCtx, l.Client, s.RemoteID
	go func() {
		u, _ := url.Parse(b.URL + "/terminal" + query(map[string]string{"session_id": sid, "cols": "80", "rows": "24"}))
		if u.Scheme == "https" {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
		h := http.Header{}
		h.Set("Authorization", "Bearer "+b.Token)
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), h)
		var term *terminal.Terminal
		if err == nil {
			conn.SetReadLimit(128 << 10)
			term, err = terminal.New(terminal.Options{Conn: &remoteTerminal{conn: conn}, Scrollback: 4 << 20})
			if err != nil {
				_ = conn.Close()
			}
		}
		a.postOwned(epoch, func() {
			if generation != w.TerminalGeneration || !w.matchesTerminal(l.Config.ActiveHost, a.current().RemoteID) {
				if term != nil {
					_ = term.Close()
				}
				return
			}
			w.TerminalStarting = false
			if err != nil {
				w.TerminalError = "终端连接失败：" + err.Error()
				w.PendingTerminalText = ""
				return
			}
			if a.current().RemoteID != sid {
				_ = term.Close()
				return
			}
			w.Term = term
			if w.PendingTerminalText != "" {
				term.Send([]byte(w.PendingTerminalText))
				w.PendingTerminalText = ""
			}
		}, func() {
			if term != nil {
				_ = term.Close()
			}
		})
	}()
}

var _ io.ReadWriteCloser = (*remoteTerminal)(nil)

// The same splitter handles pointer drags and keyboard resizing without changing
// the session, file buffers, or terminal connection being resized.
func (a *preview) resizeEdge(c *ui.Context, label string, vertical bool, value, minimum, maximum float32, save func(float32)) {
	maximum = max(minimum, maximum)
	edge := ui.Box(c.Key(label)).Label(label).Role(ui.RoleSplitter).Focusable().Absolute()
	if vertical {
		edge.Top(-3).Left(0).Height(6).FillWidth().Cursor(ui.CursorResizeNS)
	} else {
		edge.Left(-3).Top(0).Width(6).FillHeight().Cursor(ui.CursorResizeEW)
	}
	set := func(v float32) {
		next := max(minimum, min(v, maximum))
		if next != value {
			save(next)
		}
	}
	clickTime := ui.Local(edge, "clickTime", func() time.Time {
		return time.Time{}
	})
	edge.OnClick(func() {
		now := time.Now()
		if !clickTime.IsZero() && now.Sub(*clickTime) < 350*time.Millisecond {
			if !vertical {
				defaultWidth := float32(440)
				if defaultWidth < minimum || defaultWidth > maximum {
					defaultWidth = (minimum + maximum) / 2
				}
				set(defaultWidth)
			}
			*clickTime = time.Time{}
		} else {
			*clickTime = now
		}
	})
	if dx, dy, held := edge.Dragged(); held {
		if vertical {
			if dy < -4 || dy > 4 {
				set(value - dy)
			}
		} else {
			if value-dx < minimum-40 {
				a.Workbench = false
				return
			}
			if dx < -4 || dx > 4 {
				set(value - dx)
			}
		}
	}
	edge.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputKeyDown {
			return false
		}
		if !vertical && ev.Key == ui.KeyLeft || vertical && ev.Key == ui.KeyUp {
			set(value + 10)
			return true
		}
		if !vertical && ev.Key == ui.KeyRight || vertical && ev.Key == ui.KeyDown {
			set(value - 10)
			return true
		}
		return false
	})
}
