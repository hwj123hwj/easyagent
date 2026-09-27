package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	wtpkg "github.com/hwj123hwj/easyagent/internal/worktree"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestLoadSaveRoutes(t *testing.T) {
	dir := t.TempDir()
	routesFile := filepath.Join(dir, "routes.json")

	h := &Handler{
		routes:     make(map[string]*ChatRoute),
		routesFile: routesFile,
	}

	// Save some routes
	h.setRoute("oc_chat1", &ChatRoute{
		SessionID:         "sess_1",
		ProjectRoot:       "/tmp/project-a/.easyagent/worktrees/project-a",
		SourceProjectRoot: "/tmp/project-a",
		SourceRepoRoot:    "/tmp/project-a",
		WorktreeRoot:      "/tmp/project-a/.easyagent/worktrees/project-a",
		WorktreeBranch:    "easyagent/project-a",
		TaskPath:          "/tmp/project-a/.easyagent/worktrees/project-a/TASK.md",
		ChatName:          "Project A",
	})
	h.setRoute("ou_user1", &ChatRoute{
		SessionID: "sess_2",
	})

	// Verify file was written
	if _, err := os.Stat(routesFile); err != nil {
		t.Fatalf("routes file not created: %v", err)
	}

	// Create a new handler and load routes
	h2 := &Handler{
		routes:     make(map[string]*ChatRoute),
		routesFile: routesFile,
	}
	h2.loadRoutes()

	r1 := h2.getRoute("oc_chat1")
	if r1 == nil {
		t.Fatal("expected route for oc_chat1")
	}
	if r1.SessionID != "sess_1" {
		t.Errorf("sessionID = %q, want %q", r1.SessionID, "sess_1")
	}
	if r1.ProjectRoot != "/tmp/project-a/.easyagent/worktrees/project-a" {
		t.Errorf("projectRoot = %q", r1.ProjectRoot)
	}
	if r1.SourceProjectRoot != "/tmp/project-a" {
		t.Errorf("sourceProjectRoot = %q", r1.SourceProjectRoot)
	}
	if r1.WorktreeBranch != "easyagent/project-a" {
		t.Errorf("worktreeBranch = %q", r1.WorktreeBranch)
	}
	if r1.TaskPath == "" {
		t.Error("expected task path")
	}
	if r1.ChatName != "Project A" {
		t.Errorf("chatName = %q, want %q", r1.ChatName, "Project A")
	}

	r2 := h2.getRoute("ou_user1")
	if r2 == nil || r2.SessionID != "sess_2" {
		t.Error("expected route for ou_user1")
	}
}

func TestWorkspaceFor(t *testing.T) {
	h := &Handler{
		routes:     make(map[string]*ChatRoute),
		routesFile: filepath.Join(t.TempDir(), "routes.json"),
		workspace:  "/default/workspace",
	}

	// No route → default workspace
	if got := h.workspaceFor("oc_unknown"); got != "/default/workspace" {
		t.Errorf("expected default workspace, got %q", got)
	}

	// Route with project root
	h.setRoute("oc_project", &ChatRoute{ProjectRoot: "/my/project"})
	if got := h.workspaceFor("oc_project"); got != "/my/project" {
		t.Errorf("expected /my/project, got %q", got)
	}

	// Route without project root → default
	h.setRoute("oc_nodir", &ChatRoute{SessionID: "sess_1"})
	if got := h.workspaceFor("oc_nodir"); got != "/default/workspace" {
		t.Errorf("expected default workspace for empty projectRoot, got %q", got)
	}
}

func TestCreateSessionSendsCwd(t *testing.T) {
	var gotCwd string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Cwd string `json:"cwd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotCwd = req.Cwd
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "sess_1"})
	}))
	defer server.Close()

	h := &Handler{
		piAgentURL: server.URL,
		httpClient: server.Client(),
	}
	id, err := h.createSession(context.Background(), "/tmp/pi-worktree")
	if err != nil {
		t.Fatalf("createSession failed: %v", err)
	}
	if id != "sess_1" {
		t.Fatalf("id = %q, want sess_1", id)
	}
	if gotCwd != "/tmp/pi-worktree" {
		t.Fatalf("cwd = %q, want /tmp/pi-worktree", gotCwd)
	}
}

func TestPrepareProjectRouteNonGitFallback(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{}

	route, note, err := h.prepareProjectRoute(context.Background(), dir, "plain project", "task")
	if err != nil {
		t.Fatalf("prepareProjectRoute failed: %v", err)
	}
	if route.ProjectRoot != dir {
		t.Fatalf("ProjectRoot = %q, want %q", route.ProjectRoot, dir)
	}
	if route.WorktreeRoot != "" {
		t.Fatalf("WorktreeRoot = %q, want empty", route.WorktreeRoot)
	}
	if note == "" {
		t.Fatal("expected fallback note")
	}
}

func TestCmdWorktreeStatusAndDiscard(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	wt, err := wtpkg.NewManager().Create(context.Background(), wtpkg.CreateOptions{
		ProjectPath: repo,
		Name:        "discard from feishu",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := newTestHandler(t)
	h.setRoute("oc_chat", routeFromWorktree(wt, "sess_1"))

	status := h.cmdWorktree(context.Background(), "oc_chat", "/worktree status")
	for _, want := range []string{"Worktree 状态", wt.Branch, "TASK.md"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}

	reply := h.cmdWorktree(context.Background(), "oc_chat", "/worktree discard")
	if !strings.Contains(reply, "已丢弃并清理") {
		t.Fatalf("discard reply = %s", reply)
	}
	route := h.getRoute("oc_chat")
	if route.ProjectRoot != wt.SourceProjectPath {
		t.Fatalf("ProjectRoot = %q, want %q", route.ProjectRoot, wt.SourceProjectPath)
	}
	if route.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty", route.SessionID)
	}
	if route.WorktreeRoot != "" || route.WorktreeBranch != "" || route.TaskPath != "" {
		t.Fatalf("worktree fields not cleared: %+v", route)
	}
	if _, err := os.Stat(wt.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists or stat failed: %v", err)
	}
	if branchExists(t, repo, wt.Branch) {
		t.Fatalf("expected branch %q to be deleted", wt.Branch)
	}
}

func TestCmdWorktreeCommitClearsRoute(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	wt, err := wtpkg.NewManager().Create(context.Background(), wtpkg.CreateOptions{
		ProjectPath: repo,
		Name:        "commit from feishu",
		Task:        "ship it",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.WorktreeRoot, "README.md"), []byte("# done\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	h := newTestHandler(t)
	h.setRoute("oc_chat", routeFromWorktree(wt, "sess_1"))

	reply := h.cmdWorktree(context.Background(), "oc_chat", "/worktree commit finish task")
	for _, want := range []string{"已提交并清理", wt.Branch, "Commit"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("commit reply missing %q:\n%s", want, reply)
		}
	}
	route := h.getRoute("oc_chat")
	if route.ProjectRoot != wt.SourceProjectPath {
		t.Fatalf("ProjectRoot = %q, want %q", route.ProjectRoot, wt.SourceProjectPath)
	}
	if route.SessionID != "" || route.WorktreeRoot != "" {
		t.Fatalf("route not cleared after commit: %+v", route)
	}
	if _, err := os.Stat(wt.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists or stat failed: %v", err)
	}
	if !branchExists(t, repo, wt.Branch) {
		t.Fatalf("expected branch %q to remain", wt.Branch)
	}
}

func TestCmdWorktreeNoRoute(t *testing.T) {
	h := newTestHandler(t)
	reply := h.cmdWorktree(context.Background(), "oc_chat", "/worktree status")
	if !strings.Contains(reply, "未绑定 worktree") {
		t.Fatalf("reply = %s", reply)
	}
}

func TestHandleCardActionStatus(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	wt, err := wtpkg.NewManager().Create(context.Background(), wtpkg.CreateOptions{
		ProjectPath: repo,
		Name:        "card status",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := newTestHandler(t)
	h.setRoute("oc_chat", routeFromWorktree(wt, "sess_1"))

	resp, err := h.HandleCardAction(context.Background(), cardActionEvent("oc_chat", worktreeActionStatus, ""))
	if err != nil {
		t.Fatalf("HandleCardAction failed: %v", err)
	}
	if resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("unexpected toast: %#v", resp.Toast)
	}
	if resp.Card == nil || resp.Card.Type != "card_json" {
		t.Fatalf("expected card_json response: %#v", resp.Card)
	}
	card, ok := resp.Card.Data.(map[string]any)
	if !ok {
		t.Fatalf("card data type = %T", resp.Card.Data)
	}
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 3 {
		t.Fatalf("expected card with actions, got %d elements", len(elements))
	}
}

func TestHandleCardActionCommit(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	wt, err := wtpkg.NewManager().Create(context.Background(), wtpkg.CreateOptions{
		ProjectPath: repo,
		Name:        "card commit",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.WorktreeRoot, "README.md"), []byte("# card commit\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	h := newTestHandler(t)
	h.setRoute("oc_chat", routeFromWorktree(wt, "sess_1"))

	resp, err := h.HandleCardAction(context.Background(), cardActionEvent("oc_chat", worktreeActionCommit, "finish from card"))
	if err != nil {
		t.Fatalf("HandleCardAction failed: %v", err)
	}
	if resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("unexpected toast: %#v", resp.Toast)
	}
	route := h.getRoute("oc_chat")
	if route.WorktreeRoot != "" || route.SessionID != "" {
		t.Fatalf("route not cleared: %+v", route)
	}
	if _, err := os.Stat(wt.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists or stat failed: %v", err)
	}
}

func TestHandleCardActionCommitRequiresMessage(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	wt, err := wtpkg.NewManager().Create(context.Background(), wtpkg.CreateOptions{
		ProjectPath: repo,
		Name:        "card commit missing message",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	h := newTestHandler(t)
	h.setRoute("oc_chat", routeFromWorktree(wt, "sess_1"))

	resp, err := h.HandleCardAction(context.Background(), cardActionEvent("oc_chat", worktreeActionCommit, ""))
	if err != nil {
		t.Fatalf("HandleCardAction failed: %v", err)
	}
	if resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("unexpected toast: %#v", resp.Toast)
	}
	if h.getRoute("oc_chat").WorktreeRoot == "" {
		t.Fatal("route should remain bound when commit message is missing")
	}
}

func TestLoadRoutes_MissingFile(t *testing.T) {
	h := &Handler{
		routes:     make(map[string]*ChatRoute),
		routesFile: filepath.Join(t.TempDir(), "nonexistent.json"),
	}
	h.loadRoutes() // should not panic
	if len(h.routes) != 0 {
		t.Errorf("expected empty routes, got %d", len(h.routes))
	}
}

func TestLoadRoutes_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	routesFile := filepath.Join(dir, "routes.json")
	os.WriteFile(routesFile, []byte("not json"), 0o644)

	h := &Handler{
		routes:     make(map[string]*ChatRoute),
		routesFile: routesFile,
	}
	h.loadRoutes() // should not panic, just warn
	if len(h.routes) != 0 {
		t.Errorf("expected empty routes on invalid JSON, got %d", len(h.routes))
	}
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	access, err := OpenOwnerAccess(filepath.Join(t.TempDir(), "owner.json"), "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{
		access:     access,
		routes:     make(map[string]*ChatRoute),
		routesFile: filepath.Join(t.TempDir(), "routes.json"),
		workspace:  "/default/workspace",
	}
}

func routeFromWorktree(wt *wtpkg.Info, sessionID string) *ChatRoute {
	return &ChatRoute{
		SessionID:         sessionID,
		ProjectRoot:       wt.ProjectPath,
		SourceProjectRoot: wt.SourceProjectPath,
		SourceRepoRoot:    wt.SourceRoot,
		WorktreeRoot:      wt.WorktreeRoot,
		WorktreeBranch:    wt.Branch,
		TaskPath:          wt.TaskPath,
		ChatName:          "Project",
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "easyagent@example.com")
	runGit(t, repo, "config", "user.name", "easyagent")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("eval repo symlink: %v", err)
	}
	return realRepo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func cardActionEvent(chatKey, action, commitMessage string) *callback.CardActionTriggerEvent {
	formValue := map[string]interface{}{}
	if commitMessage != "" {
		formValue[worktreeCardCommitMessage] = commitMessage
	}
	return &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "test-owner"},
			Context:  &callback.Context{OpenChatID: chatKey},
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					worktreeCardActionKey: action,
					worktreeCardChatKey:   chatKey,
				},
				FormValue: formValue,
			},
		},
	}
}
