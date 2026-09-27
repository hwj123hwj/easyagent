package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Admission and idle-to-draining transition share one mutex, so a successful
// deployment lease cannot race a new prompt. Busy preparation never cancels work.
type activityGate struct {
	mu      sync.Mutex
	active  int
	lease   string
	until   time.Time
	closing bool
}

func (g *activityGate) begin() (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing || time.Now().Before(g.until) {
		return nil, false
	}
	g.active++
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); g.active--; g.mu.Unlock() }) }, true
}
func (s *Server) backgroundRuns() int {
	n := s.app.DynamicWorkflows().ActiveCount()
	s.wfMu.Lock()
	defer s.wfMu.Unlock()
	if s.wfReg != nil {
		n += s.wfReg.ActiveCount()
	}
	return n
}
func (s *Server) admissionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || r.URL.Path == "/admin/deploy" {
			next.ServeHTTP(w, r)
			return
		}
		done, ok := s.activity.begin()
		if !ok {
			w.Header().Set("Retry-After", "5")
			writeError(w, 503, "服务正在更新，请稍后重试；请求尚未执行")
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) deploymentControl(w http.ResponseWriter, r *http.Request) {
	// Always require a configured token, even for loopback/open-access deployments.
	if s.apiKey == "" || bearerToken(r) != s.apiKey {
		writeError(w, 401, "deployment control requires Bearer authentication")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	g := &s.activity
	g.mu.Lock()
	defer g.mu.Unlock()
	active := g.active + s.backgroundRuns()
	switch r.Method {
	case http.MethodGet:
		writeDeploymentJSON(w, map[string]any{"active": active, "draining": g.closing || time.Now().Before(g.until)})
	case http.MethodPost:
		if active != 0 || g.closing || time.Now().Before(g.until) {
			writeError(w, 409, "service busy; deployment deferred")
			return
		}
		token := make([]byte, 24)
		if _, err := rand.Read(token); err != nil {
			writeError(w, 500, "cannot create deployment lease")
			return
		}
		g.lease = hex.EncodeToString(token)
		g.until = time.Now().Add(5 * time.Minute)
		writeDeploymentJSON(w, map[string]any{"lease": g.lease, "expires_at": g.until, "active": 0})
	case http.MethodDelete:
		var request struct {
			Lease string `json:"lease"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request) != nil || request.Lease == "" || request.Lease != g.lease {
			writeError(w, 409, "deployment lease mismatch")
			return
		}
		g.lease = ""
		g.until = time.Time{}
		writeDeploymentJSON(w, map[string]bool{"released": true})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeError(w, 405, "method not allowed")
	}
}
func writeDeploymentJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) serveUntilSignal(addr string) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer s.cancel()
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return s.ctx }}
	exited := make(chan error, 1)
	go func() { exited <- srv.ListenAndServe() }()
	select {
	case err := <-exited:
		return err
	case <-signalCtx.Done():
	}
	s.activity.mu.Lock()
	s.activity.closing = true
	s.activity.mu.Unlock()
	s.cancel()
	s.wfMu.Lock()
	if s.wfReg != nil {
		s.wfReg.CancelAll()
	}
	s.wfMu.Unlock()
	go s.app.DynamicWorkflows().Close()
	// Cancellation first; keep the process alive while tool results are persisted.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.activity.mu.Lock()
		active := s.activity.active
		s.activity.mu.Unlock()
		if active+s.backgroundRuns() == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	err := <-exited
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
