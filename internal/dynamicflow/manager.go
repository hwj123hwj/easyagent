// Package dynamicflow hosts the ZCode workflow engine through a bounded stdio adapter.
package dynamicflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

type Persona struct {
	Name   string `json:"name"`
	System string `json:"system"`
}
type Host interface {
	Prepare(context.Context, string) (json.RawMessage, error)
	Actor(context.Context, string, json.RawMessage, Persona, string) (string, error)
	Ask(context.Context, string, string, bool, json.RawMessage) (string, error)
}
type Run struct {
	ActorConfig json.RawMessage   `json:"actor_config,omitempty"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Script      string            `json:"script"`
	Parent      string            `json:"parent_session_id,omitempty"`
	Status      string            `json:"status"`
	Error       string            `json:"error,omitempty"`
	Started     int64             `json:"started_at"`
	Updated     int64             `json:"updated_at"`
	Actors      map[string]string `json:"sessions"`
	State       json.RawMessage   `json:"state,omitempty"`
	Result      json.RawMessage   `json:"result,omitempty"`
}
type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Manager struct {
	mu           sync.Mutex
	dir, runtime string
	host         Host
	active       map[string]*activeRun
	runs         map[string]*Run
	closed       bool
}

var validID = regexp.MustCompile(`^dw-[a-f0-9]{24}$`)

func New(dir, runtimePath string, host Host) (*Manager, error) {
	var pathErr error
	dir, pathErr = filepath.Abs(dir)
	if pathErr != nil {
		return nil, pathErr
	}
	runtimePath, pathErr = filepath.Abs(runtimePath)
	if pathErr != nil {
		return nil, pathErr
	}
	m := &Manager{dir: dir, runtime: runtimePath, host: host, active: map[string]*activeRun{}, runs: map[string]*Run{}}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !validID.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "run.json"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			} // creation may stop before the first metadata commit
			return nil, err
		}
		var run Run
		if err = json.Unmarshal(data, &run); err != nil {
			slog.Warn("ignoring unreadable workflow metadata", "run_id", e.Name(), "error", err)
			continue
		}
		if run.ID != e.Name() {
			slog.Warn("ignoring workflow identity mismatch", "run_id", e.Name())
			continue
		}
		if run.Status == "running" {
			run.Status = "interrupted"
			run.Error = "服务中断，检查已执行操作后可恢复"
			if err = m.save(&run); err != nil {
				return nil, err
			}
		}
		m.runs[run.ID] = &run
	}
	return m, nil
}
func RuntimePath() string {
	if path := os.Getenv("EA_WORKFLOW_RUNTIME"); path != "" {
		return path
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "workflow-runtime.mjs")
}
func (m *Manager) Available() error {
	if _, err := exec.LookPath("node"); err != nil {
		return errors.New("动态工作流需要 Node.js 22+，请安装后重启服务")
	}
	if _, err := os.Stat(m.runtime); err != nil {
		return errors.New("动态工作流运行组件未安装，请运行 npm ci && npm run build 于 workflow-runtime 并配置 EA_WORKFLOW_RUNTIME")
	}
	return nil
}
func (m *Manager) Start(ctx context.Context, name, script, parent string) (*Run, error) {
	if err := m.Available(); err != nil {
		return nil, err
	}
	if len(script) == 0 || len(script) > 64<<10 || len(name) > 200 {
		return nil, errors.New("需要名称和不超过 64 KiB 的脚本")
	}
	policy, err := m.host.Prepare(ctx, parent)
	if err != nil {
		return nil, err
	}
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	run := &Run{ActorConfig: policy, ID: "dw-" + hex.EncodeToString(idBytes), Name: name, Script: script, Parent: parent, Status: "running", Started: time.Now().UnixMilli(), Actors: map[string]string{}}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("workflow service closed")
	}
	if len(m.active) >= 4 {
		return nil, errors.New("最多同时运行 4 个动态工作流")
	}
	if err := os.Mkdir(filepath.Join(m.dir, run.ID), 0700); err != nil {
		return nil, err
	}
	if err := m.save(run); err != nil {
		return nil, err
	}
	m.runs[run.ID] = run
	m.launch(ctx, run)
	return clone(run), nil
}
func (m *Manager) Resume(ctx context.Context, id string) (*Run, error) {
	if err := m.Available(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	if m.closed || m.active[id] != nil || (run.Status != "cancelled" && run.Status != "interrupted" && run.Status != "stopped") || len(m.active) >= 4 {
		return nil, errors.New("该工作流当前不能恢复")
	}
	previous := clone(run)
	run.Status = "running"
	run.Error = ""
	run.Result = nil
	if err := m.save(run); err != nil {
		*run = *previous
		return nil, err
	}
	m.launch(ctx, run)
	return clone(run), nil
}
func (m *Manager) launch(ctx context.Context, run *Run) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	active := &activeRun{cancel: cancel, done: make(chan struct{})}
	m.active[run.ID] = active
	go func() {
		err := m.execute(ctx, run.ID)
		m.mu.Lock()
		if err != nil {
			run.Status = "failed"
			run.Error = err.Error()
			if ctx.Err() != nil {
				run.Status = "cancelled"
			}
		}
		if run.Status == "running" {
			run.Status = "failed"
			run.Error = "运行进程未返回最终状态"
		}
		if saveErr := m.save(run); saveErr != nil {
			run.Error = "保存运行失败: " + saveErr.Error()
			run.Status = "failed"
		}
		delete(m.active, run.ID)
		close(active.done)
		m.mu.Unlock()
		cancel()
	}()
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.active[id]
	if a == nil {
		return errors.New("工作流未在运行")
	}
	a.cancel()
	return nil
}
func (m *Manager) Get(id string) (*Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id]
	if r == nil {
		return nil, os.ErrNotExist
	}
	return clone(r), nil
}
func (m *Manager) List() []*Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	runs := make([]*Run, 0, len(m.runs))
	for _, r := range m.runs {
		c := clone(r)
		c.Script = ""
		c.State = nil
		c.Result = nil
		runs = append(runs, c)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Started > runs[j].Started })
	return runs
}
func (m *Manager) Wait(ctx context.Context, id string) (*Run, error) {
	m.mu.Lock()
	a := m.active[id]
	m.mu.Unlock()
	if a != nil {
		select {
		case <-a.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.Get(id)
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	var pending []*activeRun
	for _, a := range m.active {
		a.cancel()
		pending = append(pending, a)
	}
	m.mu.Unlock()
	for _, a := range pending {
		<-a.done
	}
}
func clone(run *Run) *Run {
	data, _ := json.Marshal(run)
	var out Run
	_ = json.Unmarshal(data, &out)
	return &out
}
func (m *Manager) save(run *Run) error {
	run.Updated = time.Now().UnixMilli()
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir, run.ID, "run.json")
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}

// ActorSession identifies private worker sessions even while the run is stopped.
func (m *Manager) ActorSession(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		for _, sid := range r.Actors {
			if sid == id {
				return true
			}
		}
	}
	return false
}
func (m *Manager) ActiveActorSession(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.active {
		for _, sid := range m.runs[key].Actors {
			if sid == id {
				return true
			}
		}
	}
	return false
}

// ActiveCount includes runs between Actor requests and while finalizing storage.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}
