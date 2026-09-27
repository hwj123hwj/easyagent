package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDynamicWorkflowRoutesRequireAuthentication(t *testing.T) {
	srv := New(newTestApp(t), nil)
	srv.SetAPIKey("test-secret")
	for _, route := range []struct{ method, path string }{{"GET", "/dynamic-workflows"}, {"GET", "/dynamic-workflows/dw-000000000000000000000000"}, {"POST", "/dynamic-workflows/dw-000000000000000000000000/cancel"}, {"POST", "/dynamic-workflows/dw-000000000000000000000000/resume"}} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, localReq(route.method, route.path, nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	req := localReq("GET", "/dynamic-workflows", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"runs":[]`)
	req = localReq("POST", "/dynamic-workflows/dw-000000000000000000000000/resume", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-secret")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
