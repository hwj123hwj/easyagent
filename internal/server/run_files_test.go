package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func reviewedWrite(t *testing.T, s *Server, run *sessionRun, path, content string) {
	t.Helper()
	s.observeFiles(run, agent.AgentStreamEvent{Type: agent.StreamEventToolStart, ToolName: "write", ToolCallID: path, ToolArgs: map[string]any{"path": path}})
	require.NoError(t, os.WriteFile(filepath.Join(run.files.Workspace, path), []byte(content), 0644))
	s.observeFiles(run, agent.AgentStreamEvent{Type: agent.StreamEventToolEnd, ToolCallID: path})
}
func undoFile(t *testing.T, s *Server, run *sessionRun, path, version string, code int) {
	t.Helper()
	data, _ := json.Marshal(map[string]string{"path": path, "after_hash": version})
	r := httptest.NewRequest("POST", "/undo", bytes.NewReader(data))
	r.SetPathValue("id", run.SessionID)
	r.SetPathValue("run", run.ID)
	w := httptest.NewRecorder()
	s.undoRunFile(w, r)
	require.Equal(t, code, w.Code, w.Body.String())
}
func TestTaskReviewPreservesDirtyStagedAndUntrackedFiles(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	root := sess.Workspace()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		data, err := cmd.CombinedOutput()
		require.NoError(t, err, string(data))
		return string(data)
	}
	git("init")
	require.NoError(t, os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("staged-user-work"), 0644))
	git("add", "tracked.txt")
	index := git("show", ":tracked.txt")
	require.NoError(t, os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("unstaged-user-work"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "prior.txt"), []byte("untracked-user-work"), 0644))
	run := &sessionRun{ID: "run-review", SessionID: sess.SessionID(), StartedAt: time.Now()}
	s.beginFiles(run, root)
	require.NotNil(t, run.files)
	reviewedWrite(t, s, run, "tracked.txt", "agent-edit")
	reviewedWrite(t, s, run, "prior.txt", "agent-edit-2")
	reviewedWrite(t, s, run, "new.pdf", "%PDF-1.4\nfixture")
	require.NoError(t, os.WriteFile(filepath.Join(root, "external.txt"), []byte("external-write"), 0644))
	s.finishFiles(run)
	snapshot, err := s.loadFiles(run.SessionID, run.ID)
	require.NoError(t, err)
	changes := s.changes(snapshot)
	require.Len(t, changes, 4)
	for _, change := range changes {
		if change.Path == "external.txt" {
			require.False(t, change.CanUndo)
			require.Equal(t, "observed", change.Attribution)
			continue
		}
		require.True(t, change.CanUndo, change.Path)
		undoFile(t, s, run, change.Path, "stale-version", 409)
		undoFile(t, s, run, change.Path, change.Version, 200)
	}
	data, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	require.NoError(t, err)
	require.Equal(t, "unstaged-user-work", string(data))
	data, err = os.ReadFile(filepath.Join(root, "prior.txt"))
	require.NoError(t, err)
	require.Equal(t, "untracked-user-work", string(data))
	require.Equal(t, index, git("show", ":tracked.txt"))
	_, err = os.Stat(filepath.Join(root, "new.pdf"))
	require.True(t, os.IsNotExist(err))
}
func TestTaskUndoRejectsPostRunAndDuringRunEdits(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	run := &sessionRun{ID: "run-conflict", SessionID: sess.SessionID(), StartedAt: time.Now()}
	s.beginFiles(run, sess.Workspace())
	reviewedWrite(t, s, run, "after.txt", "agent")
	reviewedWrite(t, s, run, "during.txt", "agent")
	require.NoError(t, os.WriteFile(filepath.Join(sess.Workspace(), "during.txt"), []byte("user-after-tool"), 0644))
	s.finishFiles(run)
	changes := s.changes(run.files)
	for _, c := range changes {
		if c.Path == "during.txt" {
			require.False(t, c.CanUndo)
			require.Equal(t, "observed", c.Attribution)
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(sess.Workspace(), "after.txt"), []byte("post-run-user-work"), 0644))
	undoFile(t, s, run, "after.txt", changes[0].Version, 409)
}
func TestFileDeliveryIsAuthenticatedAndProjectScoped(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sess.Workspace(), "report.pdf"), []byte("%PDF-1.4\nfixture"), 0644))
	for _, entry := range []struct {
		path string
		code int
	}{{"report.pdf", 200}, {"../../outside", 400}} {
		r := httptest.NewRequest("GET", "/workspace/file-data?session_id="+sess.SessionID()+"&path="+entry.path, nil)
		w := httptest.NewRecorder()
		s.workspaceFileData(w, r)
		require.Equal(t, entry.code, w.Code)
		if entry.code == 200 {
			require.Contains(t, w.Body.String(), "application/pdf")
		}
	}
	s.SetAPIKey("test-secret")
	r := httptest.NewRequest("GET", "/workspace/file-data?path=report.pdf", nil)
	w := httptest.NewRecorder()
	s.authMiddleware(http.HandlerFunc(s.workspaceFileData)).ServeHTTP(w, r)
	require.Equal(t, 401, w.Code)
}

func TestTaskFilesObserveRealAgentWritesAcrossConsecutiveRuns(t *testing.T) {
	s, _, gateway := newFileRunTestServer(t, "report.md")
	close(gateway.finish)
	var sessionID string
	for i := 0; i < 3; i++ {
		run, _, err := s.startRun(sessionID, "write report", fmt.Sprintf("real-tool-%d", i))
		require.NoError(t, err)
		sessionID = run.SessionID
		_, err = s.waitRun(context.Background(), run)
		require.NoError(t, err)
		files, err := s.loadFiles(sessionID, run.ID)
		require.NoError(t, err)
		changes := s.changes(files)
		require.Len(t, changes, 1)
		require.Equal(t, "report.md", changes[0].Path)
		require.Equal(t, "file_tool", changes[0].Attribution)
		require.True(t, changes[0].CanUndo)
	}
}
func TestSnapshotsExcludeServiceDataEvenInGitWorkspace(t *testing.T) {
	s, _, _ := newFileRunTestServer(t, "", true)
	root := s.app.Config().Workspace
	require.NoError(t, os.MkdirAll(s.app.Config().DataDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(s.app.Config().DataDir, "private.json"), []byte("private"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "user.md"), []byte("user"), 0600))
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	require.NoError(t, cmd.Run())
	states, _ := s.snapshotFiles(root)
	require.Len(t, states, 1)
	require.Contains(t, states, "user.md")
}

func TestPreferenceBrowserPreflightAllowsPatch(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	s.allowedOrigins = []string{"http://localhost:5175"}
	r := httptest.NewRequest("OPTIONS", "/sessions/example", nil)
	r.Header.Set("Origin", "http://localhost:5175")
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "PATCH")
}
