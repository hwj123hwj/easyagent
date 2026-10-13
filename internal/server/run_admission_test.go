package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func delayedSnapshotGit(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX 测试替身；生产实现不依赖 shell")
	}
	git, err := exec.LookPath("git")
	require.NoError(t, err)
	dir := t.TempDir()
	marker := filepath.Join(dir, "entered")
	// Git 路径来自测试机器，不含业务数据；delay 只发生在快照扫描。
	script := "#!/bin/sh\n: > '" + marker + "'\nsleep 0.6\nexec '" + git + "' \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
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
	marker := delayedSnapshotGit(t)
	started := make(chan *sessionRun, 1)
	go func() {
		run, _, err := s.startRun(other.SessionID(), "second", "second-admission")
		if err != nil {
			t.Errorf("start: %v", err)
		}
		started <- run
	}()
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 2*time.Second, 5*time.Millisecond)
	before := time.Now()
	require.NoError(t, s.cancelRun(first.SessionID, first.ID))
	elapsed := time.Since(before)
	var second *sessionRun
	select {
	case second = <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("受理未在截止时间内返回")
	}
	if second != nil {
		require.NoError(t, s.cancelRun(second.SessionID, second.ID))
	}
	require.Less(t, elapsed, 250*time.Millisecond, "其他会话的工作区扫描不能阻塞取消")
}

func TestAdmissionPinsRequestAndSessionUntilPreparationCompletes(t *testing.T) {
	s, _, gateway := newRunTestServer(t)
	sess, err := s.app.NewSession(context.Background())
	require.NoError(t, err)
	marker := delayedSnapshotGit(t)
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
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 2*time.Second, 5*time.Millisecond)
	go func() {
		run, duplicate, err := s.startRun(sess.SessionID(), "task", "pinned-admission")
		started <- result{run, duplicate, err}
	}()
	_, _, err = s.startRun(sess.SessionID(), "changed", "pinned-admission")
	require.ErrorContains(t, err, "request_id 已用于另一条消息")
	before := time.Now()
	_, _, err = s.startRun(sess.SessionID(), "other", "different-admission")
	require.ErrorContains(t, err, "会话正在处理")
	for _, operation := range []struct{ method, path, body string }{{"DELETE", "/sessions/", ""}, {"POST", "/sessions/", "{}"}} {
		path := operation.path + sess.SessionID()
		if operation.method == "POST" {
			path += "/fork"
		}
		request := localReq(operation.method, path, nil)
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusConflict, response.Code)
	}
	require.Less(t, time.Since(before), 250*time.Millisecond, "准备中的会话立即拒绝其他受理与变更")
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
