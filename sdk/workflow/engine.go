package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// errStepRejected 标记确认门被拒绝。
var errStepRejected = errors.New("rejected by operator")

// GateHandler 处理 confirm 步骤的人工审批；nil 视为自动通过（便于嵌入与测试）。
type GateHandler interface {
	// WaitApproval 阻塞直到审批到达（true=放行）或 ctx 取消。
	// 返回的 error（含 ctx.Err()）会中止该步骤并按取消处理。
	WaitApproval(ctx context.Context, runID, stepID string) (bool, error)
}

// Engine 执行工作流。字段可在构造后覆盖（测试注入 fake factory / 缩短退避）。
type Engine struct {
	Factory        RunnerFactory
	Dir            string        // 运行数据根目录（通常 DataDir/workflows）
	MaxConcurrency int           // 全局并发会话上限，0 = 4
	RetryBackoff   time.Duration // 重试退避基数，0 = 2s（第 n 次重试等 n*backoff）
	Gates          GateHandler
}

// Run 执行一次工作流并阻塞到结束，返回最终状态。
// journal 与 meta.json 全程落盘到 Dir/<runID>/，是查询的事实来源。
// 返回 err 仅表示取消/超时或基础设施故障；步骤失败体现在返回的 status 与 meta 中。
func (e *Engine) Run(ctx context.Context, spec *Spec, vars map[string]any, runID string) (string, error) {
	if e.Factory == nil {
		return "", fmt.Errorf("workflow: RunnerFactory is required")
	}
	maxC := e.MaxConcurrency
	if maxC <= 0 {
		maxC = 4
	}
	if spec.MaxConcurrency > 0 {
		maxC = spec.MaxConcurrency
	}
	backoff := e.RetryBackoff
	if backoff <= 0 {
		backoff = 2 * time.Second
	}

	// 变量合并：spec.vars 为默认值，调用方 vars 覆盖同名项。
	merged := make(map[string]any, len(spec.Vars)+len(vars))
	for k, v := range spec.Vars {
		merged[k] = v
	}
	for k, v := range vars {
		merged[k] = v
	}

	stepIDs := make([]string, len(spec.Steps))
	for i := range spec.Steps {
		stepIDs[i] = spec.Steps[i].ID
	}
	j, err := newJournal(e.Dir, runID, spec.Name, merged, stepIDs)
	if err != nil {
		return "", err
	}
	defer j.close()
	cache, err := newStepCache(e.Dir+"/cache", spec.Name)
	if err != nil {
		return "", err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := &run{
		eng: e, spec: spec, vars: merged, id: runID,
		j: j, cache: cache,
		sem:     make(chan struct{}, maxC),
		cancel:  cancel,
		ctx:     runCtx,
		backoff: backoff,
		status:  make(map[string]string, len(spec.Steps)),
		done:    make(map[string]chan struct{}, len(spec.Steps)),
		outputs: make(map[string]StepOutput),
	}
	for _, id := range stepIDs {
		r.status[id] = "pending"
		r.done[id] = make(chan struct{})
	}

	status, runErr := r.execute()

	final := time.Now()
	errMsg := ""
	if runErr != nil {
		errMsg = runErr.Error()
	}
	_ = j.updateMeta(func(m *RunMeta) {
		m.Status = status
		m.Error = errMsg
		m.FinishedAt = &final
		m.Outputs = r.snapshotOutputs()
	})
	j.emit(Event{Ts: final, Event: EventRunEnd, Status: status, Error: errMsg})
	return status, runErr
}

// ─── 运行时状态 ──────────────────────────────────────────────────────────────

type run struct {
	eng     *Engine
	spec    *Spec
	vars    map[string]any
	id      string
	j       *journal
	cache   *stepCache
	sem     chan struct{} // 全局并发会话上限
	cancel  context.CancelFunc
	ctx     context.Context
	backoff time.Duration

	mu       sync.Mutex
	status   map[string]string // stepID → pending/running/completed/failed/skipped/waiting_approval
	outputs  map[string]StepOutput
	done     map[string]chan struct{} // stepID → 完成信号，依赖方等待
	failed   int
	rejected int
	firstErr string
}

func (r *run) snapshotOutputs() map[string]StepOutput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return stepOutputSnapshot(r.outputs)
}

func (r *run) setStep(id, status string) {
	r.mu.Lock()
	r.status[id] = status
	r.mu.Unlock()
}

func (r *run) getStep(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status[id]
}

// noteFailure 记录一次步骤终态失败。
func (r *run) noteFailure(id, status, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if status == StatusRejected {
		r.rejected++
	} else {
		r.failed++
	}
	if r.firstErr == "" {
		r.firstErr = msg
	}
	r.status[id] = status
}

// execute 为每个步骤启动 goroutine（依赖等待在步骤内部），全部结束后汇总状态。
func (r *run) execute() (string, error) {
	deps := r.spec.stepDeps()
	var wg sync.WaitGroup
	for i := range r.spec.Steps {
		st := r.spec.Steps[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runStep(st, deps[st.ID])
		}()
	}
	wg.Wait()

	if r.ctx.Err() != nil {
		return StatusCancelled, r.ctx.Err()
	}
	r.mu.Lock()
	failed, rejected, firstErr := r.failed, r.rejected, r.firstErr
	r.mu.Unlock()
	switch {
	case rejected > 0:
		return StatusRejected, fmt.Errorf("workflow rejected: %s", firstErr)
	case failed > 0:
		return StatusFailed, fmt.Errorf("workflow failed: %s", firstErr)
	default:
		return StatusCompleted, nil
	}
}

// runStep 等依赖 →（fan-out | 单步）执行 → 收尾。终态后 close done 通道放行下游。
func (r *run) runStep(st Step, deps []string) {
	for _, d := range deps {
		select {
		case <-r.done[d]:
		case <-r.ctx.Done():
			r.skipStep(st.ID, r.ctx.Err().Error())
			return
		}
	}
	for _, d := range deps {
		if s := r.getStep(d); s != StatusCompleted {
			r.skipStep(st.ID, fmt.Sprintf("upstream %s is %s", d, s))
			return
		}
	}

	if st.hasForeach() {
		r.runForeach(st)
		return
	}

	r.setStep(st.ID, StatusRunning)
	_ = r.j.updateMeta(func(m *RunMeta) { m.Steps[st.ID] = StepMeta{Status: StatusRunning} })

	prompt, err := r.render(st.Prompt, nil, 0)
	if err != nil {
		r.failStep(st.ID, StatusFailed, err.Error())
		return
	}

	if st.Confirm {
		approved, err := r.awaitGate(st)
		if err != nil {
			r.skipStep(st.ID, err.Error())
			return
		}
		if !approved {
			r.failStep(st.ID, StatusRejected, errStepRejected.Error())
			return
		}
	}

	sig := signature(r.spec.Name, st.ID, "", prompt, st.Model)
	if hit, ok := r.cache.get(sig); ok {
		r.completeStep(st.ID, StepOutput{Output: hit.Output, Outputs: hit.Outputs, Cached: true}, sig, prompt, true)
		return
	}

	output, err := r.promptWithRetry(st, prompt)
	if err != nil {
		if r.ctx.Err() != nil {
			r.skipStep(st.ID, r.ctx.Err().Error())
			return
		}
		r.failStep(st.ID, StatusFailed, err.Error())
		return
	}
	r.completeStep(st.ID, StepOutput{Output: output}, sig, prompt, false)
}

// runForeach 解析列表并并发执行各项；任一项最终失败即取消其余项（fail-fast）。
func (r *run) runForeach(st Step) {
	r.setStep(st.ID, StatusRunning)

	items, err := resolveForeach(st.Foreach, r.vars, r.snapshotOutputs())
	if err != nil {
		r.failStep(st.ID, StatusFailed, err.Error())
		return
	}
	if len(items) == 0 {
		r.completeStep(st.ID, StepOutput{}, "", "", false)
		return
	}

	_ = r.j.updateMeta(func(m *RunMeta) {
		m.Steps[st.ID] = StepMeta{Status: StatusRunning, ItemsTotal: len(items)}
	})

	stepCtx, stepCancel := context.WithCancel(r.ctx)
	defer stepCancel()

	conc := st.Concurrency
	if conc <= 0 {
		conc = r.eng.MaxConcurrency
	}
	if conc <= 0 {
		conc = 4
	}

	results := make([]string, len(items))
	var (
		wg        sync.WaitGroup
		itemMu    sync.Mutex
		itemsDone int
		itemErr   error
	)
	tickets := make(chan struct{}, conc)

	for i, item := range items {
		wg.Add(1)
		go func(i int, item any) {
			defer wg.Done()
			select {
			case tickets <- struct{}{}:
				defer func() { <-tickets }()
			case <-stepCtx.Done():
				return
			}
			if err := r.runItem(st, stepCtx, i, item, results); err != nil {
				itemMu.Lock()
				if itemErr == nil {
					itemErr = err
					stepCancel() // 阻止尚未开始的项
				}
				itemMu.Unlock()
				return
			}
			itemMu.Lock()
			itemsDone++
			done := itemsDone
			itemMu.Unlock()
			_ = r.j.updateMeta(func(m *RunMeta) {
				sm := m.Steps[st.ID]
				sm.ItemsDone = done
				m.Steps[st.ID] = sm
			})
		}(i, item)
	}
	wg.Wait()

	if itemErr != nil {
		if r.ctx.Err() != nil {
			r.skipStep(st.ID, r.ctx.Err().Error())
			return
		}
		r.failStep(st.ID, StatusFailed, itemErr.Error())
		return
	}
	if r.ctx.Err() != nil {
		r.skipStep(st.ID, r.ctx.Err().Error())
		return
	}

	output := StepOutput{Output: strings.Join(results, "\n\n"), Outputs: append([]string(nil), results...)}
	r.mu.Lock()
	r.outputs[st.ID] = output
	r.mu.Unlock()
	r.setStep(st.ID, StatusCompleted)
	_ = r.j.updateMeta(func(m *RunMeta) {
		sm := m.Steps[st.ID]
		sm.Status = StatusCompleted
		sm.ItemsDone = len(items)
		m.Steps[st.ID] = sm
		m.Outputs = r.snapshotOutputs()
	})
	r.j.emit(Event{Ts: time.Now(), Event: EventStepEnd, Step: st.ID, Output: output.Output})
	close(r.done[st.ID])
}

// runItem 执行单个 fan-out 项：渲染 → 缓存 → 重试执行，结果写入 results[i]。
// 各 goroutine 只写自己的下标，无需加锁。
func (r *run) runItem(st Step, stepCtx context.Context, i int, item any, results []string) error {
	prompt, err := r.render(st.Prompt, item, i)
	if err != nil {
		return fmt.Errorf("item %d: %w", i, err)
	}
	itemKey := fmt.Sprintf("%d|%v", i, item)
	sig := signature(r.spec.Name, st.ID, itemKey, prompt, st.Model)
	if hit, ok := r.cache.get(sig); ok {
		results[i] = hit.Output
		r.j.emit(Event{Ts: time.Now(), Event: EventItemEnd, Step: st.ID, Item: &i, Cached: true, Output: hit.Output})
		return nil
	}

	output, err := r.executeAttempt(st, stepCtx, prompt)
	for attempt := 1; err != nil && attempt <= st.Retries && stepCtx.Err() == nil; attempt++ {
		r.j.emit(Event{Ts: time.Now(), Event: EventStepError, Step: st.ID, Item: &i, Attempt: attempt, Error: err.Error()})
		select {
		case <-time.After(time.Duration(attempt) * r.backoff):
		case <-stepCtx.Done():
			return fmt.Errorf("item %d: %s: %w", i, stepCtx.Err(), err)
		}
		output, err = r.executeAttempt(st, stepCtx, prompt)
	}
	if err != nil {
		return fmt.Errorf("item %d: %w", i, err)
	}

	results[i] = output
	r.cache.put(sig, cacheEntry{Output: output, RunID: r.id, Step: st.ID, CreatedAt: time.Now()})
	r.j.emit(Event{Ts: time.Now(), Event: EventItemEnd, Step: st.ID, Item: &i, Output: output})
	return nil
}

// awaitGate 请求人工审批；审批到达前 run/step 标记 waiting_approval。
func (r *run) awaitGate(st Step) (bool, error) {
	ready := func() {
		r.setStep(st.ID, StatusWaiting)
		_ = r.j.updateMeta(func(m *RunMeta) {
			sm := m.Steps[st.ID]
			sm.Status = StatusWaiting
			m.Steps[st.ID] = sm
			m.Status = StatusWaiting
		})
		r.j.emit(Event{Ts: time.Now(), Event: EventGateWait, Step: st.ID})
	}
	approved := true
	var err error
	if gate, ok := r.eng.Gates.(interface {
		WaitApprovalReady(context.Context, string, string, func()) (bool, error)
	}); ok {
		approved, err = gate.WaitApprovalReady(r.ctx, r.id, st.ID, ready)
	} else {
		ready()
		if r.eng.Gates != nil {
			approved, err = r.eng.Gates.WaitApproval(r.ctx, r.id, st.ID)
		}
	}
	if err != nil {
		return false, err
	}
	r.j.emit(Event{Ts: time.Now(), Event: EventGateResult, Step: st.ID,
		Status: map[bool]string{true: "approved", false: "rejected"}[approved]})
	if approved {
		r.setStep(st.ID, StatusRunning)
		_ = r.j.updateMeta(func(m *RunMeta) {
			sm := m.Steps[st.ID]
			sm.Status = StatusRunning
			m.Steps[st.ID] = sm
			m.Status = StatusRunning
		})
	}
	return approved, nil
}

// promptWithRetry 普通步骤的重试执行；缓存写入统一在 completeStep。
func (r *run) promptWithRetry(st Step, prompt string) (string, error) {
	output, err := r.executeAttempt(st, r.ctx, prompt)
	for attempt := 1; err != nil && attempt <= st.Retries && r.ctx.Err() == nil; attempt++ {
		r.j.emit(Event{Ts: time.Now(), Event: EventStepError, Step: st.ID, Attempt: attempt, Error: err.Error()})
		select {
		case <-time.After(time.Duration(attempt) * r.backoff):
		case <-r.ctx.Done():
			return "", fmt.Errorf("%s: %w", r.ctx.Err(), err)
		}
		output, err = r.executeAttempt(st, r.ctx, prompt)
	}
	return output, err
}

// executeAttempt 一次会话执行：占全局并发票 → 建会话 → Prompt → 收尾。
func (r *run) executeAttempt(st Step, ctx context.Context, prompt string) (string, error) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	runner, err := r.eng.Factory.NewRunner(ctx, RunnerOptions{Model: st.Model, Label: r.spec.Name + "/" + st.ID})
	if err != nil {
		return "", fmt.Errorf("create runner: %w", err)
	}
	output, promptErr := runner.Prompt(ctx, prompt)
	if closeErr := runner.Close(); closeErr != nil && promptErr == nil {
		promptErr = fmt.Errorf("close runner: %w", closeErr)
	}
	if promptErr != nil {
		return "", promptErr
	}
	return output, nil
}

func (r *run) render(prompt string, item any, idx int) (string, error) {
	return renderPrompt(prompt, r.vars, r.snapshotOutputs(), item, idx)
}

func (r *run) completeStep(id string, out StepOutput, sig, prompt string, cached bool) {
	if !cached && sig != "" {
		r.cache.put(sig, cacheEntry{Output: out.Output, Outputs: out.Outputs, RunID: r.id, Step: id, CreatedAt: time.Now()})
	}
	r.mu.Lock()
	r.outputs[id] = out
	r.mu.Unlock()
	r.setStep(id, StatusCompleted)
	_ = r.j.updateMeta(func(m *RunMeta) {
		sm := m.Steps[id]
		sm.Status = StatusCompleted
		sm.Cached = cached
		m.Steps[id] = sm
		m.Outputs = r.snapshotOutputs()
	})
	r.j.emit(Event{Ts: time.Now(), Event: EventStepEnd, Step: id, Prompt: prompt, Output: out.Output, Cached: cached})
	close(r.done[id])
}

func (r *run) failStep(id, status, msg string) {
	r.noteFailure(id, status, msg)
	_ = r.j.updateMeta(func(m *RunMeta) {
		sm := m.Steps[id]
		sm.Status = status
		sm.Error = msg
		m.Steps[id] = sm
	})
	r.j.emit(Event{Ts: time.Now(), Event: EventStepEnd, Step: id, Status: status, Error: msg})
	close(r.done[id])
}

func (r *run) skipStep(id, reason string) {
	r.setStep(id, StatusSkipped)
	_ = r.j.updateMeta(func(m *RunMeta) {
		sm := m.Steps[id]
		sm.Status = StatusSkipped
		sm.Error = reason
		m.Steps[id] = sm
	})
	r.j.emit(Event{Ts: time.Now(), Event: EventStepEnd, Step: id, Status: StatusSkipped, Error: reason})
	close(r.done[id])
}
