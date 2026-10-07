package server

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise the public dispatcher, not just a handler that bypasses routing.
func TestSettingsRoutesDispatchThroughAuthenticatedREST(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	s.SetAPIKey("route-test-secret")
	t.Setenv("PATH", t.TempDir())
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/computer/settings", ""},
		{"POST", "/computer/settings", `{"approval_policy":"ask"}`},
		{"POST", "/computer/permissions", ""},
		{"GET", "/usage/summary", ""},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			for _, token := range []string{"", "wrong", "route-test-secret"} {
				req := localReq(route.method, route.path, strings.NewReader(route.body))
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				if token != "route-test-secret" {
					require.Equal(t, http.StatusUnauthorized, rec.Code)
					continue
				}
				// No helper is installed in test runners; requesting permissions must
				// reach its handler and fail truthfully instead of serving the web UI.
				if route.path == "/computer/permissions" {
					require.Equal(t, http.StatusServiceUnavailable, rec.Code)
				} else {
					require.Equal(t, http.StatusOK, rec.Code)
				}
				require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
				if route.path == "/computer/settings" {
					require.Equal(t, `[]`, string(payload["approved_apps"]))
				}
			}
		})
	}
}
