package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWFFactory 服务端测试用 RunnerFactory，返回确定性输出。
type fakeWFFactory struct {
	mu      sync.Mutex
	calls   []string
	started chan string // 设置后让步骤等待取消，避免取消测试与瞬时完成竞争。
}

func (f *fakeWFFactory) NewRunner(ctx context.Context, opts workflow.RunnerOptions) (workflow.Runner, error) {
	return &fakeWFRunner{f: f}, nil
}

func (f *fakeWFFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeWFRunner struct{ f *fakeWFFactory }

func (r *fakeWFRunner) Prompt(ctx context.Context, prompt string) (string, error) {
	r.f.mu.Lock()
	r.f.calls = append(r.f.calls, prompt)
	r.f.mu.Unlock()
	if r.f.started != nil {
		r.f.started <- prompt
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "OUT(" + prompt + ")", nil
}

func (r *fakeWFRunner) Close() error { return nil }

func newWorkflowTestServer(t *testing.T) (*Server, *fakeWFFactory) {
	t.Helper()
	application := newTestApp(t)
	srv := New(application, nil)
	f := &fakeWFFactory{}
	srv.SetWorkflowRunnerFactory(f)
	return srv, f
}

func waitForRunStatus(t *testing.T, srv *Server, runID string, want ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := localReq(http.MethodGet, "/workflows/"+runID, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Meta workflow.RunMeta `json:"meta"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		for _, status := range want {
			if resp.Meta.Status == status {
				return map[string]any{"meta": resp.Meta}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach %q in time", runID, want)
	return nil
}

const chainYAML = `
name: server-chain
vars:
  thing: widget
steps:
  - id: first
    prompt: "make {{vars.thing}}"
  - id: second
    prompt: "polish {{steps.first.output}}"
`

func TestServer_WorkflowRunToEnd(t *testing.T) {
	srv, f := newWorkflowTestServer(t)

	req := localReq(http.MethodPost, "/workflows", strings.NewReader(chainYAML))
	req.Header.Set("Content-Type", "text/yaml")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	var startResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&startResp))
	require.NotEmpty(t, startResp.RunID)

	waitForRunStatus(t, srv, startResp.RunID, workflow.StatusCompleted)
	assert.Equal(t, 2, f.count())

	// 列表可见
	req = localReq(http.MethodGet, "/workflows", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Runs []workflow.RunMeta `json:"runs"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Len(t, list.Runs, 1)
	assert.Equal(t, "server-chain", list.Runs[0].Name)
	assert.Equal(t, workflow.StatusCompleted, list.Runs[0].Status)
}

func TestServer_WorkflowJSONBodyWithVars(t *testing.T) {
	srv, _ := newWorkflowTestServer(t)

	body := fmt.Sprintf(`{"yaml": %q, "vars": {"thing": "gadget"}}`, chainYAML)
	req := localReq(http.MethodPost, "/workflows", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	var startResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&startResp))
	res := waitForRunStatus(t, srv, startResp.RunID, workflow.StatusCompleted)

	meta := res["meta"].(workflow.RunMeta)
	// vars 覆盖生效
	assert.Equal(t, "OUT(make gadget)", meta.Outputs["first"].Output)
}

func TestServer_WorkflowInvalidSpec(t *testing.T) {
	srv, _ := newWorkflowTestServer(t)

	req := localReq(http.MethodPost, "/workflows", strings.NewReader("name: bad\nsteps: []"))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_WorkflowGateApproveFlow(t *testing.T) {
	srv, _ := newWorkflowTestServer(t)

	yamlSrc := `
name: gated-server
steps:
  - id: draft
    prompt: "draft"
  - id: publish
    prompt: "publish {{steps.draft.output}}"
    confirm: true
`
	req := localReq(http.MethodPost, "/workflows", strings.NewReader(yamlSrc))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	var startResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&startResp))

	waitForRunStatus(t, srv, startResp.RunID, workflow.StatusWaiting)

	// 审批放行（step 省略 → 唯一等待门）
	req = localReq(http.MethodPost, "/workflows/"+startResp.RunID+"/approve", bytes.NewReader([]byte(`{}`)))
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	res := waitForRunStatus(t, srv, startResp.RunID, workflow.StatusCompleted)
	meta := res["meta"].(workflow.RunMeta)
	assert.Equal(t, "OUT(publish OUT(draft))", meta.Outputs["publish"].Output)
}

func TestServer_WorkflowGateRejectFlow(t *testing.T) {
	srv, f := newWorkflowTestServer(t)

	yamlSrc := `
name: gated-reject-server
steps:
  - id: risky
    prompt: "risky"
    confirm: true
`
	req := localReq(http.MethodPost, "/workflows", strings.NewReader(yamlSrc))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	var startResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&startResp))

	waitForRunStatus(t, srv, startResp.RunID, workflow.StatusWaiting)

	req = localReq(http.MethodPost, "/workflows/"+startResp.RunID+"/reject", bytes.NewReader([]byte(`{}`)))
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	waitForRunStatus(t, srv, startResp.RunID, workflow.StatusRejected)
	assert.Equal(t, 0, f.count())
}

func TestServer_WorkflowCancel(t *testing.T) {
	srv, f := newWorkflowTestServer(t)
	f.started = make(chan string, 1)
	t.Cleanup(srv.workflowRegistry().CancelAll)

	yamlSrc := `
name: cancel-me
steps:
  - id: a
    prompt: "a"
  - id: b
    prompt: "b {{steps.a.output}}"
`
	req := localReq(http.MethodPost, "/workflows", strings.NewReader(yamlSrc))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	var startResp struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&startResp))

	// 确认第一步已执行且仍在等待，再验证取消及后续步骤不执行。
	select {
	case prompt := <-f.started:
		require.Equal(t, "a", prompt)
	case <-time.After(5 * time.Second):
		t.Fatal("workflow did not start its first step")
	}
	req = localReq(http.MethodPost, "/workflows/"+startResp.RunID+"/cancel", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Cancellation completes asynchronously; wait for its persisted terminal state.
	waitForRunStatus(t, srv, startResp.RunID, workflow.StatusCancelled)
	assert.Equal(t, 1, f.count(), "取消后不得执行依赖步骤")
}

func TestServer_WorkflowGetUnknownID(t *testing.T) {
	srv, _ := newWorkflowTestServer(t)
	req := localReq(http.MethodGet, "/workflows/wf-99999999-deadbeef", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 非法 ID（路径穿越防护）
	req = localReq(http.MethodGet, "/workflows/..%2f..%2fetc", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
