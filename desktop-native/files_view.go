package main

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type nativeDirectory struct {
	Entries       []any
	Open, Loading bool
	Error         string
}

func portableBase(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
func relativeWorkspace(root, path string) string {
	root = strings.TrimRight(strings.ReplaceAll(root, "\\", "/"), "/")
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.TrimPrefix(path, root+"/")
}
func (a *preview) bindFileScope() {
	w := &a.Live.Workbench
	s := a.current()
	scope := a.Live.Config.ActiveHost + "\x00" + s.Workspace
	if w.FileScope == scope && w.Directories != nil {
		return
	}
	w.FileScope = scope
	w.Directories = map[string]*nativeDirectory{}
	w.Search = ""
	w.SearchFiles = nil
	w.SearchLoaded = false
	w.SearchLoading = false
	delete(a.Live.ResourceErrors, "file-search")
	w.ShowTree = false
	w.ActiveFile = ""
	for _, key := range w.Tabs {
		if f := w.Files[key]; f != nil && f.Host == a.Live.Config.ActiveHost && f.Workspace == s.Workspace {
			w.ActiveFile = key
		}
	}
}
func (a *preview) readNativeDirectory(path string) {
	l, w := a.Live, &a.Live.Workbench
	if path == "" || l.Client == nil {
		return
	}
	if entry := w.Directories[path]; entry != nil {
		return
	}
	d := &nativeDirectory{Open: true, Loading: true}
	w.Directories[path] = d
	epoch, ctx, b, scope, sid := l.epoch, l.hostCtx, l.Client, w.FileScope, a.current().RemoteID
	go func() {
		var entries []any
		err := b.request(ctx, "GET", a.workspacePath("list-dir", path, sid), nil, &entries)
		a.post(epoch, func() {
			if w.FileScope != scope {
				return
			}
			d.Loading = false
			if err != nil {
				d.Error = err.Error()
			} else {
				d.Entries = entries
			}
		})
	}()
}
func (a *preview) searchNativeFiles() {
	l, w := a.Live, &a.Live.Workbench
	if w.SearchLoaded || w.SearchLoading || l.Client == nil {
		return
	}
	w.SearchLoading = true
	epoch, ctx, b, scope, sid, root := l.epoch, l.hostCtx, l.Client, w.FileScope, a.current().RemoteID, a.current().Workspace
	go func() {
		var paths []string
		err := b.request(ctx, "GET", a.workspacePath("search-files", root, sid), nil, &paths)
		a.post(epoch, func() {
			if w.FileScope != scope {
				return
			}
			w.SearchLoading = false
			if err != nil {
				l.ResourceErrors["file-search"] = err.Error()
				return
			}
			delete(l.ResourceErrors, "file-search")
			w.SearchFiles = paths
			w.SearchLoaded = true
		})
	}()
}
func fuzzyPaths(paths []string, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	type match struct {
		path  string
		score int
	}
	var matches []match
	for _, path := range paths {
		value := strings.ToLower(path)
		at, score := 0, 0
		ok := true
		for _, r := range query {
			i := strings.IndexRune(value[at:], r)
			if i < 0 {
				ok = false
				break
			}
			score += i
			at += i + len(string(r))
		}
		if ok {
			if strings.HasPrefix(strings.ToLower(portableBase(path)), query) {
				score -= 100
			}
			matches = append(matches, match{path, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].path < matches[j].path
		}
		return matches[i].score < matches[j].score
	})
	out := []string{}
	for _, m := range matches[:min(80, len(matches))] {
		out = append(out, m.path)
	}
	return out
}
func (a *preview) nativeTreeNode(c *ui.Context, path, name string, isDir bool, depth int) {
	w := &a.Live.Workbench
	if !isDir {
		fileButton(c, name).Label("打开文件："+path).Drag(fileReference{Path: path, Workspace: a.current().Workspace, Host: a.Live.Config.ActiveHost}).Justify(ui.Start).FillWidth().MinWidth(0).Padding(5, 8+float32(depth)*13).FontSize(12).Tooltip(path).OnClick(func() { a.openNativeFile(path) })
		return
	}
	d := w.Directories[path]
	expanded := d != nil && d.Open
	glyph := "chevron-right"
	if expanded {
		glyph = "chevron-down"
	}
	ui.ButtonBase(c.Key(path)).Label("目录："+path).Expanded(expanded).Justify(ui.Start).FillWidth().MinWidth(0).Padding(5, 8+float32(depth)*13).Gap(6).FontSize(12).OnClick(func() {
		if d == nil {
			a.readNativeDirectory(path)
		} else {
			d.Open = !d.Open
		}
	}).Children(func() { icon(c, glyph); icon(c, "folder"); ui.Text(c, name).SingleLine().MinWidth(0) })
	if !expanded {
		return
	}
	if d.Loading {
		a.note(c, "正在读取目录…")
		return
	}
	if d.Error != "" {
		ui.Column(c).Padding(8, 16).Gap(8).Children(func() {
			a.note(c, d.Error)
			a.action(c, "重试目录").AlignSelf(ui.Start).OnClick(func() { delete(w.Directories, path); a.readNativeDirectory(path) })
		})
		return
	}
	for _, raw := range d.Entries {
		entry := object(raw)
		a.nativeTreeNode(c.Key(str(entry["path"])), str(entry["path"]), str(entry["name"]), boolean(entry["isDir"]), depth+1)
	}
}
func (a *preview) filesWorkbench(c *ui.Context) {
	l, w, s := a.Live, &a.Live.Workbench, a.current()
	a.bindFileScope()
	a.restoreFileTabs()
	if s.Workspace == "" {
		ui.Column(c).Grow(1).Padding(20).Children(func() { a.note(c, "选择项目后浏览文件。") })
		return
	}
	f := w.Files[w.ActiveFile]
	if f != nil && (f.Host != l.Config.ActiveHost || f.Workspace != s.Workspace) {
		f = nil
	}
	browsing := w.ShowTree || f == nil
	ui.Row(c).Height(36).Gap(4).Padding(0, 6).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		label := "目录"
		glyph := "folder"
		if browsing && f != nil {
			label = "返回文件"
			glyph = "arrow-left"
		}
		flatButton(c, glyph, label).FontSize(11).Disabled(f == nil).OnClick(func() { w.ShowTree = !w.ShowTree })
		ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(34).Children(func() {
			ui.Row(c).Gap(2).Children(func() {
				for _, key := range w.Tabs {
					file := w.Files[key]
					if file == nil || file.Host != l.Config.ActiveHost || file.Workspace != s.Workspace {
						continue
					}
					ui.Row(c.Key(key)).Gap(1).Background(func() ui.Color {
						if key == w.ActiveFile {
							return c.Theme().SurfaceHover
						}
						return c.Theme().Background
					}()).Children(func() {
						title := portableBase(file.Path)
						if file.Code != nil && file.Code.Buffer.Modified() {
							title += " •"
						}
						flatButton(c, "file", title).Label("文件标签：" + file.Path).FontSize(11).MaxWidth(160).MinWidth(0).OnClick(func() { w.ActiveFile = key; w.ShowTree = false })
						iconButton(c, "x", "关闭标签："+file.Path).Size(22, 26).OnClick(func() { a.requestCloseNativeFile(key) })
					})
				}
			})
		})
		if f != nil && !browsing {
			iconButton(c, "download", "导出文件").Size(26, 28).OnClick(func() { a.exportNativeFile(f) })
			open := iconButton(c, "menu", "文件打开方式").Size(26, 28).OnClick(func() { w.FileOpenMenu = !w.FileOpenMenu })
			a.menuPanel(c, open, &w.FileOpenMenu, func() {
				menuOption(c, "copy", "复制文件路径", false).OnClick(func() { c.WriteClipboard(f.Path); w.FileOpenMenu = false })
				menuOption(c, "folder", "在 Finder 中显示", false).Disabled(!a.Settings.host().Local).OnClick(func() { w.FileOpenMenu = false; go mygo.Shell.ShowItemInFolder(f.Path) })
				menuOption(c, "terminal", "在终端中打开", false).Disabled(!a.Settings.host().Local).OnClick(func() {
					w.FileOpenMenu = false
					if !w.TerminalOpen {
						a.toggleTerminal()
					}
					if w.Term != nil {
						w.Term.Send([]byte("cd " + shellQuote(filepath.Dir(f.Path)) + "\r"))
					} else if w.TerminalStarting {
						w.PendingTerminalText = "cd " + shellQuote(filepath.Dir(f.Path)) + "\r"
					}
				})
				menuOption(c, "plus", "添加到消息", false).OnClick(func() {
					a.addFileReference(s, fileReference{Path: f.Path, Workspace: s.Workspace, Host: l.Config.ActiveHost})
					w.FileOpenMenu = false
					a.ReturnToComposer = true
				})
			})
		}
	})
	if browsing {
		ui.Column(c).Padding(8, 12).Gap(8).Children(func() {
			ui.Text(c, portableBase(s.Workspace)).FontSize(12).FontWeight(500).Tooltip(s.Workspace)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				icon(c, "search")
				input := ui.TextInputBase(c, &w.Search).Label("搜索工作区文件").Placeholder("搜索文件名…").Grow(1).MinWidth(0).FontSize(12)
				if input.Changed() && strings.TrimSpace(w.Search) != "" {
					a.searchNativeFiles()
				}
				if w.Search != "" {
					iconButton(c, "x", "清除文件搜索").Size(20, 22).OnClick(func() { w.Search = "" })
				}
			})
		})
		ui.Scroll(c.Key("tree/"+w.FileScope)).Grow(1).MinHeight(0).Padding(0, 6, 12, 6).Children(func() {
			if strings.TrimSpace(w.Search) != "" {
				if w.SearchLoading {
					a.note(c, "正在搜索…")
					return
				}
				if err := l.ResourceErrors["file-search"]; err != "" {
					a.note(c, err)
					a.action(c, "重试搜索").OnClick(a.searchNativeFiles)
					return
				}
				results := fuzzyPaths(w.SearchFiles, w.Search)
				if len(results) == 0 {
					a.note(c, "没有匹配的文件。")
				}
				for _, rel := range results {
					path := strings.TrimRight(s.Workspace, "/\\") + "/" + rel
					ui.Column(c.Key(path)).Children(func() {
						fileButton(c, portableBase(rel)).Label("打开文件：" + path).Drag(fileReference{Path: path, Workspace: s.Workspace, Host: l.Config.ActiveHost}).Justify(ui.Start).FillWidth().MinWidth(0).FontSize(12).OnClick(func() { a.openNativeFile(path) })
						if dir := filepath.Dir(rel); dir != "." {
							ui.Text(c, dir).FontSize(10).TextColor(c.Theme().TextMuted).Padding(0, 26, 4, 26)
						}
					})
				}
				return
			}
			if w.Directories[s.Workspace] == nil {
				a.readNativeDirectory(s.Workspace)
			}
			a.nativeTreeNode(c, s.Workspace, portableBase(s.Workspace), true, 0)
		})
		return
	}
	if f.Loading {
		ui.Text(c, "正在读取文件…").Padding(16)
		return
	}
	if f.Error != "" {
		ui.Column(c).Padding(16).Gap(10).Children(func() {
			ui.Text(c, f.Error).FontSize(12).TextColor(c.Theme().Danger)
			a.action(c, "重新打开文件").AlignSelf(ui.Start).OnClick(func() { a.closeNativeFile(w.ActiveFile); a.openNativeFile(f.Path) })
		})
		return
	}
	ui.Row(c).Wrap().Gap(6).Padding(6, 10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, portableBase(s.Workspace)+" › "+strings.ReplaceAll(relativeWorkspace(s.Workspace, f.Path), "/", " › ")).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Tooltip(f.Path)
		if f.Code != nil {
			if f.Editing {
				if f.Code.Buffer.Modified() {
					ui.Text(c, "未保存").FontSize(11).TextColor(c.Theme().Warning)
				}
				a.action(c, "取消编辑").FontSize(11).OnClick(func() { f.Code.Buffer.Reset(f.Original); f.Editing = false })
				a.primary(c, "保存").FontSize(11).Disabled(l.Loading["file-save"]).OnClick(func() { a.saveNativeFile(f) })
			} else {
				if markdownPath(f.Path) {
					tabButton(c, "预览", f.Preview).FontSize(11).OnClick(func() { f.Preview = true })
					tabButton(c, "源码", !f.Preview).FontSize(11).OnClick(func() { f.Preview = false })
				}
				flatButton(c, "edit", "编辑").FontSize(11).OnClick(func() { f.Editing = true })
			}
		}
	})
	if f.Kind == "pdf" {
		a.pdfFileView(c, f)
	} else if f.Image != nil || f.SVG != nil {
		ui.Scroll(c).Grow(1).TrackScroll(&f.Scroll).Padding(12).Children(func() {
			if f.Image != nil {
				ui.Image(c, f.Image).FillWidth().Fit(ui.Contain)
			} else {
				ui.Image(c, f.SVG).FillWidth().Fit(ui.Contain)
			}
		})
	} else if f.Preview && !f.Editing && markdownPath(f.Path) {
		ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&f.Scroll).Padding(14).Children(func() { a.renderSessionMarkdown(c, &f.Markdown, f.Code.Buffer.String(), a.current(), f.Path, false) })
	} else if f.Code != nil {
		f.Code.ReadOnly = !f.Editing
		editor.View(c, f.Code).Grow(1).FillWidth().Label("文件编辑器")
		if f.Editing && f.Code.Focused() && c.Shortcut(ui.Cmd, ui.KeyS) {
			a.saveNativeFile(f)
		}
	} else {
		ui.Column(c).Grow(1).Padding(20).Gap(10).Children(func() {
			a.note(c, "此文件不支持文本预览。")
			a.action(c, "导出文件").AlignSelf(ui.Start).OnClick(func() { a.exportNativeFile(f) })
		})
	}
}
func shellQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'" }
func markdownPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}
func (a *preview) requestCloseNativeFile(key string) {
	f := a.Live.Workbench.Files[key]
	if f == nil {
		return
	}
	if f.Code != nil && f.Code.Buffer.Modified() {
		a.Live.Confirm = &nativeConfirmation{"关闭未保存的文件？", f.Path, func() { a.closeNativeFile(key) }}
	} else {
		a.closeNativeFile(key)
	}
}
func (a *preview) openNativeFile(path string) {
	l, w, s := a.Live, &a.Live.Workbench, a.current()
	host, sid, workspace := l.Config.ActiveHost, s.RemoteID, s.Workspace
	if l.Client == nil || sid == "" {
		l.Error = "请先连接运行主机并创建会话"
		return
	}
	key := workspaceFileKey(host, workspace, path)
	if w.Files[key] != nil {
		w.ActiveFile = key
		w.ShowTree = false
		return
	}
	f := &nativeFile{Path: path, SessionID: sid, Workspace: workspace, Host: host, Loading: true, Zoom: 1}
	w.Files[key] = f
	w.Tabs = append(w.Tabs, key)
	w.ActiveFile = key
	w.ShowTree = false
	ext := strings.ToLower(filepath.Ext(path))
	resource := "read-file"
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		f.Kind = "image"
		resource = "read-file-base64"
	case ".pdf":
		f.Kind = "pdf"
		resource = "file-data"
	}
	epoch, ctx, b := l.epoch, l.hostCtx, l.Client
	go func() {
		var value map[string]any
		err := b.request(ctx, "GET", a.workspacePath(resource, path, sid), nil, &value)
		var data []byte
		var bitmap *ui.Bitmap
		var svg *ui.SVG
		if err == nil && resource != "read-file" {
			data, err = base64.StdEncoding.DecodeString(str(value["data"]))
			if err == nil && f.Kind == "image" {
				if ext == ".svg" {
					svg, err = ui.ParseSVG(data)
				} else {
					bitmap, err = ui.DecodeBitmap(data)
				}
			}
		}
		a.post(epoch, func() {
			if w.Files[key] != f {
				return
			}
			f.Loading = false
			if err != nil {
				f.Error = err.Error()
				return
			}
			f.Data = data
			f.Image = bitmap
			f.SVG = svg
			if resource == "read-file" {
				f.Original = str(value["content"])
				if !strings.ContainsRune(f.Original, 0) {
					buf := editor.NewBuffer(f.Original)
					buf.SetLanguage(editor.LanguageFor(path))
					f.Code = &editor.State{Buffer: buf, ReadOnly: true}
					f.Preview = markdownPath(path)
					if f.RestoredPreview != nil {
						f.Preview = *f.RestoredPreview
						f.RestoredPreview = nil
					}
				}
			}
			if f.Kind == "pdf" {
				a.loadPDFPage(f, 0)
			}
		})
	}()
}

// The PDF implementation is native to the host OS and only returns pixels to MyGo.
func (a *preview) loadPDFPage(f *nativeFile, page int) {
	f.Loading = true
	f.Error = ""
	f.PDFPage = page
	data, epoch := f.Data, a.Live.epoch
	go func() {
		result, err := renderNativePDF(data, page)
		a.post(epoch, func() {
			f.Loading = false
			if err != nil {
				f.Error = err.Error()
				return
			}
			f.PDFCount = result.Count
			f.Image = result.Image
		})
	}()
}
func (a *preview) pdfFileView(c *ui.Context, f *nativeFile) {
	ui.Row(c).Gap(8).Padding(6, 12).Children(func() {
		iconButton(c, "arrow-left", "PDF 上一页").Disabled(f.PDFPage <= 0 || f.Loading).OnClick(func() { a.loadPDFPage(f, f.PDFPage-1) })
		ui.Text(c, fmt.Sprintf("%d / %d", f.PDFPage+1, f.PDFCount)).FontSize(12)
		iconButton(c, "chevron-right", "PDF 下一页").Disabled(f.PDFPage+1 >= f.PDFCount || f.Loading).OnClick(func() { a.loadPDFPage(f, f.PDFPage+1) })
		ui.Spacer(c)
		flatButton(c, "", "−").Label("缩小 PDF").OnClick(func() { f.Zoom = max(.5, f.Zoom-.25) })
		flatButton(c, "", fmt.Sprintf("%.0f%%", f.Zoom*100)).OnClick(func() { f.Zoom = 1 })
		flatButton(c, "", "+").Label("放大 PDF").OnClick(func() { f.Zoom = min(3, f.Zoom+.25) })
	})
	if f.Image != nil {
		ui.Scroll(c).Grow(1).TrackScroll(&f.Scroll).Padding(12).Children(func() {
			ui.ScrollHorizontal(c).Children(func() {
				iw, ih := f.Image.Size()
				width := max(200, a.Settings.Preferences.WorkbenchWidth-80)
				if width < 300 {
					width = 340
				}
				ui.Image(c, f.Image).Width(width * f.Zoom).Height(width * f.Zoom * float32(ih) / float32(iw))
			})
		})
	}
}
