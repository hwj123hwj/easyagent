package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureSettings(t *testing.T) *feishuSettings {
	t.Helper()
	dir := t.TempDir()
	f := &feishuSettings{envFile: filepath.Join(dir, "feishu.env"), ownerFile: filepath.Join(dir, "owner.json"), run: func(context.Context, string, ...string) (string, error) { return "active", nil }}
	os.WriteFile(f.envFile, []byte("# deployment\nFEISHU_APP_ID=cli_old\nFEISHU_APP_SECRET=private_secret\nEXTRA=keep\n"), 0600)
	os.WriteFile(f.ownerFile, []byte(`{"owner":"owner123"}`), 0600)
	return f
}
func TestFeishuSettingsSave(t *testing.T) {
	f := fixtureSettings(t)
	owner, _ := os.ReadFile(f.ownerFile)
	if err := f.save(context.Background(), "cli_old", ""); err != nil {
		t.Fatal(err)
	}
	values, _ := f.values()
	if values["FEISHU_APP_SECRET"] != "private_secret" || values["EXTRA"] != "keep" {
		t.Fatal("lost existing settings")
	}
	if err := f.save(context.Background(), "cli_old", "new_secret"); err != nil {
		t.Fatal(err)
	}
	values, _ = f.values()
	if values["FEISHU_APP_ID"] != "cli_old" || values["FEISHU_APP_SECRET"] != "new_secret" {
		t.Fatal(values)
	}
	info, _ := os.Stat(f.envFile)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	after, _ := os.ReadFile(f.ownerFile)
	if string(owner) != string(after) {
		t.Fatal("owner changed")
	}
	status, err := f.status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(status)
	if strings.Contains(string(b), "new_secret") || strings.Contains(string(b), "owner123") {
		t.Fatal("credentials leaked")
	}
}
func TestFeishuSettingsRejectAndRollback(t *testing.T) {
	f := fixtureSettings(t)
	before, _ := os.ReadFile(f.envFile)
	for _, input := range [][2]string{{"cli_new", "new_secret"}, {"cli_new", ""}, {"bad\nINJECT=true", "secret"}, {"cli_old", "$(whoami)"}} {
		if err := f.save(context.Background(), input[0], input[1]); err == nil {
			t.Fatal("accepted invalid credentials")
		}
	}
	f.run = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("secret diagnostic must not leak")
	}
	if err := f.save(context.Background(), "cli_old", "new_secret"); err == nil || strings.Contains(err.Error(), "diagnostic") {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(f.envFile)
	if string(before) != string(after) {
		t.Fatal("rollback lost configuration")
	}
}
func TestFeishuSettingsAuthAndPairing(t *testing.T) {
	f := fixtureSettings(t)
	t.Setenv("EA_FEISHU_ENV_FILE", f.envFile)
	t.Setenv("FEISHU_OWNER_STATE_FILE", f.ownerFile)
	s := &Server{apiKey: "test-key"}
	mux := http.NewServeMux()
	s.registerFeishuSettings(mux)
	handler := s.authMiddleware(mux)
	request := func(path, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/settings/feishu", "/settings/feishu/pairing"} {
		if w := request(path, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request("/settings/feishu/pairing", "test-key"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	state := map[string]any{"code": "one-use-code", "expires": time.Now().Add(time.Hour)}
	b, _ := json.Marshal(state)
	os.WriteFile(f.ownerFile, b, 0600)
	w := request("/settings/feishu/pairing", "test-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/pair one-use-code") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ := os.ReadFile(f.ownerFile)
	if string(after) != string(b) {
		t.Fatal("reading consumed pairing")
	}
	state["expires"] = time.Now().Add(-time.Hour)
	b, _ = json.Marshal(state)
	os.WriteFile(f.ownerFile, b, 0600)
	if w := request("/settings/feishu/pairing", "test-key"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	s.apiKey = ""
	r := httptest.NewRequest("GET", "/settings/feishu", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("no-key admin allowed")
	}
}

func TestFeishuSettingsFirstSetup(t *testing.T) {
	f := fixtureSettings(t)
	os.WriteFile(f.envFile, []byte("EXTRA=keep\n"), 0600)
	if err := f.save(context.Background(), "cli_first", "first_secret"); err != nil {
		t.Fatal(err)
	}
	values, _ := f.values()
	if values["FEISHU_APP_ID"] != "cli_first" || values["EXTRA"] != "keep" {
		t.Fatal("first setup failed")
	}
}
