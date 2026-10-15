package coding

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hwj123hwj/easyagent/internal/agents/coding/profile"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	basetools "github.com/hwj123hwj/easyagent/sdk/tools"
)

// CodingSessionExt implements runtime.SessionExt for the coding-agent.
// It holds per-session application state (profile, goal, readTracker).
type CodingSessionExt struct {
	mu          sync.RWMutex
	profile     string
	goal        string
	rebuild     func() error
	readTracker *basetools.ReadTracker
}

// NewCodingSessionExt creates a new CodingSessionExt with default profile "coding".
func NewCodingSessionExt(rebuild func() error) *CodingSessionExt {
	return &CodingSessionExt{
		profile:     string(profile.ProfileCoding),
		rebuild:     rebuild,
		readTracker: basetools.NewReadTracker(),
	}
}

// ReadTracker returns the per-session ReadTracker.
func (e *CodingSessionExt) ReadTracker() *basetools.ReadTracker {
	if e == nil {
		return nil
	}
	return e.readTracker
}

// SetRebuild sets the rebuild callback. Called by AgentSession after creation
// to inject the rebuild function (avoids circular dependency in constructor).
// NOTE: Parameter must be `func() error` (not a named type) so that the
// interface assertion in AgentSession (`interface{ SetRebuild(func() error) }`)
// succeeds. Go treats named types as distinct from their underlying types.
func (e *CodingSessionExt) SetRebuild(fn func() error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rebuild = fn
}

func (e *CodingSessionExt) Profile() string { e.mu.RLock(); defer e.mu.RUnlock(); return e.profile }

func (e *CodingSessionExt) SwitchProfile(ctx context.Context, p string) error {
	if !profile.Valid(p) {
		return fmt.Errorf("unknown profile: %q (available: %v)", p, profile.All())
	}
	e.mu.Lock()
	old, rebuild := e.profile, e.rebuild
	e.profile = p
	e.mu.Unlock()
	if rebuild != nil {
		if err := rebuild(); err != nil {
			e.mu.Lock()
			e.profile = old
			e.mu.Unlock()
			return fmt.Errorf("rebuild agent with profile %q: %w", p, err)
		}
	}
	return nil
}

func (e *CodingSessionExt) Goal() string { e.mu.RLock(); defer e.mu.RUnlock(); return e.goal }

func (e *CodingSessionExt) SetGoal(goal string) {
	e.mu.Lock()
	old, rebuild := e.goal, e.rebuild
	e.goal = goal
	e.mu.Unlock()
	if rebuild != nil {
		if err := rebuild(); err != nil {
			e.mu.Lock()
			e.goal = old
			e.mu.Unlock()
			slog.Error("failed to rebuild agent after goal set", "error", err)
		}
	}
}

func (e *CodingSessionExt) ClearGoal() {
	e.SetGoal("")
}

// Compile-time check.
var _ runtime.SessionExt = (*CodingSessionExt)(nil)
