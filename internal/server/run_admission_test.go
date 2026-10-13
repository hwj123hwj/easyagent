package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func blockAdmissionSnapshot(t *testing.T, s *Server) (<-chan struct{}, func()) {
	t.Helper()
	entered, released := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	t.Cleanup(release)
	s.workspaceSnapshot = func(string) (map[string]fileState, []string) {
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-released:
		case <-s.ctx.Done():
		}
		return map[string]fileState{}, nil
	}
	return entered, release
}

// Only the observed preparation operation is timed. Entering it is independent
// of external git startup and temporary PATH lifetimes across test cleanup.
func waitAdmissionSnapshot(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("受理未进入工作区快照准备阶段")
	}
}

func requireAdmissionResponsive(t *testing.T, operation func()) {
	t.Helper()
	finished := make(chan struct{})
	go func() { operation(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("其他受理准备不能阻塞取消、busy 判断或会话变更")
	}
}

func TestAdmissionPreparationDoesNotBlockUnrelatedCancellation(t *testing.T) {
	s, _, gateway := newRunTestServer(t)
	first, _, err := s.startRun("", "first", "first-admission")
	require.NoError(t, err)
	select {
	case <-gateway.requests:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not reached")
	}
	other, err := s.app.NewSession(context.Background())
	require.NoError(t, err)
	entered, release := blockAdmissionSnapshot(t, s)
	type result struct {
		run *sessionRun
		err error
	}
	started := make(chan result, 1)
	go func() {
		run, _, err := s.startRun(other.SessionID(), "second", "second-admission")
		started <- result{run, err}
	}()
	waitAdmissionSnapshot(t, entered)
	var cancelErr error
	requireAdmissionResponsive(t, func() { cancelErr = s.cancelRun(first.SessionID, first.ID) })
	require.NoError(t, cancelErr)
	release()
	var second result
	select {
	case second = <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("受理未在截止时间内返回")
	}
	require.NoError(t, second.err)
	require.NotNil(t, second.run)
	require.NoError(t, s.cancelRun(second.run.SessionID, second.run.ID))
}

func TestAdmissionPinsRequestAndSessionUntilPreparationCompletes(t *testing.T) {
	s, _, gateway := newRunTestServer(t)
	sess, err := s.app.NewSession(context.Background())
	require.NoError(t, err)
	entered, release := blockAdmissionSnapshot(t, s)
	type result struct {
		run       *sessionRun
		duplicate bool
		err       error
	}
	started := make(chan result, 2)
	go func() {
		run, duplicate, err := s.startRun(sess.SessionID(), "task", "pinned-admission")
		started <- result{run, duplicate, err}
	}()
	waitAdmissionSnapshot(t, entered)
	go func() {
		run, duplicate, err := s.startRun(sess.SessionID(), "task", "pinned-admission")
		started <- result{run, duplicate, err}
	}()
	requireAdmissionResponsive(t, func() { _, _, err = s.startRun(sess.SessionID(), "changed", "pinned-admission") })
	require.ErrorContains(t, err, "request_id 已用于另一条消息")
	requireAdmissionResponsive(t, func() { _, _, err = s.startRun(sess.SessionID(), "other", "different-admission") })
	require.ErrorContains(t, err, "会话正在处理")
	for _, operation := range []struct{ method, path, body string }{{"DELETE", "/sessions/", ""}, {"POST", "/sessions/", "{}"}} {
		path := operation.path + sess.SessionID()
		if operation.method == "POST" {
			path += "/fork"
		}
		request := localReq(operation.method, path, nil)
		response := httptest.NewRecorder()
		requireAdmissionResponsive(t, func() { s.Handler().ServeHTTP(response, request) })
		require.Equal(t, http.StatusConflict, response.Code)
	}
	release()
	var results []result
	for i := 0; i < 2; i++ {
		select {
		case result := <-started:
			results = append(results, result)
		case <-time.After(3 * time.Second):
			t.Fatal("原请求或重复请求未在截止时间内返回")
		}
	}
	a, b := results[0], results[1]
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Same(t, a.run, b.run)
	require.NotEqual(t, a.duplicate, b.duplicate)
	require.Eventually(t, func() bool { return gateway.calls.Load() == 1 }, 3*time.Second, time.Millisecond)
	require.NoError(t, s.cancelRun(sess.SessionID(), a.run.ID))
}
