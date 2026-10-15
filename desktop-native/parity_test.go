package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func filePreviewFixture(t *testing.T) *preview {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/read-file":
			json.NewEncoder(w).Encode(map[string]any{"content": "# Saved document\n\nOriginal text\n"})
		case "/workspace/list-dir":
			fmt.Fprint(w, `[{"name":"docs","path":"/demo/easyagent/docs","isDir":true},{"name":"README.md","path":"/demo/easyagent/README.md","isDir":false}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(srv.Close)
	a := alignmentPreview(t)
	a.Live.Client, _ = newBackendClient(srv.URL, "test")
	a.Live.Connected = true
	a.Workbench = true
	a.bindFileScope()
	return a
}
func TestNativeFilesEditCancelCloseAndRestore(t *testing.T) {
	a := filePreviewFixture(t)
	path := "/demo/easyagent/README.md"
	a.openNativeFile(path)
	key := a.Live.Workbench.ActiveFile
	waitNative(t, a, func() bool { return !a.Live.Workbench.Files[key].Loading })
	f := a.Live.Workbench.Files[key]
	tt := ui.NewTester(a.View, 1240, 820)
	if !tt.HasText("Saved document") {
		t.Fatal("Markdown file preview missing")
	}
	click(t, tt, "编辑")
	f.Code.Insert("unsaved")
	tt.Frame()
	click(t, tt, "取消编辑")
	if f.Editing || f.Code.Buffer.String() != f.Original || f.Code.Buffer.Modified() {
		t.Fatal("cancel did not restore original")
	}
	f.Scroll.Y = 123
	data := a.fileStateSnapshot()
	version := a.Live.Store.reserve("workspace-files.json")
	if err := a.Live.Store.save("workspace-files.json", version, data); err != nil {
		t.Fatal(err)
	}
	recovered := alignmentPreview(t)
	recovered.Live.Dir = a.Live.Dir
	recovered.loadFileState()
	recovered.Live.Client = a.Live.Client
	recovered.Workbench = true
	recovered.bindFileScope()
	recovered.restoreFileTabs()
	waitNative(t, recovered, func() bool {
		return recovered.Live.Workbench.Files[key] != nil && !recovered.Live.Workbench.Files[key].Loading
	})
	if recovered.Live.Workbench.ActiveFile != key || !recovered.Live.Workbench.Files[key].Preview {
		t.Fatal("file tabs/preview not restored")
	}
	click(t, tt, "关闭标签："+path)
	if a.Live.Workbench.Files[key] != nil {
		t.Fatal("close did not remove tab")
	}
	state := a.fileStateSnapshot()
	var saved map[string]fileWorkspace
	json.Unmarshal(state, &saved)
	if len(saved[a.Live.Workbench.FileScope].Tabs) != 0 {
		t.Fatal("closed tab resurrected")
	}
}
func TestNativeWorkspacePathAndFuzzySearch(t *testing.T) {
	for _, path := range []string{"/demo/easyagent/../secret", "/demo/easyagent/./a", "/demo/easyagent-other/file"} {
		if workspaceContains("/demo/easyagent", path) {
			t.Fatal("restore escaped workspace", path)
		}
	}
	if !workspaceContains(`C:\work`, `C:\work\src\main.go`) {
		t.Fatal("Windows workspace path failed")
	}
	matches := fuzzyPaths([]string{"docs/main.md", "src/main.go", "file.txt"}, "mgo")
	if len(matches) != 1 || matches[0] != "src/main.go" {
		t.Fatal("fuzzy file search failed", matches)
	}
}
func TestNativeContextRedactsLargeImagePayload(t *testing.T) {
	raw := map[string]any{"content": []any{map[string]any{"type": "image", "data": "secret-payload", "mime_type": "image/png"}}}
	redacted := pretty(inspectContext(raw))
	if strings.Contains(redacted, "secret-payload") || !strings.Contains(redacted, "image/png") {
		t.Fatal("image inspection changed metadata or leaked payload", redacted)
	}
	if !strings.Contains(pretty(raw), "secret-payload") {
		t.Fatal("redaction changed model input")
	}
}
func TestNativeStructuredPlanFromTools(t *testing.T) {
	s := &session{Run: &runInfo{ID: "run"}}
	s.event(agentEvent{Type: "tool_start", ToolID: "todo", ToolName: "todo_write", Args: map[string]any{"todos": []any{map[string]any{"content": "检查测试", "status": "in_progress"}}}})
	s.event(agentEvent{Type: "tool_end", ToolID: "todo"})
	if len(s.Plan) != 1 || s.Plan[0].Content != "检查测试" {
		t.Fatal("todo tool not projected", s.Plan)
	}
	a := alignmentPreview(t)
	a.current().Plan = s.Plan
	a.Workbench = true
	a.Live.Workbench.Tab = "plan"
	tt := ui.NewTester(a.View, 1240, 820)
	if !tt.HasText("检查测试") || !tt.HasText("0 / 1 已完成") {
		t.Fatal("structured plan not visible")
	}
}
func TestNativeStreamingMarkdown(t *testing.T) {
	var doc markdownDocument
	source := ""
	stream := true
	tt := ui.NewTester(func(c *ui.Context) { doc.render(c, source, stream) }, 760, 650)
	for _, part := range []string{"## 回", "复\n\n**强调", "内容**\n\n```go\nfunc ready() bool { return true }", "\n```\n\n- [x] 已验证\n\n| 模型 | 状态 |\n|---|---|\n| native | ok |"} {
		source += part
		tt.Frame()
	}
	stream = false
	tt.Frame()
	if !tt.HasText("强调内容") || !tt.HasText("已验证") || !tt.HasText("native") {
		t.Fatal("streaming Markdown not completed")
	}
	click(t, tt, "复制代码")
	if tt.Clipboard() != "func ready() bool { return true }" {
		t.Fatal("code copy lost source", tt.Clipboard())
	}
}
func TestNativeNewParityPanelLayouts(t *testing.T) {
	dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS")
	if dir == "" {
		t.Skip("snapshot opt-in")
	}
	for _, tab := range []string{"files", "review", "context", "profile", "kb", "plan", "tasks"} {
		for _, width := range []int{900, 1240} {
			a := alignmentPreview(t)
			a.Workbench = true
			a.Live.Workbench.Tab = tab
			a.Live.Data["context"] = map[string]any{"system": "System prompt", "messages": []any{map[string]any{"role": "user", "message": map[string]any{"content": "hello"}}}, "tools": []any{map[string]any{"name": "read_file"}}}
			a.Live.Data["profile"] = map[string]any{"total_facts": float64(1), "summary": "**偏好**：中文", "categories": []any{map[string]any{"name": "preferences", "label": "偏好", "facts": []any{map[string]any{"key": "language", "value": "中文", "source": "user", "updated": "2026-10-15"}}}}}
			a.current().Plan = []nativePlanEntry{{Content: "验证布局", Status: "in_progress"}}
			tt := ui.NewTester(a.View, width, 820)
			saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("parity-%s-%d.png", tab, width)))
		}
	}
}
