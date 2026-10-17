package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/gorilla/websocket"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *memorySecrets) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[k], nil
}
func (s *memorySecrets) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[k] = v
	return nil
}
func (s *memorySecrets) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, k)
	return nil
}
func waitNative(t *testing.T, a *preview, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		a.liveTick()
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("native condition timed out: %s", a.Live.Error)
}
func TestNativeProjectionRecoveryAndConfirmations(t *testing.T) {
	s := &session{}
	s.reduce(envelope{Type: "snapshot", Seq: 10, Run: &runInfo{ID: "r", State: "running", Prompt: "hello"}, Events: []json.RawMessage{json.RawMessage(`{"type":"text_delta","text_delta":"first"}`)}})
	s.reduce(envelope{Type: "event", Seq: 11, RunID: "r", Event: &agentEvent{Type: "text_delta", Text: " second"}})
	s.reduce(envelope{Type: "event", Seq: 11, RunID: "r", Event: &agentEvent{Type: "text_delta", Text: " duplicate"}})
	s.reduce(envelope{Type: "snapshot", Seq: 9, Messages: []storedMessage{{Role: "user", Content: "stale"}}})
	if len(s.Messages) != 2 || s.Messages[1].Text != "first second" {
		t.Fatal("duplicate or stale projection applied")
	}
	s.reduce(envelope{Type: "confirmation", Seq: 12, RunID: "r", Confirmation: &confirmation{ID: "one"}})
	s.reduce(envelope{Type: "confirmation", Seq: 13, RunID: "r", Confirmation: &confirmation{ID: "two"}})
	s.reduce(envelope{Type: "confirmed", RunID: "r", ConfirmationID: "one"})
	if len(s.Confirmations) != 1 || s.Confirmations[0].ID != "two" {
		t.Fatal("confirm removed unrelated approvals")
	}
	s.reduce(envelope{Type: "snapshot", Reset: true, Seq: 1, Run: &runInfo{ID: "r", State: "completed", Prompt: "hello", UserEntry: "user", AssistantEntry: "answer"}, Messages: []storedMessage{{Role: "user", Content: "hello", EntryID: "user"}, {Role: "assistant", Content: "persisted", EntryID: "answer"}}})
	if s.Running || len(s.Messages) != 2 || s.Messages[1].Text != "persisted" || s.Messages[1].EntryID != "answer" {
		t.Fatal("restored receipt not authoritative")
	}
}
func TestNativeDuplicateCreationAndQueue(t *testing.T) {
	var creates, adds atomic.Int32
	block := make(chan struct{})
	var release sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/sessions":
			creates.Add(1)
			<-block
			fmt.Fprint(w, `{"id":"created"}`)
		case strings.HasSuffix(r.URL.Path, "/queue"):
			adds.Add(1)
			time.Sleep(30 * time.Millisecond)
			fmt.Fprint(w, `{"items":[],"revision":1}`)
		case strings.HasSuffix(r.URL.Path, "/run"):
			fmt.Fprint(w, `{"type":"snapshot","session_id":"created","messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/info"):
			fmt.Fprint(w, `{"workspace":"/tmp","model":"fixture"}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	defer release.Do(func() { close(block) })
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	a.Live.Connected = true
	a.createLiveSession("")
	a.createLiveSession("")
	release.Do(func() { close(block) })
	waitNative(t, a, func() bool { return a.current().RemoteID == "created" && !a.Live.Loading["create-session"] })
	if creates.Load() != 1 {
		t.Fatal("duplicate session creation")
	}
	s := a.current()
	s.Run = &runInfo{ID: "r", State: "running"}
	s.Running = true
	s.Draft = "queued prompt \n"
	a.submitLive()
	a.submitLive()
	waitNative(t, a, func() bool { return !a.Live.Loading["queue-add/created"] })
	if adds.Load() != 1 || s.Draft != "" {
		t.Fatal("queue duplicate or whitespace draft retained")
	}
}
func TestNativeFileHostGuardAndOrderedPersistence(t *testing.T) {
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	f := &nativeFile{Host: "other", SessionID: "same", Workspace: "/same", Path: "/same/a.go", Code: &editor.State{Buffer: editor.NewBuffer("secret")}}
	a.saveNativeFile(f)
	if a.Live.Error == "" {
		t.Fatal("cross-host write accepted")
	}
	a.Live.Error = ""
	a.exportNativeFile(f)
	if a.Live.Error == "" {
		t.Fatal("cross-host export accepted")
	}
	store := &nativeStore{dir: t.TempDir()}
	old := store.reserve("drafts.json")
	newer := store.reserve("drafts.json")
	if err := store.save("drafts.json", newer, map[string]string{"draft": "latest"}); err != nil {
		t.Fatal(err)
	}
	_ = store.save("drafts.json", old, map[string]string{"draft": "stale"})
	data, _ := os.ReadFile(filepath.Join(store.dir, "drafts.json"))
	if !strings.Contains(string(data), "latest") {
		t.Fatal("stale worker overwrote final save")
	}
}
func TestNativeStaleOwnedResourceReleased(t *testing.T) {
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	var released atomic.Bool
	a.postOwned(a.Live.epoch-1, func() { t.Error("stale result applied") }, func() { released.Store(true) })
	waitNative(t, a, func() bool { return released.Load() })
}
func TestNativeFeishuCredentialPatch(t *testing.T) {
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	path := filepath.Join(a.Live.Dir, "core-home", "feishu-credentials.json")
	old := map[string]any{"app_id": "app", "app_secret": "old", "user_open_id": "owner", "user_access_token": "token", "bot_open_id": "bot", "platform": "lark"}
	if err := atomicJSON(path, old); err != nil {
		t.Fatal(err)
	}
	a.saveLocalFeishu("app", "new")
	waitNative(t, a, func() bool { return !a.Live.Loading["feishu-save"] })
	data, _ := os.ReadFile(path)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	if result["user_open_id"] != "owner" || result["user_access_token"] != "token" || result["bot_open_id"] != "bot" || result["platform"] != "lark" || result["app_secret"] != "new" {
		t.Fatal("rotating secret dropped registration fields")
	}
	a.saveLocalFeishu("different-app", "new")
	waitNative(t, a, func() bool { return !a.Live.Loading["feishu-save"] })
	data, _ = os.ReadFile(path)
	_ = json.Unmarshal(data, &result)
	if result["app_id"] != "app" {
		t.Fatal("silent rebind replaced existing account")
	}
}
func TestNativeLiveSettingsLayouts(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{{"desktop", 1240, 820}, {"compact", 900, 680}} {
		for _, page := range settingsPages {
			a := newConnectedPreview(t.TempDir())
			a.current().Draft = "保留真实草稿"
			a.Live.Data["computer"] = map[string]any{"enabled": false, "provider": "macos:cua", "available": true, "granted": false, "approval_policy": "ask", "missing": []any{"屏幕录制", "辅助功能"}, "approved_apps": []any{}}
			a.openSettings(page.ID)
			tt := ui.NewTester(a.View, size.w, size.h)
			if !tt.HasText(page.Label) {
				t.Fatalf("missing %s", page.ID)
			}
			if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
				saveSnapshot(t, tt, filepath.Join(dir, "live-"+page.ID+"-"+size.name+".png"))
			}
			a.closeSettings()
			if a.current().Draft != "保留真实草稿" {
				t.Fatal("settings lost draft")
			}
			a.closeLive()
		}
	}
}

func nativeCoreFixture(t *testing.T) (*preview, *httptest.Server, string, *atomic.Int32) {
	t.Helper()
	if os.Getenv("EASYAGENT_NATIVE_INTEGRATION") != "1" {
		t.Skip("set EASYAGENT_NATIVE_INTEGRATION=1 after tools/prepare")
	}
	core, err := filepath.Abs("resources/darwin-arm64/easyagent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(core); err != nil {
		t.Fatal("build core with go run ./tools/prepare")
	}
	workspace := t.TempDir()
	calls := &atomic.Int32{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":[{"id":"fixture-model"},{"id":"fixture-other"}]}`)
			return
		}
		calls.Add(1)
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		messages := list(input["messages"])
		last := object(messages[len(messages)-1])
		text := displayText(last["content"])
		w.Header().Set("Content-Type", "text/event-stream")
		if str(last["role"]) != "tool" && strings.Contains(text, "fixture_ask") {
			args, _ := json.Marshal(map[string]any{"questions": []any{map[string]any{"question": "选择操作", "options": []any{map[string]any{"label": "只读"}, map[string]any{"label": "修改"}}}}})
			chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "fixture-ask", "type": "function", "function": map[string]any{"name": "ask_user_question", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}}
			encoded, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
			return
		}
		if str(last["role"]) != "tool" && strings.Contains(text, "fixture_write") {
			args, _ := json.Marshal(map[string]any{"path": filepath.Join(workspace, "created.txt"), "content": "native-real-tool"})
			chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "fixture-call", "type": "function", "function": map[string]any{"name": "write", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}}
			encoded, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"真实核心首段 \"}}]}\n\n")
		w.(http.Flusher).Flush()
		if strings.Contains(text, "fixture_hold") {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"完成\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Setenv("EASYAGENT_NATIVE_CORE", core)
	t.Setenv("EA_PROVIDER", "openai")
	t.Setenv("EA_BASE_URL", provider.URL)
	t.Setenv("EA_API_KEY", "fixture-key")
	t.Setenv("EA_MODEL", "fixture-model")
	t.Setenv("EA_WORKSPACE", workspace)
	t.Setenv("EA_SKILL_MARKET", "0")
	t.Setenv("EA_AUTO_APPROVE", "false")
	t.Setenv("EA_ENABLE_BASH", "false")
	t.Setenv("EA_KB_REPO_PATH", t.TempDir())
	t.Setenv("EA_ENV_FILE", filepath.Join(t.TempDir(), "absent.env"))
	a := newConnectedPreview(t.TempDir())
	a.Live.Secrets = &memorySecrets{}
	a.connectLive()
	t.Cleanup(func() { a.closeLive(); provider.Close() })
	waitNative(t, a, func() bool { return a.Live.Connected && a.Live.Client != nil })
	return a, provider, workspace, calls
}
func TestNativeCoreConversationSettingsAndRestart(t *testing.T) {
	a, provider, workspace, calls := nativeCoreFixture(t)
	s := a.current()
	s.Draft = "hello real core"
	a.submitLive()
	waitNative(t, a, func() bool { s = a.current(); return s.Run != nil && !s.Running && len(s.Messages) > 1 })
	if s.Draft != "" || s.Messages[len(s.Messages)-1].Text != "真实核心首段 完成" {
		t.Fatalf("bad actual core projection: %+v", s.Messages)
	}
	id := s.RemoteID
	a.patchLiveSession(map[string]any{"title": "持久化会话", "pinned": true})
	waitNative(t, a, func() bool { return a.current().Title == "持久化会话" })
	a.switchLiveModel("fixture-other")
	waitNative(t, a, func() bool { return s.Model == "fixture-other" })
	a.operation("test-computer", "POST", "/computer/settings", map[string]any{"enabled": false, "approval_policy": "ask"}, func(v any) { a.Live.Data["computer"] = v })
	waitNative(t, a, func() bool { return a.Live.Data["computer"] != nil })
	a.operation("test-mcp", "PUT", "/mcp/servers/native-disabled", map[string]any{"command": "/usr/bin/false", "enabled": false, "trust": false}, func(v any) { a.Live.Data["mcp"] = v })
	waitNative(t, a, func() bool { return len(list(object(a.Live.Data["mcp"])["servers"])) == 1 })
	file := filepath.Join(workspace, "attachment.txt")
	_ = os.WriteFile(file, []byte("fixture attachment"), 0600)
	att, err := a.Live.Client.upload(context.Background(), id, file)
	if err != nil || str(att["id"]) == "" {
		t.Fatalf("attachment: %v %#v", err, att)
	}
	a.Settings.Mode = "在桌面客户端配置"
	a.Settings.Provider = "OpenAI 兼容服务 / 网关"
	a.Settings.BaseURL = provider.URL
	a.Settings.Model = "fixture-model"
	a.Settings.APIKey = "fixture-secret"
	a.applyLiveModel()
	waitNative(t, a, func() bool { return a.Live.Config.Model.Mode == "override" && a.Live.Connected && !a.Live.Connecting })
	metadata, _ := os.ReadFile(filepath.Join(a.Live.Dir, "connections.json"))
	if strings.Contains(string(metadata), "fixture-secret") {
		t.Fatal("model secret leaked to JSON")
	}
	a.refreshLiveSessions(id)
	waitNative(t, a, func() bool { return a.current().RemoteID == id && len(a.current().Messages) >= 2 })
	s = a.current()
	if s.Title != "持久化会话" || !s.Pinned {
		t.Fatal("session metadata lost at core restart")
	}
	if calls.Load() != 1 {
		t.Fatalf("model discovery unexpectedly invoked inference: %d", calls.Load())
	}
	// The reconnection must restart a child that exited independently.
	a.Live.Core.mu.Lock()
	cmd, done := a.Live.Core.cmd, a.Live.Core.done
	a.Live.Core.mu.Unlock()
	_ = cmd.Process.Kill()
	<-done
	a.connectLive()
	waitNative(t, a, func() bool { return a.Live.Connected && !a.Live.Connecting })
	if a.Live.Core.cmd == cmd {
		t.Fatal("dead cached child reused")
	}
}
func TestNativeCoreApprovalCancelAndTerminal(t *testing.T) {
	a, _, workspace, _ := nativeCoreFixture(t)
	a.createLiveSession("")
	waitNative(t, a, func() bool { return a.current().RemoteID != "" })
	s := a.current()
	s.Draft = "fixture_write"
	a.submitLive()
	waitNative(t, a, func() bool { return len(s.Confirmations) > 0 })
	if _, err := os.Stat(filepath.Join(workspace, "created.txt")); !os.IsNotExist(err) {
		t.Fatal("tool ran before approval")
	}
	a.confirmLive(true)
	waitNative(t, a, func() bool { return s.Run != nil && !s.Running })
	data, err := os.ReadFile(filepath.Join(workspace, "created.txt"))
	if err != nil || string(data) != "native-real-tool" {
		t.Fatalf("actual approved tool failed: %v", err)
	}
	s.Draft = "fixture_hold"
	a.submitLive()
	waitNative(t, a, func() bool { return s.Running && len(s.Messages) > 0 && s.Messages[len(s.Messages)-1].Text != "" })
	a.cancelLive()
	waitNative(t, a, func() bool { return s.Run != nil && s.Run.State == "cancelled" })
	u := strings.Replace(a.Live.Client.URL, "http", "ws", 1) + "/terminal" + query(map[string]string{"session_id": s.RemoteID})
	conn, _, err := websocket.DefaultDialer.Dial(u, http.Header{"Authorization": []string{"Bearer " + a.Live.Client.Token}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	term := &remoteTerminal{conn: conn}
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	if _, err = term.Write([]byte("printf '%s%s\\n' NATIVE_ TERMINAL_OK\r")); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	buf := make([]byte, 4096)
	for !strings.Contains(output.String(), "NATIVE_TERMINAL_OK") {
		n, err := term.Read(buf)
		output.Write(buf[:n])
		if err != nil {
			t.Fatalf("terminal read: %v", err)
		}
	}
}

func TestNativeDelayedCommandKeepsSessionAndDraft(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var gotSession, gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/command") {
			close(entered)
			<-release
			fmt.Fprint(w, `{"should_query":true,"query_prompt":"generated query"}`)
		} else if strings.HasSuffix(r.URL.Path, "/queue") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotSession, gotPrompt = r.URL.Path, str(body["prompt"])
			fmt.Fprint(w, `{"items":[],"revision":1}`)
		} else {
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	a.Live.Connected = true
	first := &session{ID: 1, RemoteID: "A", Running: true, Draft: "/demo create plan"}
	second := &session{ID: 2, RemoteID: "B", Draft: "B unsent draft"}
	a.Sessions = []*session{first, second}
	a.Active = 1
	a.submitLive()
	<-entered
	if first.Draft != "/demo create plan" {
		t.Fatal("command cleared before acknowledgement")
	}
	first.Draft = "new A draft"
	a.Active = 2
	once.Do(func() { close(release) })
	waitNative(t, a, func() bool { return first.Queue.Revision == 1 })
	if gotSession != "/sessions/A/queue" || gotPrompt != "generated query" || first.Draft != "new A draft" || second.Draft != "B unsent draft" {
		t.Fatalf("wrong session or overwritten draft: %q %q %q %q", gotSession, gotPrompt, first.Draft, second.Draft)
	}
}

func TestNativeModelApplicationBlocksNewWork(t *testing.T) {
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	a.Live.Connected = true
	a.Live.Loading["model-save"] = true
	s := a.current()
	s.Draft = "preserve me"
	a.submitLive()
	a.createLiveSession("")
	a.queueAction(s, "resume", "", "")
	if s.Draft != "preserve me" || s.Sending || a.Live.Loading["create-session"] || a.Live.Error == "" {
		t.Fatal("new work admitted during core restart")
	}
}

func TestNativePermissionResponseIsSessionScoped(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; fmt.Fprint(w, `{}`) }))
	defer srv.Close()
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	first := &session{ID: 1, RemoteID: "A"}
	second := &session{ID: 2, RemoteID: "B", AccessMode: "ask"}
	a.Sessions = []*session{first, second}
	a.Active = 1
	a.switchLivePermission("完全权限")
	<-entered
	a.Active = 2
	once.Do(func() { close(release) })
	waitNative(t, a, func() bool { return !a.Live.Loading["permission/A"] })
	if first.AccessMode != "full" || a.livePermission() != "每次询问" {
		t.Fatal("permission leaked across sessions")
	}
	a.Active = 1
	if a.livePermission() != "完全权限" {
		t.Fatal("session permission lost")
	}
}

func TestNativeTerminalLateFailureDoesNotCloseReplacement(t *testing.T) {
	firstEntered, secondEntered := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	var firstOnce, secondOnce sync.Once
	defer firstOnce.Do(func() { close(firstRelease) })
	defer secondOnce.Do(func() { close(secondRelease) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session_id") == "A" {
			close(firstEntered)
			<-firstRelease
		} else {
			close(secondEntered)
			<-secondRelease
		}
		http.Error(w, "fixture denied", http.StatusForbidden)
	}))
	defer srv.Close()
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	a.Sessions = []*session{{ID: 1, RemoteID: "A", Workspace: "/a"}, {ID: 2, RemoteID: "B", Workspace: "/b"}}
	a.Active = 1
	a.toggleTerminal()
	<-firstEntered
	a.Live.Workbench.close()
	a.Active = 2
	a.toggleTerminal()
	<-secondEntered
	firstOnce.Do(func() { close(firstRelease) })
	// Wait for and apply A's result while B's independent dial is still pending.
	select {
	case r := <-a.Live.results:
		r.apply()
	case <-time.After(2 * time.Second):
		t.Fatal("missing stale terminal result")
	}
	w := &a.Live.Workbench
	if !w.TerminalOpen || !w.TerminalStarting || !w.matchesTerminal("local", "B") || a.Live.Error != "" {
		t.Fatal("stale terminal response modified replacement")
	}
	secondOnce.Do(func() { close(secondRelease) })
	waitNative(t, a, func() bool { return !w.TerminalStarting })
}

func TestNativeEditedEndpointGetsSeparateIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sessions" {
			fmt.Fprint(w, `[]`)
		} else {
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	a := newConnectedPreview(t.TempDir())
	defer a.closeLive()
	a.Live.Secrets = &memorySecrets{}
	a.Live.Config.Hosts = []hostConfig{{ID: "old", Name: "Old", URL: "http://127.0.0.1:1", Workspace: "/old-server"}}
	a.Live.Config.ActiveHost = "old"
	a.syncSettingsConfig()
	a.Sessions = []*session{{ID: 1, RemoteID: "same", Draft: "old server draft"}}
	a.Settings.EditHost = "old"
	a.Settings.HostName = "New"
	a.Settings.HostURL = srv.URL
	a.saveLiveHost()
	waitNative(t, a, func() bool { return a.Live.Client != nil && a.Live.Client.URL == srv.URL })
	if len(a.Live.Config.Hosts) != 2 || a.Live.Config.ActiveHost == "old" || a.Live.workspace() != "" || a.Live.Drafts["old/same"] != "old server draft" {
		t.Fatal("endpoint change reused previous server identity or workspace")
	}
	if a.Live.Config.Hosts[0].URL != "http://127.0.0.1:1" {
		t.Fatal("old connection overwritten")
	}
}

func TestNativeOldInfoCannotUndoModelOrPermissionChange(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(fmt.Sprintf("read_during_mutation_%t", during), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			mutationRelease := make(chan struct{})
			var mutationOnce sync.Once
			defer mutationOnce.Do(func() { close(mutationRelease) })
			if !during {
				mutationOnce.Do(func() { close(mutationRelease) })
			}
			var once sync.Once
			defer once.Do(func() { close(release) })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "POST" {
					<-mutationRelease
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					close(entered)
					<-release
					fmt.Fprint(w, `{"model":"old","access_mode":"ask","workspace":"/tmp"}`)
				case strings.HasSuffix(r.URL.Path, "/model"):
					fmt.Fprint(w, `{"model":"new"}`)
				case strings.HasSuffix(r.URL.Path, "/run"):
					fmt.Fprint(w, `{"type":"snapshot","session_id":"A","messages":[]}`)
				default:
					fmt.Fprint(w, `{}`)
				}
			}))
			defer srv.Close()
			a := newConnectedPreview(t.TempDir())
			defer a.closeLive()
			a.Live.Client, _ = newBackendClient(srv.URL, "")
			s := a.current()
			s.RemoteID = "A"
			s.Model = "old"
			if !during {
				a.loadSession(s)
				<-entered
			}
			a.switchLivePermission("完全权限")
			a.switchLiveModel("new")
			if during {
				a.loadSession(s)
				<-entered
				mutationOnce.Do(func() { close(mutationRelease) })
			}
			waitNative(t, a, func() bool { return s.Model == "new" && s.AccessMode == "full" })
			once.Do(func() { close(release) })
			// The outstanding /info request is the only blocked response. Apply it after
			// both mutations and verify it cannot rewind the committed toolbar state.
			select {
			case r := <-a.Live.results:
				r.apply()
			case <-time.After(2 * time.Second):
				t.Fatal("missing old info result")
			}
			a.liveTick()
			if s.Model != "new" || s.AccessMode != "full" {
				t.Fatal("old /info undid a committed session setting")
			}

		})
	}
}

func TestNativeModelDiscoveryAcceptsVersionedBase(t *testing.T) {
	for _, provider := range []string{"OpenAI 兼容服务 / 网关", "Anthropic"} {
		t.Run(provider, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					http.NotFound(w, r)
					return
				}
				if provider == "Anthropic" && r.Header.Get("x-api-key") != "fixture-key" {
					http.Error(w, "missing key", 401)
					return
				}
				fmt.Fprint(w, `{"data":[{"id":"fixture-discovered"}]}`)
			}))
			defer srv.Close()
			a := newConnectedPreview(t.TempDir())
			defer a.closeLive()
			a.Settings.Mode = "在桌面客户端配置"
			a.Settings.Provider = provider
			a.Settings.BaseURL = srv.URL + "/v1"
			a.Settings.Model = "fixture-discovered"
			a.Settings.APIKey = "fixture-key"
			a.checkLiveModel()
			waitNative(t, a, func() bool { return !a.Live.Loading["model-check"] })
			if a.Live.Error != "" || len(list(object(a.Live.Data["model-check"])["data"])) != 1 {
				t.Fatal("versioned model base failed:", a.Live.Error)
			}
		})
	}
}

func TestNativeCoreQuestionAnswerRoundTrip(t *testing.T) {
	a, _, _, _ := nativeCoreFixture(t)
	a.current().Draft = "fixture_ask"
	a.submitLive()
	waitNative(t, a, func() bool { return len(a.current().Confirmations) > 0 })
	s := a.current()
	question := s.Confirmations[0]
	if len(parseNativeQuestions(question.Args)) != 1 {
		t.Fatalf("core question not projected: %+v", question)
	}
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "只读")
	click(t, tt, "提交回答")
	waitNative(t, a, func() bool { return s.Run != nil && !s.Run.active() && len(s.Confirmations) == 0 })
	found := false
	for _, m := range s.Messages {
		if m.Tool == "ask_user_question" && strings.Contains(m.Result, "只读") {
			found = true
		}
	}
	if !found {
		t.Fatal("real core did not receive selected answer", copyNativeConversation(s))
	}
}
