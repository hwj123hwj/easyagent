package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hwj123hwj/easyagent/sdk/agent"
)

const snapshotFileLimit = 4 * 1024 * 1024
const snapshotTotalLimit = 32 * 1024 * 1024

type fileState struct {
	Hash string      `json:"hash"`
	Mode fs.FileMode `json:"mode"`
	Size int64       `json:"size"`
}
type runFiles struct {
	mu        sync.Mutex
	RunID     string               `json:"run_id"`
	SessionID string               `json:"session_id"`
	Workspace string               `json:"workspace"`
	StartedAt time.Time            `json:"started_at"`
	Before    map[string]fileState `json:"before"`
	After     map[string]fileState `json:"after"`
	Owned     map[string]bool      `json:"owned"`
	Skipped   []string             `json:"skipped"`
	Complete  bool                 `json:"complete"`
	Undone    map[string]bool      `json:"undone,omitempty"`
	calls     map[string][]string
	toolAfter map[string]fileState
}
type runFileChange struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Attribution string `json:"attribution"`
	Size        int64  `json:"size"`
	CanUndo     bool   `json:"can_undo"`
	Conflict    string `json:"conflict,omitempty"`
	Before      string `json:"before,omitempty"`
	After       string `json:"after,omitempty"`
	Binary      bool   `json:"binary"`
	Undone      bool   `json:"undone"`
	Version     string `json:"version"`
}

func (s *Server) filesPath(runID string) string {
	hash := sha256.Sum256([]byte(runID))
	return filepath.Join(s.app.Config().DataDir, "run-files", hex.EncodeToString(hash[:])+".json")
}
func (s *Server) blobPath(hash string) string {
	return filepath.Join(s.app.Config().DataDir, "run-files", "blobs", hash)
}
func (s *Server) saveFiles(f *runFiles) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	path := s.filesPath(f.RunID)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
func (s *Server) snapshotFiles(root string) (map[string]fileState, []string) {
	files := map[string]fileState{}
	skipped := []string{}
	names := []string{}
	canonicalRoot, _ := filepath.EvalSymlinks(root)
	dataDir, _ := filepath.Abs(s.app.Config().DataDir)
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = resolved
	}
	isData := func(path string) bool {
		rel, err := filepath.Rel(dataDir, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", ".")
	cmd.Dir = root
	if output, err := cmd.Output(); err == nil {
		for _, name := range bytes.Split(output, []byte{0}) {
			if len(name) > 0 {
				names = append(names, string(name))
			}
		}
	} else {
		visited := 0
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			visited++
			if visited > 5000 || ctx.Err() != nil {
				skipped = append(skipped, "扫描达到上限")
				return fs.SkipAll
			}
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if rel == "." {
				return nil
			}
			if isData(filepath.Join(canonicalRoot, rel)) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", ".easyagent", "node_modules", "vendor", "data", "dist", "build", ".cache":
					return fs.SkipDir
				}
				return nil
			}
			names = append(names, rel)
			return nil
		})
	}
	sort.Strings(names)
	total := int64(0)
	for _, rel := range names {
		if strings.HasPrefix(rel, ".easyagent/") || isData(filepath.Join(canonicalRoot, rel)) {
			continue
		}
		if len(files) >= 2000 || total >= snapshotTotalLimit {
			skipped = append(skipped, "快照达到文件或总大小上限")
			break
		}
		path, err := securePath(root, rel)
		if err != nil {
			skipped = append(skipped, rel)
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > snapshotFileLimit {
			skipped = append(skipped, rel)
			files[rel] = fileState{Hash: fmt.Sprintf("large:%d:%d", info.Size(), info.ModTime().UnixNano()), Mode: info.Mode().Perm(), Size: info.Size()}
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			skipped = append(skipped, rel)
			continue
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		blob := s.blobPath(hash)
		if err = os.MkdirAll(filepath.Dir(blob), 0700); err != nil {
			skipped = append(skipped, rel)
			continue
		}
		if _, err = os.Stat(blob); errors.Is(err, os.ErrNotExist) {
			if err = os.WriteFile(blob, data, 0600); err != nil {
				skipped = append(skipped, rel)
				continue
			}
		}
		files[rel] = fileState{Hash: hash, Mode: info.Mode().Perm(), Size: info.Size()}
		total += int64(len(data))
	}
	return files, skipped
}
func (s *Server) beginFiles(run *sessionRun, root string) {
	before, skipped := s.snapshotFiles(root)
	run.files = &runFiles{RunID: run.ID, SessionID: run.SessionID, Workspace: root, StartedAt: run.StartedAt, Before: before, Owned: map[string]bool{}, Skipped: skipped, calls: map[string][]string{}, toolAfter: map[string]fileState{}, Undone: map[string]bool{}}
	if err := s.saveFiles(run.files); err != nil {
		run.files = nil
	}
}
func (s *Server) observeFiles(run *sessionRun, event agent.AgentStreamEvent) {
	f := run.files
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.Type == agent.StreamEventToolStart {
		switch event.ToolName {
		case "write", "edit", "multiedit":
		default:
			return
		}
		data, _ := json.Marshal(event.ToolArgs)
		if raw, ok := event.ToolArgs.(string); ok {
			data = []byte(raw)
		}
		var args map[string]any
		_ = json.Unmarshal(data, &args)
		var paths []string
		for _, key := range []string{"path", "file_path"} {
			if p, ok := args[key].(string); ok {
				paths = append(paths, p)
			}
		}
		if edits, ok := args["edits"].([]any); ok {
			for _, entry := range edits {
				if edit, ok := entry.(map[string]any); ok {
					if p, ok := edit["file_path"].(string); ok {
						paths = append(paths, p)
					}
				}
			}
		}
		for _, path := range paths {
			if safe, err := securePath(f.Workspace, path); err == nil {
				if info, err := os.Lstat(safe); err == nil && !info.Mode().IsRegular() {
					continue
				}
				canonicalRoot, _ := filepath.EvalSymlinks(f.Workspace)
				rel, _ := filepath.Rel(canonicalRoot, safe)
				expected, existed := f.Before[rel]
				if previous, ok := f.toolAfter[rel]; ok {
					expected = previous
					existed = true
				}
				current, present := readFileState(safe)
				if present != existed || current != expected {
					continue
				}
				f.calls[event.ToolCallID] = append(f.calls[event.ToolCallID], rel)
			}
		}
	}
	if event.Type == agent.StreamEventToolEnd && !event.IsError {
		for _, path := range f.calls[event.ToolCallID] {
			f.Owned[path] = true
			f.toolAfter[path], _ = readFileState(filepath.Join(f.Workspace, path))
		}
	}
}
func (s *Server) finishFiles(run *sessionRun) {
	if run.files != nil {
		run.files.mu.Lock()
		defer run.files.mu.Unlock()
		after, skipped := s.snapshotFiles(run.files.Workspace)
		run.files.After = after
		run.files.Skipped = append(run.files.Skipped, skipped...)
		for path, expected := range run.files.toolAfter {
			if run.files.After[path] != expected {
				delete(run.files.Owned, path)
			}
		}
		run.files.Complete = true
		_ = s.saveFiles(run.files)
	}
}
func (s *Server) loadFiles(sessionID, runID string) (*runFiles, error) {
	if sessionID == "" || sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, `/\`) || !s.app.SessionManager().Exists(sessionID) {
		return nil, fmt.Errorf("session not found")
	}
	data, err := os.ReadFile(s.filesPath(runID))
	if err != nil {
		return nil, err
	}
	var f runFiles
	if err = json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.RunID != runID || f.SessionID != sessionID {
		return nil, fmt.Errorf("run not found")
	}
	if f.Undone == nil {
		f.Undone = map[string]bool{}
	}
	return &f, nil
}
func (s *Server) changes(f *runFiles) []runFileChange {
	keys := map[string]bool{}
	for p := range f.Before {
		keys[p] = true
	}
	for p := range f.After {
		keys[p] = true
	}
	out := []runFileChange{}
	for path := range keys {
		before, b := f.Before[path]
		after, a := f.After[path]
		if before == after && b == a {
			continue
		}
		kind := "modified"
		if !b {
			kind = "created"
		}
		if !a {
			kind = "deleted"
		}
		versionData, _ := json.Marshal([]fileState{before, after})
		version := sha256.Sum256(versionData)
		c := runFileChange{Binary: strings.HasPrefix(before.Hash, "large:") || strings.HasPrefix(after.Hash, "large:"), Version: hex.EncodeToString(version[:]), Path: path, Kind: kind, Size: after.Size, Attribution: "observed", Undone: f.Undone[path]}
		if f.Owned[path] {
			c.Attribution = "file_tool"
		}
		currentPath, err := securePath(f.Workspace, path)
		if err != nil {
			c.Conflict = "路径不再位于工作区"
		} else if info, err := os.Lstat(currentPath); err == nil && !info.Mode().IsRegular() {
			c.Conflict = "文件类型已经改变"
		} else if current, present := readFileState(currentPath); present {
			if !a || current != after {
				c.Conflict = "任务结束后文件已被修改"
			}
		} else if _, statErr := os.Lstat(currentPath); !errors.Is(statErr, os.ErrNotExist) || a {
			c.Conflict = "文件不存在或无法读取"
		}
		unbounded := false
		for _, skip := range f.Skipped {
			if skip == path || strings.Contains(skip, "上限") {
				unbounded = true
			}
		}
		c.CanUndo = !unbounded && f.Complete && f.Owned[path] && c.Conflict == "" && !c.Undone
		if b && !strings.HasPrefix(before.Hash, "large:") {
			data, _ := os.ReadFile(s.blobPath(before.Hash))
			if utf8.Valid(data) && !bytes.ContainsRune(data, 0) {
				c.Before = string(data)
			} else {
				c.Binary = true
			}
		}
		if a && !strings.HasPrefix(after.Hash, "large:") {
			data, _ := os.ReadFile(s.blobPath(after.Hash))
			if utf8.Valid(data) && !bytes.ContainsRune(data, 0) {
				c.After = string(data)
			} else {
				c.Binary = true
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func (s *Server) getRunFiles(w http.ResponseWriter, r *http.Request) {
	f, err := s.loadFiles(r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		writeError(w, 404, "run snapshot not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"run_id": f.RunID, "workspace": f.Workspace, "complete": f.Complete, "skipped": f.Skipped, "files": s.changes(f)})
}
func (s *Server) undoRunFile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path      string `json:"path"`
		AfterHash string `json:"after_hash"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request) != nil {
		writeError(w, 400, "invalid request")
		return
	}
	// Serialize review restores and admission; active runs are never overwritten.
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	for _, state := range s.runs.sessions {
		if state.run != nil && runActive(state.run) {
			writeError(w, 409, "请等待运行中的任务结束后撤回")
			return
		}
	}
	f, err := s.loadFiles(r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		writeError(w, 404, "run snapshot not found")
		return
	}
	var selected *runFileChange
	for _, c := range s.changes(f) {
		if c.Path == request.Path {
			copy := c
			selected = &copy
			break
		}
	}
	if selected == nil || !selected.CanUndo || request.AfterHash != selected.Version {
		writeError(w, 409, "不能安全撤回：归属不明确、文件已修改或已撤回")
		return
	}
	path, err := securePath(f.Workspace, request.Path)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	before, existed := f.Before[request.Path]
	if existed {
		data, readErr := os.ReadFile(s.blobPath(before.Hash))
		sum := sha256.Sum256(data)
		if readErr != nil || hex.EncodeToString(sum[:]) != before.Hash {
			writeError(w, 500, "缺少原始快照")
			return
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err == nil {
			var tmp *os.File
			tmp, err = os.CreateTemp(filepath.Dir(path), ".easyagent-restore-*")
			if err == nil {
				defer os.Remove(tmp.Name())
				_, err = tmp.Write(data)
				if err == nil {
					err = tmp.Chmod(before.Mode)
				}
				closeErr := tmp.Close()
				if err == nil {
					err = closeErr
				}
				if err == nil {
					err = os.Rename(tmp.Name(), path)
				}
			}
		}
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		writeError(w, 500, "撤回失败："+err.Error())
		return
	}
	f.Undone[request.Path] = true
	if err = s.saveFiles(f); err != nil {
		writeError(w, 500, "文件已恢复，但撤回记录保存失败")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// Downloads remain authenticated and scoped to the session project.
func (s *Server) downloadWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	path, err := securePath(root, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "file not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32*1024*1024 {
		writeError(w, 413, "文件不可预览或超过 32 MiB")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", urlPathName(filepath.Base(path))))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, file)
}
func urlPathName(name string) string { return url.PathEscape(name) }
func fileMode(path string) fs.FileMode {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	return info.Mode().Perm()
}

func (s *Server) listRunFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.app.SessionManager().Exists(id) {
		writeError(w, 404, "session not found")
		return
	}
	entries, _ := os.ReadDir(filepath.Join(s.app.Config().DataDir, "run-files"))
	out := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.app.Config().DataDir, "run-files", entry.Name()))
		if err != nil {
			continue
		}
		var f runFiles
		if json.Unmarshal(data, &f) != nil || f.SessionID != id {
			continue
		}
		files := []runFileChange{}
		for _, change := range summarizeFiles(&f) {
			change.Before = ""
			change.After = ""
			files = append(files, change)
		}
		out = append(out, map[string]any{"run_id": f.RunID, "started_at": f.StartedAt, "workspace": f.Workspace, "complete": f.Complete, "skipped": f.Skipped, "files": files})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["started_at"].(time.Time).Before(out[j]["started_at"].(time.Time)) })
	if len(out) > 50 {
		out = out[len(out)-50:]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
func (s *Server) workspaceFileData(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	path, err := securePath(root, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, 404, "regular file not found")
		return
	}
	if info.Size() > 32*1024*1024 {
		writeError(w, 413, "文件超过 32 MiB，请在主机上打开")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, 404, "file not found")
		return
	}
	mime := http.DetectContentType(data)
	if strings.HasPrefix(string(data), "%PDF-") {
		mime = "application/pdf"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"data": base64.StdEncoding.EncodeToString(data), "mimeType": mime, "name": filepath.Base(path)})
}

func readFileState(path string) (fileState, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > snapshotFileLimit {
		return fileState{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileState{}, false
	}
	sum := sha256.Sum256(data)
	return fileState{Hash: hex.EncodeToString(sum[:]), Mode: info.Mode().Perm(), Size: info.Size()}, true
}

func summarizeFiles(f *runFiles) []runFileChange {
	keys := map[string]bool{}
	for path := range f.Before {
		keys[path] = true
	}
	for path := range f.After {
		keys[path] = true
	}
	out := []runFileChange{}
	for path := range keys {
		b, bok := f.Before[path]
		a, aok := f.After[path]
		if b == a && bok == aok {
			continue
		}
		kind := "modified"
		if !bok {
			kind = "created"
		}
		if !aok {
			kind = "deleted"
		}
		attribution := "observed"
		if f.Owned[path] {
			attribution = "file_tool"
		}
		out = append(out, runFileChange{Path: path, Kind: kind, Size: a.Size, Attribution: attribution, Undone: f.Undone[path]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
