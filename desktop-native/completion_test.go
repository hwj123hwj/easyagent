package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestNativeConversationSearchUnicodeAndToolResults(t *testing.T) {
	messages := []*message{{Text: "İSTANBUL 中文你好中文"}, {Kind: "tool", Tool: "bash", Args: map[string]any{"command": "go test"}, Result: "检查通过"}}
	for _, q := range []string{"istanbul", "中文", "检查通过", "go test"} {
		matches := conversationMatches(messages, q)
		if len(matches) == 0 {
			t.Fatalf("missing %q", q)
		}
		for _, m := range matches {
			if !utf8.ValidString(m.Snippet) {
				t.Fatalf("split Unicode %q", m.Snippet)
			}
		}
	}
	copied := copyNativeConversation(&session{Messages: messages})
	if !strings.Contains(copied, "go test") || !strings.Contains(copied, "检查通过") {
		t.Fatal("copy omitted tool inputs/results")
	}
	a := alignmentPreview(t)
	a.current().Messages = messages
	tt := ui.NewTester(a.View, 1240, 820)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	click(t, tt, "搜索当前会话内容")
	tt.Type("中文")
	tt.Frame()
	if !a.current().FindOpen || len(conversationMatches(messages, a.current().FindQuery)) != 2 {
		t.Fatal("CmdF search unavailable")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.current().FindIndex != 1 {
		t.Fatal("next match failed")
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	if a.current().FindIndex != 0 {
		t.Fatal("previous match failed")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.current().FindOpen {
		t.Fatal("Escape did not dismiss search")
	}
}
func TestNativeQuestionSchemaAndChoices(t *testing.T) {
	args := map[string]any{"questions": []any{map[string]any{"question": "选择模式", "options": []any{map[string]any{"label": "只读", "preview": "**只检查**"}, map[string]any{"label": "修改"}}}}}
	questions := parseNativeQuestions(args)
	if len(questions) != 1 || nativeAnswersComplete(questions, []questionAnswer{{Selected: []string{"只读", "修改"}}}) || nativeAnswersComplete(questions, []questionAnswer{{Selected: []string{"伪造"}}}) || nativeAnswersComplete(questions, []questionAnswer{{Text: strings.Repeat("字", 4001)}}) {
		t.Fatal("answer validation failed")
	}
	a := alignmentPreview(t)
	s := a.current()
	s.Confirmations = []confirmation{{ID: "ask", ToolName: "ask_user_question", Args: args}}
	s.Pending = true
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "只读")
	if !tt.HasText("只检查") {
		t.Fatal("selected option preview missing")
	}
	click(t, tt, "其他回答")
	tt.Frame()
	if _, ok := tt.Find("其他回答 1"); !ok {
		t.Fatal("other text input missing")
	}
	if _, ok := tt.Find("跳过问题"); !ok {
		t.Fatal("skip unavailable")
	}
}
func TestNativePromptReferencesPersistenceAndOwnership(t *testing.T) {
	a := alignmentPreview(t)
	s := a.current()
	ref := fileReference{Path: s.Workspace + "/main.go", Workspace: s.Workspace, Host: "local"}
	a.addFileReference(s, ref)
	a.addFileReference(s, ref)
	a.addFileReference(s, fileReference{Path: "/other", Workspace: "/other", Host: "remote"})
	if len(s.FileReferences) != 1 {
		t.Fatal("duplicate/foreign reference accepted")
	}
	s.Attachments = []map[string]any{{"id": "attachment-one", "name": "image.png"}}
	raw := a.captureDraftInputs()
	version := a.Live.Store.reserve("draft-inputs.json")
	if err := a.Live.Store.save("draft-inputs.json", version, raw); err != nil {
		t.Fatal(err)
	}
	a.Live.DraftInputs = nil
	a.loadDraftInputs()
	s.Attachments = nil
	s.FileReferences = nil
	a.restoreDraftInputs(s)
	encoded, _ := json.Marshal(promptInputMap(s, s.Attachments))
	if !strings.Contains(string(encoded), "attachment-one") || !strings.Contains(string(encoded), "main.go") || strings.Contains(string(encoded), "Host") {
		t.Fatal("context protocol/persistence failed", string(encoded))
	}
	a.Live.Loading["queue-add/"+s.RemoteID] = true
	a.addFileReference(s, fileReference{Path: s.Workspace + "/view.go", Workspace: s.Workspace, Host: "local"})
	if len(s.FileReferences) != 1 {
		t.Fatal("in-flight queue draft context changed")
	}
}
func TestNativeRunFileAggregationAndStats(t *testing.T) {
	file := func(before, after string) any {
		return map[string]any{"path": "main.go", "before": before, "after": after, "kind": "modified", "attribution": "file_tool"}
	}
	runs := []map[string]any{{"run_id": "one", "workspace": "/w", "complete": true, "files": []any{file("", "a\n")}}, {"run_id": "two", "workspace": "/w", "complete": true, "files": []any{file("a\n", "b\n")}}}
	details := map[string]any{"one": runs[0], "two": runs[1]}
	files := aggregateRunFiles(runs, details)
	if len(files) != 1 || files[0].Before != "" || files[0].After != "b\n" || files[0].Partial {
		t.Fatal("continuous attribution aggregate failed", files)
	}
	added, removed, known := diffLineCounts("a\nb\nc\n", "a\nd\ne\nc\n")
	if !known || added != 2 || removed != 1 {
		t.Fatal(added, removed, known)
	}
	details["two"] = map[string]any{"files": []any{file("external\n", "b\n")}}
	if !aggregateRunFiles(runs, details)[0].Partial {
		t.Fatal("external intervening changes miscounted")
	}
	filtered := workspaceRuns([]any{runs[0], map[string]any{"workspace": "/other", "complete": true, "files": []any{file("a", "b")}}}, "/w")
	if len(filtered) != 1 {
		t.Fatal("cross-workspace result displayed")
	}
}

type fixtureVoice struct{ data []byte }

func (v *fixtureVoice) Stop() ([]byte, error) { return v.data, nil }
func TestNativeVoiceTranscriptReturnsToOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/asr/transcribe" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"text":"转写内容"}`)
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.Live.Client, _ = newBackendClient(srv.URL, "test")
	a.Live.Connected = true
	origin := a.current()
	a.Sessions = append(a.Sessions, &session{ID: 8, RemoteID: "other"})
	a.Live.Voice = &fixtureVoice{pcmWAV([]byte{0, 0})}
	a.Live.VoiceOwner = origin
	a.Active = 8
	a.startVoice()
	waitNative(t, a, func() bool { return !a.Live.Loading["voice"] })
	if origin.Draft != "转写内容" || a.current().Draft != "" {
		t.Fatal("transcript contaminated active session")
	}
}
func TestNativeQRAndPairingLifecycle(t *testing.T) {
	now := time.Now()
	valid := map[string]any{"command": "/pair abc_12-3", "expires": now.Add(time.Minute).Format(time.RFC3339)}
	if !validFeishuPairing(valid, now) || validFeishuPairing(valid, now.Add(2*time.Minute)) || validFeishuPairing(map[string]any{"command": "/pair a\nextra", "expires": valid["expires"]}, now) {
		t.Fatal("pairing expiry/format check failed")
	}
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"authorized":true}`)
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.Live.Client, _ = newBackendClient(srv.URL, "test")
	a.Settings.Open = true
	a.Settings.Tab = "feishu"
	a.Live.QRCode = "old"
	a.Live.QRExpires = now.Add(time.Minute)
	a.pollQR()
	a.closeSettings()
	close(release)
	waitNative(t, a, func() bool { return !a.Live.Loading["qr-poll"] })
	if a.Live.QRAuthorized || a.Live.QRCode != "" {
		t.Fatal("dismissed QR authorized after late response")
	}
}
func TestNativeMarkdownFilesAndImages(t *testing.T) {
	for _, tc := range []struct{ workspace, base, href, want string }{{"/w", "/w/docs/a.md", "../main.go", "/w/main.go"}, {`C:\w`, "", "src/main.go", "C:/w/src/main.go"}, {"/w", "", "/tmp/file.go", "/tmp/file.go"}} {
		got, err := resolveMarkdownFile(tc.workspace, tc.base, tc.href)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	for _, href := range []string{"//example.com/file", "javascript:alert(1)", "a\x00b"} {
		if _, err := resolveMarkdownFile("/w", "", href); err == nil {
			t.Fatal("invalid href accepted", href)
		}
	}
	converted := markdownFileReferences("阅读 `/w/main.go`\n```go\n`/not-a-link`\n```")
	if !strings.Contains(converted, "ea-file:") || strings.Count(converted, "ea-file:") != 1 {
		t.Fatal("file reference/fence transform failed", converted)
	}
	if _, err := decodeNativeImage([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`), "preview.svg"); err != nil {
		t.Fatal(err)
	}
}

func populatedPreview(t *testing.T) *preview {
	a := alignmentPreview(t)
	l, s := a.Live, a.current()
	s.Application = "coding"
	s.UpdatedAt = time.Now().Format(time.RFC3339)
	s.Draft = "核对这些文件，保留当前设置。"
	s.FileReferences = []fileReference{{Path: s.Workspace + "/main.go", Workspace: s.Workspace, Host: "local"}}
	s.Messages = []*message{{User: true, Text: "请检查界面和交互。"}, {Kind: "tool", Tool: "read", CallID: "read", Args: map[string]any{"path": s.Workspace + "/main.go"}, Result: "package main\n\nfunc main() {}", Status: "完成", DurationMS: 56}, {Kind: "tool", Tool: "bash", CallID: "test", Args: map[string]any{"command": "go test ./..."}, Result: "ok package\nExit code: 0", Status: "完成", DurationMS: 1200}, {Text: "已完成检查。\n\n## 验证结果\n\n- 会话与文件按项目隔离。\n- 设置保存后生效。\n\n```go\nui.Column(c).Gap(12)\n```"}}
	s.Usage = map[string]any{"context_window": 128000, "estimated_tokens": 24000, "messages": 12000, "system": 8000, "tools": 4000, "window_known": true}
	l.Data["usage"] = map[string]any{"total": map[string]any{"run_count": 42, "input_tokens": 210000, "output_tokens": 40000}, "sessions": []any{map[string]any{"session_id": s.RemoteID, "run_count": 42, "input_tokens": 210000, "output_tokens": 40000}}}
	l.Data["mcp"] = map[string]any{"project_trusted": true, "servers": []any{map[string]any{"name": "filesystem", "scope": "user", "transport": "stdio", "state": "connected", "enabled": true, "trusted": true, "tool_count": 2, "tools": []any{map[string]any{"name": "read_file", "enabled": true, "description": "读取工作区文件"}, map[string]any{"name": "list_directory", "enabled": false, "description": "列出目录中的文件"}}}}}
	l.Data["feishu"] = map[string]any{"managed": true, "app_id": "cli_fixture", "secret_configured": true, "paired": false, "services": map[string]any{"core": map[string]any{"state": "active", "autostart": "enabled"}, "bridge": map[string]any{"state": "failed", "autostart": "disabled"}}, "linger": "yes"}
	l.Data["computer"] = map[string]any{"enabled": true, "available": true, "granted": false, "provider": "macos:cua", "approval_policy": "ask", "missing": []any{"屏幕录制", "辅助功能"}}
	l.Data["capabilities"] = map[string]any{"workspace": s.Workspace, "skills": []any{map[string]any{"name": "code-review", "description": "检查代码、测试与实现边界", "scope": "project", "model_invocation": true, "source": "/demo/easyagent/.agents/skills/review/SKILL.md"}}, "workflow_entry": "create_workflow", "tools": []any{"create_workflow", "get_workflow"}, "workflows": []any{map[string]any{"id": "workflow-one", "name": "验收流程", "status": "completed", "result": "检查通过"}}}
	l.Data["market"] = map[string]any{"skills": []any{map[string]any{"id": "review", "name": "code-review", "display_name": "代码审查", "description": "检查功能、边界与测试，整理需要修复的问题。", "version": "1.0.0", "featured": true, "download_count": 1000}, map[string]any{"id": "ui", "name": "native-ui", "display_name": "原生界面设计", "description": "依据现有设计系统补齐页面与交互。", "version": "2.0.0"}}, "total": 2}
	l.Data["profile"] = map[string]any{"total_facts": 1, "summary": "优先 Go，保留已有工作。", "categories": []any{map[string]any{"name": "preferences", "label": "个人偏好", "facts": []any{map[string]any{"key": "语言", "value": "中文", "source": "conversation", "updated": "今天"}}}}}
	l.Data["kb"] = map[string]any{"entries": []any{map[string]any{"path": "guide.md", "rel_path": "guide.md", "title": "界面验收指南", "summary": "逐项验证布局、数据和交互。", "category": "开发", "modified": "今天"}}}
	l.Data["kb-stats"] = map[string]any{"total_entries": 1, "categories": 1, "tags": 2}
	l.Data["kb-categories"] = map[string]any{"categories": []any{map[string]any{"name": "开发", "count": 1}}}
	l.Data["kb-tags"] = map[string]any{"tags": []any{map[string]any{"name": "Go", "count": 1}}}
	a.bindWorkbench()
	for key, value := range l.Data {
		encoded, _ := json.Marshal(value)
		var normalized any
		json.Unmarshal(encoded, &normalized)
		l.Data[key] = normalized
	}
	encoded, _ := json.Marshal(s.Usage)
	json.Unmarshal(encoded, &s.Usage)
	s.Plan = []nativePlanEntry{{Content: "核对原版入口与布局", Status: "completed"}, {Content: "验证保存、队列和授权状态", Status: "in_progress"}, {Content: "打包并检查实际应用", Status: "pending"}}
	l.Data["context"] = map[string]any{"system": "临时测试系统提示词", "usage": s.Usage, "messages": []any{map[string]any{"role": "user", "message": map[string]any{"content": "检查界面"}}}, "tools": []any{map[string]any{"name": "read", "description": "读取文件"}}}
	return a
}
func TestNativePopulatedParityScreens(t *testing.T) {
	dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS")
	if dir == "" {
		t.Skip("visual QA snapshots")
	}
	for _, size := range []struct{ w, h int }{{1240, 820}, {900, 680}} {
		for _, mode := range []string{"chat", "files", "review", "file-summary", "tools", "find", "queue", "question", "image", "models", "references", "recovery", "tasks", "plan", "profile", "kb", "kb-tags", "kb-health", "context", "general", "appearance", "models-settings", "usage", "skillMarket", "experiments", "connections", "mcp", "capabilities", "feishu"} {
			t.Run(fmt.Sprintf("%s-%d", mode, size.w), func(t *testing.T) {
				a := populatedPreview(t)
				s, l := a.current(), a.Live
				switch mode {
				case "files":
					a.Workbench = true
					l.Workbench.Tab = "files"
					a.bindFileScope()
					w := &l.Workbench
					path := s.Workspace + "/README.md"
					w.Directories[s.Workspace] = &nativeDirectory{Open: true, Entries: []any{map[string]any{"name": "README.md", "path": path, "isDir": false}, map[string]any{"name": "main.go", "path": s.Workspace + "/main.go", "isDir": false}}}
					w.ActiveFile = path
					w.Tabs = []string{path}
					w.Files[path] = &nativeFile{Path: path, SessionID: s.RemoteID, Host: "local", Workspace: s.Workspace, Kind: "text", Original: "# 项目说明\n\n检查 **Markdown**、代码和文件引用。", Preview: true, Code: &editor.State{Buffer: editor.NewBuffer("# 项目说明\n\n检查 **Markdown**、代码和文件引用。"), ReadOnly: true}}
				case "review", "file-summary":
					change := map[string]any{"path": s.Workspace + "/main.go", "kind": "modified", "before": "package main\n", "after": "package main\n\nfunc main() {}\n", "attribution": "file_tool", "can_undo": true, "version": "fixture"}
					run := map[string]any{"run_id": "fixture-run", "workspace": s.Workspace, "complete": true, "files": []any{change}}
					l.Data["run-results/"+s.RemoteID] = []any{run}
					l.Data["run-result-detail/"+s.RemoteID+"/fixture-run"] = run
					if mode == "review" {
						a.Workbench = true
						l.Workbench.Tab = "review"
						l.Data["review"] = []any{run}
						l.Data["run-files"] = run
						l.Workbench.SelectedRun = "fixture-run"
						a.selectReviewFile(change)
					}
				case "tools":
					s.Density = "verbose"
				case "find":
					s.FindOpen = true
					s.FindQuery = "test"
					s.FindJump = true
				case "queue":
					s.Queue = messageQueue{Items: []queueItem{{ID: "one", Prompt: "继续完成余下检查", Inputs: map[string]any{"files": []any{map[string]any{"path": "main.go"}}}}, {ID: "two", Prompt: "整理验收结果"}}}
				case "question":
					s.Confirmations = []confirmation{{ID: "ask", ToolName: "ask_user_question", Args: map[string]any{"questions": []any{map[string]any{"question": "如何处理剩余问题？", "header": "执行方式", "options": []any{map[string]any{"label": "继续修复", "description": "保存现有设置和工作区。"}, map[string]any{"label": "先检查", "description": "列出可复现的问题。"}}}}}}}
					s.Pending = true
				case "image":
					im := image.NewRGBA(image.Rect(0, 0, 600, 400))
					for y := 0; y < 400; y++ {
						for x := 0; x < 600; x++ {
							im.Set(x, y, color.RGBA{34, 110, 110, 255})
						}
					}
					l.ImagePreview = ui.NewBitmap(im)
					l.ImageTitle = "图片预览"
					l.ImageZoom = 1
				case "references":
					s.Draft = "检查 @mai"
					l.Data["mention/"+s.RemoteID] = []any{"main.go", "src/main_test.go"}
				case "recovery":
					s.Run = &runInfo{ID: "failed", State: "failed", Prompt: "检查", Error: "HTTP 429: rate limit exceeded"}
				case "models":
					a.ModelMenu = true
					a.ModelSearch = ""
					l.Models = []string{"coding", "glm-5.3-flash"}
				case "tasks", "plan", "profile", "kb", "kb-tags", "kb-health", "context":
					a.Workbench = true
					l.Workbench.Tab = mode
					if strings.HasPrefix(mode, "kb-") {
						l.Workbench.Tab = "kb"
						l.Workbench.KBView = strings.TrimPrefix(mode, "kb-")
					}
				default:
					if mode != "chat" {
						page := mode
						if page == "models-settings" {
							page = "models"
						}
						a.openSettings(page)
					}
				}
				tt := ui.NewTester(a.View, size.w, size.h)
				tt.Frame()
				if mode == "recovery" || mode == "file-summary" {
					s.Scroll.Y = s.Scroll.MaxY
					tt.Frame()
				}
				saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("parity-%s-%d.png", mode, size.w)))
				if mode == "image" {
					closeBox, ok := tt.Find("关闭图片预览")
					if !ok || closeBox.X < 0 || closeBox.X+closeBox.W > float32(size.w) || closeBox.Y+closeBox.H > float32(size.h) {
						t.Fatal("image controls outside window", closeBox)
					}
				}
			})
		}
	}
}

func TestNativeActionIconsExist(t *testing.T) {
	paths, _ := filepath.Glob("*.go")
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if fn.Name != "icon" && fn.Name != "iconButton" && fn.Name != "flatButton" && fn.Name != "menuOption" {
				return true
			}
			arg, ok := call.Args[1].(*ast.BasicLit)
			if !ok || arg.Kind != token.STRING {
				return true
			}
			name, _ := strconv.Unquote(arg.Value)
			if name != "" && icons[name] == nil {
				t.Errorf("%s: missing action icon %q", path, name)
			}
			return true
		})
	}
	for _, page := range settingsPages {
		if icons[page.Icon] == nil {
			t.Errorf("missing settings icon %q", page.Icon)
		}
	}
	for _, tab := range workbenchTabs {
		if icons[tab.icon] == nil {
			t.Errorf("missing workbench icon %q", tab.icon)
		}
	}
}

func TestNativeInlineQueueEditRetainsFailedDraft(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/layout/queue" {
			t.Errorf("wrong queue owner %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if str(body["id"]) != "one" || str(body["prompt"]) != "修正的待发消息" {
			t.Errorf("lost edited draft %+v", body)
		}
		if calls.Add(1) == 1 {
			http.Error(w, "fixture temporarily unavailable", 503)
			return
		}
		fmt.Fprint(w, `{"items":[{"id":"one","prompt":"修正的待发消息"}],"revision":2}`)
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.Live.Client, _ = newBackendClient(srv.URL, "test")
	a.Live.Connected = true
	s := a.current()
	s.Queue = messageQueue{Items: []queueItem{{ID: "one", Prompt: "原来的待发消息"}}, Revision: 1}
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "编辑")
	click(t, tt, "编辑待发消息")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("修正的待发消息")
	click(t, tt, "保存待发消息")
	waitNative(t, a, func() bool { return !a.Live.Loading["queue/"+s.RemoteID] })
	if a.Live.EditQueueID != "one" || a.Live.QueueDraft != "修正的待发消息" || s.Queue.Items[0].Prompt != "原来的待发消息" {
		t.Fatal("failed queue edit closed or erased draft")
	}
	tt.Frame()
	click(t, tt, "保存待发消息")
	waitNative(t, a, func() bool { return !a.Live.Loading["queue/"+s.RemoteID] })
	if a.Live.EditQueueID != "" || s.Queue.Items[0].Prompt != "修正的待发消息" || calls.Load() != 2 {
		t.Fatal("queue retry failed")
	}
}

func TestNativeLateSessionListDoesNotReplaceCreatedSession(t *testing.T) {
	entered, release, responded := make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sessions" && r.Method == "GET":
			close(entered)
			<-release
			fmt.Fprint(w, `[]`)
			close(responded)
		case r.URL.Path == "/sessions" && r.Method == "POST":
			fmt.Fprint(w, `{"id":"new-session"}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.current().RemoteID = ""
	a.Live.Connected = true
	a.Live.Client, _ = newBackendClient(srv.URL, "test")
	a.refreshLiveSessions("")
	<-entered
	a.createLiveSessionIn("", "/demo/easyagent", "")
	waitNative(t, a, func() bool { return !a.Live.Loading["create-session"] })
	close(release)
	<-responded
	// Give the stale response time to cross HTTP decoding and the UI queue.
	until := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(until) {
		a.liveTick()
		time.Sleep(time.Millisecond)
	}
	if a.current().RemoteID != "new-session" {
		t.Fatal("late initial list discarded the newly created session")
	}
}
