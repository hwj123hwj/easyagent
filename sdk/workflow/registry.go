package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Registry 管理活跃运行（取消、审批）并提供基于磁盘的查询。
// 磁盘（meta.json + journal.jsonl）是唯一事实来源：GET 类接口不区分活跃与历史运行。
type Registry struct {
	eng *Engine

	mu   sync.Mutex
	runs map[string]*liveRun
}

type liveRun struct {
	cancel context.CancelFunc
	gates  map[string]chan bool // stepID → 审批结果通道
}

var runIDRe = regexp.MustCompile(`^wf-[0-9]+-[0-9a-f]{8}$`)

func NewRegistry(eng *Engine) *Registry {
	return &Registry{eng: eng, runs: make(map[string]*liveRun)}
}

// Start 校验 spec 后异步启动一次运行，立即返回 runID。
// 返回前同步写入初始 meta.json，保证 Start 返回后 GET 即可命中。
// 运行 goroutine 绑定独立 context，不随调用方 ctx 结束。
func (r *Registry) Start(spec *Spec, vars map[string]any) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	id := newRunID()
	ctx, cancel := context.WithCancel(context.Background())
	if err := writeInitialMeta(r.eng.Dir, id, spec.Name); err != nil {
		cancel()
		return "", fmt.Errorf("init run meta: %w", err)
	}

	r.mu.Lock()
	r.runs[id] = &liveRun{cancel: cancel, gates: make(map[string]chan bool)}
	r.mu.Unlock()

	eng := *r.eng
	eng.Gates = r // confirm 步骤经 Registry 审批通道
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.runs, id)
			r.mu.Unlock()
			cancel()
		}()
		_, _ = eng.Run(ctx, spec, vars, id) // 结果已落盘，无需处理
	}()
	return id, nil
}

// Cancel 结束一次运行。运行已结束时，若磁盘状态残留为 running/waiting_approval
// （如进程重启导致的孤儿运行），将其标记为 cancelled。
func (r *Registry) Cancel(id string) error {
	r.mu.Lock()
	live := r.runs[id]
	r.mu.Unlock()
	if live != nil {
		live.cancel()
		return nil
	}
	return r.closeStaleRun(id, StatusCancelled)
}

func (r *Registry) closeStaleRun(id, status string) error {
	j, err := openJournalForUpdate(r.eng.Dir, id)
	if err != nil {
		return err
	}
	defer j.close()
	switch j.meta.Status {
	case StatusRunning, StatusWaiting:
		return j.updateMeta(func(m *RunMeta) {
			m.Status = status
			now := time.Now()
			m.FinishedAt = &now
		})
	default:
		return fmt.Errorf("run %s is already %s", id, j.meta.Status)
	}
}

// Approve / Reject 放行或拒绝一次等待中的确认门。stepID 为空时要求恰好只有一个等待门。
func (r *Registry) Approve(runID, stepID string) error { return r.resolveGate(runID, stepID, true) }
func (r *Registry) Reject(runID, stepID string) error  { return r.resolveGate(runID, stepID, false) }

func (r *Registry) resolveGate(runID, stepID string, approve bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	live := r.runs[runID]
	if live == nil {
		return fmt.Errorf("run %s is not active", runID)
	}
	if stepID == "" {
		if len(live.gates) != 1 {
			ids := make([]string, 0, len(live.gates))
			for id := range live.gates {
				ids = append(ids, id)
			}
			return fmt.Errorf("run %s has %d waiting gates, specify step; waiting: %v", runID, len(live.gates), ids)
		}
		for id := range live.gates {
			stepID = id
		}
	}
	ch, ok := live.gates[stepID]
	if !ok {
		return fmt.Errorf("step %q is not waiting approval in run %s", stepID, runID)
	}
	delete(live.gates, stepID)
	ch <- approve
	return nil
}

// WaitApproval 实现 GateHandler：注册审批通道并阻塞等待。
func (r *Registry) WaitApproval(ctx context.Context, runID, stepID string) (bool, error) {
	return r.WaitApprovalReady(ctx, runID, stepID, nil)
}

// WaitApprovalReady 先注册审批通道，再发布等待状态，防止可见但无法审批。
func (r *Registry) WaitApprovalReady(ctx context.Context, runID, stepID string, ready func()) (bool, error) {
	ch := make(chan bool, 1)
	r.mu.Lock()
	live := r.runs[runID]
	if live == nil {
		r.mu.Unlock()
		return false, fmt.Errorf("run %s is not active", runID)
	}
	live.gates[stepID] = ch
	r.mu.Unlock()
	if ready != nil {
		ready()
	}

	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(live.gates, stepID)
		r.mu.Unlock()
		return false, ctx.Err()
	}
}

// List 返回全部运行（活跃 + 历史），按开始时间倒序。
func (r *Registry) List() ([]RunMeta, error) {
	entries, err := os.ReadDir(r.eng.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RunMeta{}, nil
		}
		return nil, err
	}
	var runs []RunMeta
	for _, entry := range entries {
		if !entry.IsDir() || !runIDRe.MatchString(entry.Name()) {
			continue
		}
		meta, err := loadMeta(r.eng.Dir, entry.Name())
		if err != nil {
			continue // 半成品目录容忍跳过
		}
		runs = append(runs, *meta)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	if runs == nil {
		runs = []RunMeta{}
	}
	return runs, nil
}

// Get 返回一次运行的 meta 与全部 journal 事件。
func (r *Registry) Get(id string) (*RunMeta, []Event, error) {
	if !runIDRe.MatchString(id) {
		return nil, nil, fmt.Errorf("invalid run id %q", id)
	}
	meta, err := loadMeta(r.eng.Dir, id)
	if err != nil {
		return nil, nil, err
	}
	events, err := loadEvents(r.eng.Dir, id)
	if err != nil {
		return meta, nil, err
	}
	return meta, events, nil
}

// ─── 磁盘读取 ────────────────────────────────────────────────────────────────

func runDir(root, id string) string { return filepath.Join(root, id) }

func loadMeta(root, id string) (*RunMeta, error) {
	data, err := os.ReadFile(filepath.Join(runDir(root, id), "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("load run %s: %w", id, err)
	}
	var meta RunMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta of run %s: %w", id, err)
	}
	return &meta, nil
}

func loadEvents(root, id string) ([]Event, error) {
	data, err := os.ReadFile(filepath.Join(runDir(root, id), "journal.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil // 运行刚创建、journal 尚未落盘
		}
		return nil, err
	}
	events := []Event{}
	for _, line := range splitLines(data) {
		var e Event
		if err := json.Unmarshal(line, &e); err == nil {
			events = append(events, e)
		}
	}
	return events, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// openJournalForUpdate 打开一次运行已存在的 meta 供修改（孤儿运行清理用）。
func openJournalForUpdate(root, id string) (*journal, error) {
	if !runIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid run id %q", id)
	}
	meta, err := loadMeta(root, id)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(runDir(root, id), "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &journal{dir: runDir(root, id), file: f, meta: *meta}, nil
}

// writeInitialMeta 落盘一份占位 meta；引擎启动后会用完整 meta 覆盖。
func writeInitialMeta(root, id, name string) error {
	dir := runDir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	meta := RunMeta{
		ID:        id,
		Name:      name,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		Steps:     map[string]StepMeta{},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644)
}

func newRunID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("wf-%d-%s", time.Now().Unix(), hex.EncodeToString(b[:]))
}

// ActiveCount includes runs waiting for human approval.
func (r *Registry) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

// CancelAll requests cancellation; callers can wait for ActiveCount to reach zero.
func (r *Registry) CancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		run.cancel()
	}
}
