package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/assert"
)

func TestAllowedOriginPreflightDoesNotRequireBearerButActualRequestDoes(t *testing.T) {
	t.Setenv("EA_ALLOWED_ORIGINS", "http://localhost:5173")
	application := modelTestApp(t, func(cfg *config.Config) { cfg.OpenAIBaseURL = "" })
	srv := New(application, nil)
	srv.apiKey = "server-test-token"
	for _, tc := range []struct {
		method, origin, token string
		status                int
		allowOrigin           string
	}{
		{http.MethodOptions, "http://localhost:5173", "", http.StatusNoContent, "http://localhost:5173"},
		{http.MethodGet, "http://localhost:5173", "", http.StatusUnauthorized, "http://localhost:5173"},
		{http.MethodGet, "http://localhost:5173", "server-test-token", http.StatusOK, "http://localhost:5173"},
		{http.MethodOptions, "https://untrusted.example", "", http.StatusNoContent, ""},
		{http.MethodGet, "https://untrusted.example", "", http.StatusUnauthorized, ""},
	} {
		t.Run(tc.method+tc.origin+tc.token, func(t *testing.T) {
			req := localReq(tc.method, "/models", nil)
			req.Header.Set("Origin", tc.origin)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", "GET")
				req.Header.Set("Access-Control-Request-Headers", "Authorization")
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.allowOrigin, w.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}
