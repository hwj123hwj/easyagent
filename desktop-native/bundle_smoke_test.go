package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Invoked by build-native.sh against the staged bundle, without opening the UI.
func TestPackagedCoreSmoke(t *testing.T) {
	bundle := os.Getenv("EASYAGENT_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("set EASYAGENT_TEST_BUNDLE to validate a built app")
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "EA_") || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "ANTHROPIC_") || key == "EASYAGENT_NATIVE_CORE" {
			t.Setenv(key, "")
		}
	}
	exe := filepath.Join(bundle, "Contents", "MacOS", "EasyAgent")
	want := filepath.Join(bundle, "Contents", "Resources", "easyagent")
	core, err := coreBinaryAt(exe)
	if err != nil || core != want {
		t.Fatalf("bundle selected %q, want %q: %v", core, want, err)
	}
	for _, stale := range []string{"runtime", "workflow-runtime.mjs"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(core), stale)); !os.IsNotExist(err) {
			t.Fatalf("obsolete bundled resource %s: %v", stale, err)
		}
	}
	dir := t.TempDir()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"smoke-only","object":"model"}]}`))
			return
		}
		t.Errorf("health smoke unexpectedly called model endpoint: %s", r.URL.Path)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	for key, value := range map[string]string{
		"EASYAGENT_NATIVE_CORE": core,
		"EA_ENV_FILE":           filepath.Join(dir, "absent.env"),
		"EA_WORKSPACE":          dir, "EA_KB_REPO_PATH": dir,
		"EA_SKILL_MARKET": "0", "EA_ENABLE_BASH": "false",
	} {
		t.Setenv(key, value)
	}
	backend := &localBackend{}
	defer backend.close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	model := modelConfig{Mode: "override", Provider: "openai", BaseURL: provider.URL, Model: "smoke-only"}
	client, err := backend.start(ctx, dir, model, &memorySecrets{values: map[string]string{"model": "smoke-only"}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", client.URL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health status: %d", res.StatusCode)
	}
	done := backend.done
	backend.stop()
	select {
	case <-done:
	default:
		t.Fatal("core still running after stop")
	}
}
