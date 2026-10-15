package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type filePosition struct {
	Top     float32 `json:"top"`
	Preview bool    `json:"preview"`
}
type fileWorkspace struct {
	Tabs      []string                `json:"tabs"`
	Active    string                  `json:"active"`
	Positions map[string]filePosition `json:"positions"`
	Updated   int64                   `json:"updated"`
}

func workspaceFileKey(host, workspace, path string) string {
	return host + "\x00" + workspace + "\x00" + path
}
func workspaceContains(root, path string) bool {
	root = strings.TrimRight(strings.ReplaceAll(root, "\\", "/"), "/")
	path = strings.ReplaceAll(path, "\\", "/")
	if root == "" || !strings.HasPrefix(path, root+"/") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
func (a *preview) restoreFileTabs() {
	l, w, s := a.Live, &a.Live.Workbench, a.current()
	if s.RemoteID == "" || l.Client == nil || w.RestoredFiles[w.FileScope] {
		return
	}
	w.RestoredFiles[w.FileScope] = true
	entry := w.SavedFiles[w.FileScope]
	for _, path := range entry.Tabs[max(0, len(entry.Tabs)-20):] {
		if !workspaceContains(s.Workspace, path) {
			continue
		}
		key := workspaceFileKey(l.Config.ActiveHost, s.Workspace, path)
		if w.Files[key] != nil {
			continue
		}
		a.openNativeFile(path)
		if f := w.Files[key]; f != nil {
			if p, ok := entry.Positions[path]; ok && p.Top >= 0 && !math.IsNaN(float64(p.Top)) && !math.IsInf(float64(p.Top), 0) {
				f.Scroll.Y = p.Top
				f.RestoredPreview = &p.Preview
			}
		}
	}
	key := workspaceFileKey(l.Config.ActiveHost, s.Workspace, entry.Active)
	if w.Files[key] != nil {
		w.ActiveFile = key
	}
}
func (a *preview) fileStateSnapshot() json.RawMessage {
	w := &a.Live.Workbench
	scopes := map[string]fileWorkspace{}
	for _, key := range w.Tabs {
		f := w.Files[key]
		if f == nil || !workspaceContains(f.Workspace, f.Path) {
			continue
		}
		scope := f.Host + "\x00" + f.Workspace
		entry, ok := scopes[scope]
		if !ok {
			entry.Positions = map[string]filePosition{}
			entry.Updated = w.SavedFiles[scope].Updated
		}
		entry.Tabs = append(entry.Tabs, f.Path)
		entry.Positions[f.Path] = filePosition{Top: f.Scroll.Y, Preview: f.Preview}
		entry.Active = f.Path
		scopes[scope] = entry
	}
	for scope := range w.RestoredFiles {
		if _, ok := scopes[scope]; !ok {
			scopes[scope] = fileWorkspace{Positions: map[string]filePosition{}, Updated: w.SavedFiles[scope].Updated}
		}
	}
	for scope, entry := range scopes {
		if f := w.Files[w.ActiveFile]; f != nil && f.Host+"\x00"+f.Workspace == scope {
			entry.Active = f.Path
		}
		if len(entry.Tabs) > 20 {
			entry.Tabs = entry.Tabs[len(entry.Tabs)-20:]
		}
		if !reflect.DeepEqual(entry, w.SavedFiles[scope]) {
			entry.Updated = time.Now().UnixMilli()
			w.SavedFiles[scope] = entry
		}
	}
	if len(w.SavedFiles) > 30 {
		keys := make([]string, 0, len(w.SavedFiles))
		for key := range w.SavedFiles {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return w.SavedFiles[keys[i]].Updated < w.SavedFiles[keys[j]].Updated })
		for _, key := range keys[:len(keys)-30] {
			delete(w.SavedFiles, key)
		}
	}
	data, _ := json.Marshal(w.SavedFiles)
	return data
}
func (a *preview) loadFileState() {
	if data, err := os.ReadFile(filepath.Join(a.Live.Dir, "workspace-files.json")); err == nil {
		var state map[string]fileWorkspace
		if json.Unmarshal(data, &state) == nil && state != nil {
			a.Live.Workbench.SavedFiles = state
		}
	}
}
func (a *preview) saveFileState() {
	l := a.Live
	data := a.fileStateSnapshot()
	if string(data) == l.LastFileState || l.Loading["file-state-save"] {
		return
	}
	version := l.Store.reserve("workspace-files.json")
	a.run("file-state-save", func(context.Context, *backendClient) (any, error) {
		return nil, l.Store.save("workspace-files.json", version, data)
	}, func(any) { l.LastFileState = string(data) })
}
