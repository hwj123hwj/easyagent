package feishu

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// cardUpdater holds one latest snapshot, never a queue of token-by-token HTTP
// requests. The SSE reader only copies state; one worker owns all progress I/O.
type cardUpdater struct {
	mu      sync.Mutex
	text    string
	metrics FooterMetrics
	wake    chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc
}

func newCardUpdater(ctx context.Context, card *StreamingCardHandle, interval time.Duration) *cardUpdater {
	ctx, cancel := context.WithCancel(ctx)
	u := &cardUpdater{wake: make(chan struct{}, 1), done: make(chan struct{}), cancel: cancel}
	go u.run(ctx, card, interval)
	return u
}

func (u *cardUpdater) update(text string, metrics FooterMetrics) {
	u.mu.Lock()
	u.text, u.metrics = text, metrics
	u.mu.Unlock()
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// Stop cancels in-flight progress I/O and joins the worker before finalization.
// The final card carries all text, so stale intermediate snapshots are discarded.
func (u *cardUpdater) stop() { u.cancel(); <-u.done }

func (u *cardUpdater) run(ctx context.Context, card *StreamingCardHandle, interval time.Duration) {
	defer close(u.done)
	var lastAttempt time.Time
	failed := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.wake:
		}
		if delay := time.Until(lastAttempt.Add(interval)); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		// Consume notifications accumulated while waiting; send the latest snapshot.
		select {
		case <-u.wake:
		default:
		}
		if ctx.Err() != nil {
			return
		}
		u.mu.Lock()
		text, metrics := u.text, u.metrics
		u.mu.Unlock()
		lastAttempt = time.Now()
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := card.PushUpdate(requestCtx, text, metrics)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !failed {
				slog.Warn("card progress update failed; retrying latest state", "error", err)
			}
			failed = true
			select {
			case u.wake <- struct{}{}:
			default:
			}
		} else {
			failed = false
		}
	}
}
