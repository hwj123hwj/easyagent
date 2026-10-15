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

// writeCreds 写一份最小凭据文件，供 LoadCredentials 读取。
func writeCreds(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	creds := `{"app_id":"cli_test","app_secret":"secret_test","user_open_id":"ou_test"}`
	if err := os.WriteFile(filepath.Join(dir, "feishu-credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFeishuBridgeRestartLifecycle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EA_HOME", dir)
	writeCreds(t, dir)

	var b feishuBridge
	firstCancelled := make(chan struct{})
	first := func(ctx context.Context, baseURL string) error {
		<-ctx.Done()
		close(firstCancelled)
		return ctx.Err()
	}
	status, err := b.restart("http://127.0.0.1:9", first)
	if err != nil {
		t.Fatal(err)
	}
	if status["running"] != true {
		t.Fatalf("启动后状态 = %v", status)
	}

	// 重启取消上一代，新代开始运行。
	secondCancelled := make(chan struct{})
	second := func(ctx context.Context, baseURL string) error {
		<-ctx.Done()
		close(secondCancelled)
		return ctx.Err()
	}
	if status, err = b.restart("http://127.0.0.1:9", second); err != nil {
		t.Fatal(err)
	}
	if status["running"] != true {
		t.Fatalf("重启后状态 = %v", status)
	}
	select {
	case <-firstCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("重启未取消上一代桥接")
	}
	if b.status()["running"] != true {
		t.Fatal("上一代退出不应影响新代状态")
	}
	// 收尾：取消新代并等它退出，避免泄漏 goroutine 影响后续断言。
	cancel := b.cancel
	if cancel != nil {
		cancel()
	}
	select {
	case <-secondCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("取消后桥接未退出")
	}
}

func TestFeishuBridgeRunnerErrorRecorded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EA_HOME", dir)
	writeCreds(t, dir)

	var b feishuBridge
	if _, err := b.restart("http://127.0.0.1:9", func(ctx context.Context, baseURL string) error {
		return errors.New("feishu 长连接失败")
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var status map[string]any
	for time.Now().Before(deadline) {
		if status = b.status(); status["running"] == false {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if status["running"] != false {
		t.Fatalf("runner 退出后应标记为停止: %v", status)
	}
	if status["error"] != "feishu 长连接失败" {
		t.Fatalf("runner 错误未记录: %v", status)
	}
	// 主动取消（stop 语义）不记录为错误。
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	time.Sleep(10 * time.Millisecond)
	if status = b.status(); status["error"] != "feishu 长连接失败" {
		t.Fatalf("取消不应覆盖错误记录: %v", status)
	}
}

func TestFeishuBridgeRestartWithoutCredentials(t *testing.T) {
	t.Setenv("EA_HOME", t.TempDir())
	var b feishuBridge
	runner := func(ctx context.Context, baseURL string) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := b.restart("http://127.0.0.1:9", runner); err == nil || !strings.Contains(err.Error(), "扫码绑定") {
		t.Fatalf("无凭据应同步报错: %v", err)
	}
	if b.status()["running"] != false {
		t.Fatal("启动失败不应进入运行状态")
	}
}

func TestFeishuBridgeRouteUnmanagedAndManaged(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EA_HOME", dir)
	writeCreds(t, dir)

	started := make(chan struct{})
	runner := func(ctx context.Context, baseURL string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	s := &Server{apiKey: "test-key", baseURL: "http://127.0.0.1:9", localBridgeRunner: runner}
	mux := http.NewServeMux()
	s.registerFeishuSettings(mux)
	handler := s.authMiddleware(mux)
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer test-key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	// 非托管：GET 状态带 bridge 字段，POST restart 启动注入的 runner。
	w := request("GET", "/settings/feishu")
	if w.Code != 200 {
		t.Fatalf("GET status = %d", w.Code)
	}
	var status map[string]any
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if _, ok := status["bridge"]; !ok {
		t.Fatalf("非托管状态缺少 bridge 字段: %v", status)
	}
	if w = request("POST", "/settings/feishu/bridge/restart"); w.Code != 200 {
		t.Fatalf("POST restart = %d: %s", w.Code, w.Body.String())
	}
	var restarted map[string]any
	if err := json.NewDecoder(w.Body).Decode(&restarted); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"managed", "local_mode", "app_id", "secret_configured"} {
		if restarted[key] != status[key] {
			t.Fatalf("restart lost %s: %v", key, restarted)
		}
	}
	bridge, ok := restarted["bridge"].(map[string]any)
	if !ok || bridge["running"] != true || bridge["started_at"] == "" {
		t.Fatalf("invalid bridge status: %v", restarted)
	}
	if strings.Contains(w.Body.String(), "secret_test") {
		t.Fatal("restart leaked secret")
	}
	defer s.localBridge.cancel()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("restart 未运行注入的 runner")
	}
	if s.localBridge.status()["running"] != true {
		t.Fatal("restart 后应处于运行中")
	}

	// 托管主机：restart 拒绝并指向「保存并重启桥接」。newFeishuSettings 在
	// 注册时读取环境变量，必须先设 env 再构造新的 Server 和路由。
	f := fixtureSettings(t)
	t.Setenv("EA_FEISHU_ENV_FILE", f.envFile)
	t.Setenv("FEISHU_OWNER_STATE_FILE", f.ownerFile)
	managed := &Server{apiKey: "test-key", baseURL: "http://127.0.0.1:9", localBridgeRunner: runner}
	managedMux := http.NewServeMux()
	managed.registerFeishuSettings(managedMux)
	managedHandler := managed.authMiddleware(managedMux)
	w = httptest.NewRecorder()
	managedReq := httptest.NewRequest("POST", "/settings/feishu/bridge/restart", nil)
	managedReq.Header.Set("Authorization", "Bearer test-key")
	managedHandler.ServeHTTP(w, managedReq)
	if w.Code != http.StatusConflict {
		t.Fatalf("managed restart = %d", w.Code)
	}
}

func TestNormalizeLocalBaseURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8080": "http://127.0.0.1:8080",
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:4001":   "http://127.0.0.1:4001",
		"[::1]:9":        "http://[::1]:9",
		"[::]:9":         "http://127.0.0.1:9",
	}
	for addr, want := range cases {
		if got := normalizeLocalBaseURL(addr); got != want {
			t.Fatalf("normalizeLocalBaseURL(%q) = %q, want %q", addr, got, want)
		}
	}
}
