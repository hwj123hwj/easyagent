package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/skillmarket"
)

// newMarketFixture starts a fake store serving one installable skill and
// returns a tool bound to it.
func newMarketFixture(t *testing.T) (*SkillMarketTool, string) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	file, err := zw.Create("demo-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("---\nname: demo-skill\ndescription: demo\n---\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(buf.Bytes())

	item := skillmarket.SkillItem{
		ID:          7,
		Name:        "demo-skill",
		DisplayName: "Demo Skill",
		Description: "A demo",
		Version:     "2.0.0",
		SHA256:      strPtrM(hex.EncodeToString(digest[:])),
		TarballSize: int64(buf.Len()),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": []skillmarket.SkillItem{item}, "total": 1, "page": 1})
	})
	mux.HandleFunc("GET /api/skills/7", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(item)
	})
	mux.HandleFunc("GET /api/skills/7/download", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	skillsDir := t.TempDir()
	client := skillmarket.NewClient(skillsDir, skillmarket.WithBaseURL(server.URL))
	return NewSkillMarketTool(client), skillsDir
}

func strPtrM(s string) *string { return &s }

func rawParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSkillMarketToolValidate(t *testing.T) {
	tool, _ := newMarketFixture(t)

	if _, err := tool.Validate(rawParams(t, map[string]any{"action": "search"})); err != nil {
		t.Fatalf("search should validate: %v", err)
	}
	if _, err := tool.Validate(rawParams(t, map[string]any{"action": "install"})); err == nil {
		t.Fatal("install without id should fail validation")
	}
	if _, err := tool.Validate(rawParams(t, map[string]any{"action": "detail", "id": 7})); err != nil {
		t.Fatalf("detail with id should validate: %v", err)
	}
	if _, err := tool.Validate(rawParams(t, map[string]any{"action": "uninstall"})); err == nil {
		t.Fatal("uninstall without name should fail validation")
	}
	if _, err := tool.Validate(rawParams(t, map[string]any{"action": "explode"})); err == nil {
		t.Fatal("unknown action should fail validation")
	}
}

func TestSkillMarketToolRequiresConfirmationForMutations(t *testing.T) {
	tool, _ := newMarketFixture(t)

	if _, ok := tool.RequiresConfirmation(rawParams(t, map[string]any{"action": "search"})); ok {
		t.Fatal("search must not require confirmation")
	}
	if _, ok := tool.RequiresConfirmation(rawParams(t, map[string]any{"action": "detail", "id": 7})); ok {
		t.Fatal("detail must not require confirmation")
	}
	if desc, ok := tool.RequiresConfirmation(rawParams(t, map[string]any{"action": "install", "id": 7})); !ok || desc == "" {
		t.Fatal("install must require confirmation")
	}
	if desc, ok := tool.RequiresConfirmation(rawParams(t, map[string]any{"action": "uninstall", "name": "demo-skill"})); !ok || desc == "" {
		t.Fatal("uninstall must require confirmation")
	}
}

func TestSkillMarketToolConcurrencySafety(t *testing.T) {
	tool, _ := newMarketFixture(t)

	if !tool.IsConcurrencySafe(rawParams(t, map[string]any{"action": "search"})) {
		t.Fatal("search is concurrency-safe")
	}
	if tool.IsConcurrencySafe(rawParams(t, map[string]any{"action": "install", "id": 7})) {
		t.Fatal("install is not concurrency-safe")
	}
}

func TestSkillMarketToolSearchAndDetail(t *testing.T) {
	tool, _ := newMarketFixture(t)

	result, err := tool.Execute(context.Background(), rawParams(t, map[string]any{"action": "search", "query": "demo"}), nil)
	if err != nil {
		t.Fatalf("search execute: %v", err)
	}
	if result.IsError || !bytes.Contains([]byte(result.Content), []byte("Demo Skill")) {
		t.Fatalf("unexpected search result: %+v", result)
	}

	result, err = tool.Execute(context.Background(), rawParams(t, map[string]any{"action": "detail", "id": 7}), nil)
	if err != nil {
		t.Fatalf("detail execute: %v", err)
	}
	if result.IsError || !bytes.Contains([]byte(result.Content), []byte(`"version": "2.0.0"`)) {
		t.Fatalf("unexpected detail result: %s", result.Content)
	}
}

func TestSkillMarketToolInstallUninstallWithMutationCallback(t *testing.T) {
	tool, skillsDir := newMarketFixture(t)

	mutations := 0
	tool.onMutation = func() { mutations++ }

	result, err := tool.Execute(context.Background(), rawParams(t, map[string]any{"action": "install", "id": 7}), nil)
	if err != nil {
		t.Fatalf("install execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("install reported error: %s", result.Content)
	}
	if mutations != 1 {
		t.Fatalf("expected 1 mutation callback, got %d", mutations)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo-skill", "demo-skill", "SKILL.md")); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}

	result, err = tool.Execute(context.Background(), rawParams(t, map[string]any{"action": "uninstall", "name": "demo-skill"}), nil)
	if err != nil {
		t.Fatalf("uninstall execute: %v", err)
	}
	if mutations != 2 {
		t.Fatalf("expected 2 mutation callbacks, got %d", mutations)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo-skill")); !os.IsNotExist(err) {
		t.Fatal("skill still present after uninstall")
	}
}
