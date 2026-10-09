package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/skillmarket"
)

// marketJob is one background install tracked by marketJobRegistry.
type marketJob struct {
	ID         string                   `json:"id"`
	SkillID    int                      `json:"skill_id"`
	State      string                   `json:"state"` // running | succeeded | failed
	SkillName  string                   `json:"skill_name,omitempty"`
	Display    string                   `json:"display_name,omitempty"`
	Version    string                   `json:"version,omitempty"`
	Phase      skillmarket.InstallPhase `json:"phase,omitempty"`
	BytesDone  int64                    `json:"bytes_done,omitempty"`
	BytesTotal int64                    `json:"bytes_total,omitempty"`
	Error      string                   `json:"error,omitempty"`
	StartedAt  time.Time                `json:"started_at"`
	EndedAt    *time.Time               `json:"ended_at,omitempty"`

	mu     sync.Mutex
	subs   map[chan marketEvent]struct{}
	next   int64
	cancel context.CancelFunc
}

// marketEvent is the SSE payload for one job update.
type marketEvent struct {
	JobID      string                   `json:"job_id"`
	SkillID    int                      `json:"skill_id"`
	State      string                   `json:"state"`
	Phase      skillmarket.InstallPhase `json:"phase,omitempty"`
	Name       string                   `json:"name,omitempty"`
	Display    string                   `json:"display_name,omitempty"`
	Version    string                   `json:"version,omitempty"`
	BytesDone  int64                    `json:"bytes_done,omitempty"`
	BytesTotal int64                    `json:"bytes_total,omitempty"`
	Error      string                   `json:"error,omitempty"`
}

// ErrJobNotFound is returned for unknown job ids.
var ErrJobNotFound = errors.New("skillmarket: install job not found")

// marketJobRegistry runs installs in the background and fans progress out to
// SSE subscribers. Jobs are kept until EndedAt+retention so clients that
// reconnect (or subscribe late) can catch the terminal event.
type marketJobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*marketJob
	// Last terminal event per job for late-subscriber replay (nil while running).
	final map[string]*marketEvent
	seq   int
	app   *app.App
	ctx   context.Context
}

// retention keeps terminal jobs for 10 minutes after completion.
const jobRetention = 10 * time.Minute

func newMarketJobRegistry(ctx context.Context, application *app.App) *marketJobRegistry {
	return &marketJobRegistry{jobs: map[string]*marketJob{}, final: map[string]*marketEvent{}, app: application, ctx: ctx}
}

// Start launches a background install for skillID and returns its job id.
func (r *marketJobRegistry) Start(skillID int) (string, error) {
	client := r.app.SkillMarket()
	if client == nil {
		return "", fmt.Errorf("skillmarket: marketplace disabled")
	}

	r.mu.Lock()
	// One install per skill at a time.
	for _, j := range r.jobs {
		if j.SkillID == skillID && j.State == "running" {
			r.mu.Unlock()
			return "", fmt.Errorf("skillmarket: install already running for skill %d", skillID)
		}
	}
	r.seq++
	jobID := fmt.Sprintf("inst-%d-%d", time.Now().UnixMilli(), r.seq)
	ctx, cancel := context.WithCancel(r.ctx)
	job := &marketJob{
		ID:        jobID,
		SkillID:   skillID,
		State:     "running",
		Phase:     skillmarket.PhaseResolving,
		StartedAt: time.Now(),
		subs:      map[chan marketEvent]struct{}{},
		cancel:    cancel,
	}
	r.jobs[jobID] = job
	r.mu.Unlock()

	go r.run(ctx, job)
	return jobID, nil
}

func (r *marketJobRegistry) run(ctx context.Context, job *marketJob) {
	client := r.app.SkillMarket()
	item, err := client.InstallWithProgress(ctx, job.SkillID, func(p skillmarket.InstallProgress) {
		r.publish(job, marketEvent{
			JobID:      job.ID,
			SkillID:    job.SkillID,
			State:      "running",
			Phase:      p.Phase,
			Name:       p.Name,
			Display:    p.DisplayName,
			Version:    p.Version,
			BytesDone:  p.BytesDone,
			BytesTotal: p.BytesTotal,
			Error:      p.Err,
		})
	})

	if err == nil {
		r.app.BumpSkillMarketRevision()
	}
	now := time.Now()
	r.mu.Lock()
	job.mu.Lock()
	job.EndedAt = &now
	if err != nil {
		job.State = "failed"
		job.Error = err.Error()
		// Already-installed is a benign conflict, not a server error.
		if errors.Is(err, skillmarket.ErrAlreadyInstalled) {
			job.Error = err.Error()
		}
	} else {
		job.State = "succeeded"
		job.SkillName = item.Name
		job.Display = item.DisplayName
		job.Version = item.Version
	}
	job.mu.Unlock()
	evt := marketEvent{
		JobID:   job.ID,
		SkillID: job.SkillID,
		State:   job.State,
		Phase:   skillmarket.PhaseDone,
		Name:    job.SkillName,
		Display: job.Display,
		Version: job.Version,
		Error:   job.Error,
	}
	r.final[job.ID] = &evt
	r.mu.Unlock()

	r.publish(job, evt)

	// Garbage-collect the job after the retention window.
	go func(id string) {
		select {
		case <-time.After(jobRetention):
		case <-r.ctx.Done():
		}
		r.mu.Lock()
		delete(r.jobs, id)
		delete(r.final, id)
		r.mu.Unlock()
	}(job.ID)

}

// publish stamps the event and fans it out to current subscribers.
func (r *marketJobRegistry) publish(job *marketJob, evt marketEvent) {
	job.mu.Lock()
	job.Phase = evt.Phase
	job.BytesDone = evt.BytesDone
	job.BytesTotal = evt.BytesTotal
	if evt.Name != "" {
		job.SkillName = evt.Name
		job.Display = evt.Display
		job.Version = evt.Version
	}
	if evt.Error != "" {
		job.Error = evt.Error
	}
	defer job.mu.Unlock()
	for ch := range job.subs {
		select {
		case ch <- evt:
		default:
			// Keep the newest snapshot, particularly terminal events, even when
			// the subscriber is slow. Publish is serialized under job.mu.
			select {
			case <-ch:
			default:
			}
			ch <- evt
		}
	}
}

// Subscribe registers a channel for job events and immediately replays the
// latest snapshot (terminal event if finished, current snapshot if running).
// The returned cancel func must be called to release the subscription.
func (r *marketJobRegistry) Subscribe(jobID string, buffer int) (<-chan marketEvent, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok {
		return nil, nil, ErrJobNotFound
	}
	job.mu.Lock()
	defer job.mu.Unlock()

	if job.State != "running" && r.final[jobID] != nil {
		// Terminal: replay once, no live subscription needed.
		ch := make(chan marketEvent, 1)
		ch <- *r.final[jobID]
		return ch, func() {}, nil
	}

	ch := make(chan marketEvent, max(buffer, 64))
	snapshot := marketEvent{
		JobID:      job.ID,
		SkillID:    job.SkillID,
		State:      job.State,
		Phase:      job.Phase,
		Name:       job.SkillName,
		Display:    job.Display,
		Version:    job.Version,
		BytesDone:  job.BytesDone,
		BytesTotal: job.BytesTotal,
		Error:      job.Error,
	}
	ch <- snapshot
	job.subs[ch] = struct{}{}
	return ch, func() {
		job.mu.Lock()
		delete(job.subs, ch)
		job.mu.Unlock()
	}, nil
}

// marketJobSnapshot is the lock-free view of a job for API responses.
type marketJobSnapshot struct {
	ID         string                   `json:"id"`
	SkillID    int                      `json:"skill_id"`
	State      string                   `json:"state"`
	SkillName  string                   `json:"skill_name,omitempty"`
	Display    string                   `json:"display_name,omitempty"`
	Version    string                   `json:"version,omitempty"`
	Phase      skillmarket.InstallPhase `json:"phase,omitempty"`
	BytesDone  int64                    `json:"bytes_done,omitempty"`
	BytesTotal int64                    `json:"bytes_total,omitempty"`
	Error      string                   `json:"error,omitempty"`
	StartedAt  time.Time                `json:"started_at"`
	EndedAt    *time.Time               `json:"ended_at,omitempty"`
}

// Snapshot returns the job's current state for polling fallback.
func (r *marketJobRegistry) Snapshot(jobID string) (marketJobSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok {
		return marketJobSnapshot{}, false
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return marketJobSnapshot{
		ID:         job.ID,
		SkillID:    job.SkillID,
		State:      job.State,
		SkillName:  job.SkillName,
		Display:    job.Display,
		Version:    job.Version,
		Phase:      job.Phase,
		BytesDone:  job.BytesDone,
		BytesTotal: job.BytesTotal,
		Error:      job.Error,
		StartedAt:  job.StartedAt,
		EndedAt:    job.EndedAt,
	}, true
}

// Cancel aborts a running install (downloads stop; partial dirs are removed
// by the client's cleanup paths).
func (r *marketJobRegistry) Cancel(jobID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.State != "running" {
		return false
	}
	job.cancel()
	return true
}

// RunningForSkill reports whether a skill already has an install in flight.
func (r *marketJobRegistry) RunningForSkill(skillID int) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		if j.SkillID == skillID && j.State == "running" {
			return j.ID, true
		}
	}
	return "", false
}
