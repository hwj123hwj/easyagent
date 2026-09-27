package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebUI_ServesConsolePages(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// 首页包含导航与三个页面容器
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	body := w.Body.String()
	for _, want := range []string{"page-workflows", "page-sessions", "page-chat", "login-overlay"} {
		assert.Contains(t, body, want)
	}

	// 新增模块可访问
	for _, path := range []string{"/js/api.js", "/js/workflows.js", "/js/sessions.js", "/js/app.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"), path)
	}

	// SPA 回退仍工作
	req = httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.True(t, strings.Contains(w.Body.String(), "<!DOCTYPE html>"))
}
