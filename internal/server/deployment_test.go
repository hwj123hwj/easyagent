package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/workflow"
	"github.com/stretchr/testify/require"
)

func deployRequest(s *Server, method, body, key string) *httptest.ResponseRecorder {
	r := localReq(method, "/admin/deploy", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestDeploymentAdmission(t *testing.T) {
	s := New(newTestApp(t), nil)
	s.SetAPIKey("test")
	require.Equal(t, 401, deployRequest(s, "POST", "{}", "wrong").Code)
	release, ok := s.activity.begin()
	require.True(t, ok)
	require.Equal(t, 409, deployRequest(s, "POST", "{}", "test").Code)
	another, ok := s.activity.begin()
	require.True(t, ok, "busy preparation must not block ongoing use")
	another()
	release()
	w := deployRequest(s, "POST", "{}", "test")
	require.Equal(t, 200, w.Code)
	var lease struct {
		Lease string `json:"lease"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lease))
	require.Len(t, lease.Lease, 48)
	_, ok = s.activity.begin()
	require.False(t, ok, "WS and scheduler use the same gate")
	r := localReq("POST", "/sessions", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer test")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, r)
	require.Equal(t, 503, response.Code)
	require.Equal(t, 409, deployRequest(s, "DELETE", `{"lease":"wrong"}`, "test").Code)
	require.Equal(t, 200, deployRequest(s, "DELETE", `{"lease":"`+lease.Lease+`"}`, "test").Code)
	release, ok = s.activity.begin()
	require.True(t, ok)
	release()
}
func TestDeploymentRequiresExplicitAuthAndLeaseExpires(t *testing.T) {
	s := New(newTestApp(t), nil)
	require.Equal(t, 401, deployRequest(s, "POST", "{}", "").Code)
	s.SetAPIKey("test")
	require.Equal(t, 200, deployRequest(s, "POST", "{}", "test").Code)
	s.activity.mu.Lock()
	s.activity.until = time.Now().Add(-time.Second)
	s.activity.mu.Unlock()
	release, ok := s.activity.begin()
	require.True(t, ok)
	release()
}
func TestAdmissionProtectsEntireRequest(t *testing.T) {
	s := New(newTestApp(t), nil)
	s.SetAPIKey("test")
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := s.admissionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-finish }))
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), localReq("POST", "/chat/stream", nil))
	}()
	<-started
	require.Equal(t, 409, deployRequest(s, "POST", "{}", "test").Code)
	close(finish)
	<-done
	require.Equal(t, 200, deployRequest(s, "POST", "{}", "test").Code)
}
func TestAdmissionConcurrentPrepareAndPrompt(t *testing.T) {
	s := New(newTestApp(t), nil)
	s.SetAPIKey("test")
	for i := 0; i < 100; i++ {
		s.activity.mu.Lock()
		s.activity.until = time.Time{}
		s.activity.mu.Unlock()
		start, finish := make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if done, ok := s.activity.begin(); ok {
				<-finish
				done()
			}
		}()
		close(start)
		response := deployRequest(s, "POST", "{}", "test")
		if response.Code == 200 {
			s.activity.mu.Lock()
			require.Zero(t, s.activity.active)
			s.activity.mu.Unlock()
		} else {
			require.Equal(t, 409, response.Code)
		}
		close(finish)
		wg.Wait()
	}
}

func TestDeploymentDefersWhileWorkflowWaitsForApproval(t *testing.T) {
	s, _ := newWorkflowTestServer(t)
	s.SetAPIKey("test")
	spec, err := workflow.ParseSpec([]byte("name: pending\nsteps:\n  - id: approval\n    prompt: wait\n    confirm: true\n"))
	require.NoError(t, err)
	id, err := s.workflowRegistry().Start(spec, nil)
	require.NoError(t, err)
	defer s.workflowRegistry().Cancel(id)
	require.Equal(t, 409, deployRequest(s, "POST", "{}", "test").Code)
	require.NoError(t, s.workflowRegistry().Cancel(id))
	require.Eventually(t, func() bool { return s.workflowRegistry().ActiveCount() == 0 }, time.Second, time.Millisecond)
	require.Equal(t, 200, deployRequest(s, "POST", "{}", "test").Code)
}
