package skillmarket

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeStore spins up an httptest server serving a minimal skill store:
// one skill (id=1, name=hello-skill) whose zip contains a SKILL.md.
type fakeStore struct {
	server    *httptest.Server
	skill     SkillItem
	zipBytes  []byte
	downloads atomic.Int32
}

func newFakeStore(t *testing.T) *fakeStore {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	file, err := zw.Create("hello-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("---\nname: hello-skill\ndescription: greeting\n---\n\n# Hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()
	digest := sha256.Sum256(zipBytes)

	store := &fakeStore{
		zipBytes: zipBytes,
		skill: SkillItem{
			ID:          1,
			Name:        "hello-skill",
			DisplayName: "Hello Skill",
			Description: "Says hello",
			Category:    "demo",
			Tags:        []string{"greeting"},
			Version:     "1.0.0",
			SHA256:      strPtr(hex.EncodeToString(digest[:])),
			TarballSize: int64(len(zipBytes)),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"skills": []SkillItem{store.skill},
			"total":  1,
			"page":   1,
		})
	})
	mux.HandleFunc("GET /api/skills/1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.skill)
	})
	mux.HandleFunc("GET /api/skills/2", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sections": []Section{{ID: 1, Name: "Popular", OrderIndex: 1}},
		})
	})
	mux.HandleFunc("GET /api/skills/1/download", func(w http.ResponseWriter, r *http.Request) {
		store.downloads.Add(1)
		_, _ = w.Write(store.zipBytes)
	})

	store.server = httptest.NewServer(mux)
	t.Cleanup(store.server.Close)
	return store
}

func strPtr(s string) *string { return &s }

func newTestClient(t *testing.T, store *fakeStore) *Client {
	t.Helper()
	dir := t.TempDir()
	return NewClient(dir, WithBaseURL(store.server.URL))
}

func TestSearchSendsFiltersAndParsesResponse(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	result, err := client.Search(t.Context(), BrowseQuery{Query: "hello", Page: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "hello-skill" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Total != 1 {
		t.Fatalf("expected total=1, got %d", result.Total)
	}
}

func TestDetailNotFound(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	if _, err := client.Detail(t.Context(), 2, ""); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSections(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	sections, err := client.Sections(t.Context())
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	if len(sections) != 1 || sections[0].Name != "Popular" {
		t.Fatalf("unexpected sections: %+v", sections)
	}
}

func TestInstallDownloadVerifyExtractManifest(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	item, err := client.Install(t.Context(), 1)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if item.Name != "hello-skill" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if store.downloads.Load() != 1 {
		t.Fatalf("expected exactly one download, got %d", store.downloads.Load())
	}

	// Extracted content exists and manifest records the install.
	skillMD := filepath.Join(client.SkillsDir(), "hello-skill", "hello-skill", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("SKILL.md not extracted: %v", err)
	}
	if !HasManifest(filepath.Join(client.SkillsDir(), "hello-skill")) {
		t.Fatal("manifest missing after install")
	}

	installed, err := client.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(installed) != 1 || installed[0].Name != "hello-skill" || installed[0].Version != "1.0.0" {
		t.Fatalf("unexpected installed list: %+v", installed)
	}

	// Temp zip cleaned up.
	entries, _ := os.ReadDir(os.TempDir())
	for _, e := range entries {
		if bytes.HasPrefix([]byte(e.Name()), []byte("easyagent-skill-")) {
			t.Fatalf("temp zip left behind: %s", e.Name())
		}
	}
}

func TestInstallDigestMismatchRejected(t *testing.T) {
	store := newFakeStore(t)
	store.skill.SHA256 = strPtr("0000000000000000000000000000000000000000000000000000000000000000")
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err == nil {
		t.Fatal("expected digest mismatch error")
	}
	// Nothing left on disk after failed install.
	if _, err := os.Stat(filepath.Join(client.SkillsDir(), "hello-skill")); !os.IsNotExist(err) {
		t.Fatal("target dir should not exist after failed install")
	}
}

func TestInstallRefusesExistingDirectory(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	// Pre-create a user-owned skill dir with the same name.
	target := filepath.Join(client.SkillsDir(), "hello-skill")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("hand-written"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Install(t.Context(), 1); err == nil {
		t.Fatal("expected ErrAlreadyInstalled")
	} else if !bytes.Contains([]byte(err.Error()), []byte("already exists")) {
		t.Fatalf("unexpected error: %v", err)
	}

	// Hand-written content untouched.
	data, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil || string(data) != "hand-written" {
		t.Fatalf("user skill clobbered: %v %q", err, data)
	}
}

func TestInstallRejectsInvalidPackageName(t *testing.T) {
	store := newFakeStore(t)
	store.skill.Name = "../evil"
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err == nil {
		t.Fatal("expected invalid package name error")
	}
}

func TestInstallLegacyRowWithoutSHA(t *testing.T) {
	store := newFakeStore(t)
	store.skill.SHA256 = nil
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err != nil {
		t.Fatalf("legacy install (no sha256) should succeed: %v", err)
	}
}

func TestUninstallOnlyRemovesMarketplaceSkills(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err != nil {
		t.Fatal(err)
	}

	// Hand-written skill in the same root.
	handDir := filepath.Join(client.SkillsDir(), "my-own-skill")
	if err := os.MkdirAll(handDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handDir, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := client.Uninstall("my-own-skill"); err != ErrNotMarketplace {
		t.Fatalf("expected ErrNotMarketplace for hand-written skill, got %v", err)
	}
	if _, err := os.Stat(handDir); err != nil {
		t.Fatal("hand-written skill was removed")
	}

	if err := client.Uninstall("hello-skill"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(client.SkillsDir(), "hello-skill")); !os.IsNotExist(err) {
		t.Fatal("marketplace skill still present after uninstall")
	}

	// Path traversal refused.
	if err := client.Uninstall("../escape"); err == nil {
		t.Fatal("expected traversal refusal")
	}
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("../../../etc/passwd"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := extractZip(src, dst); err == nil {
		t.Fatal("expected zip-slip refusal")
	}
}

func TestExtractZipRejectsAbsoluteEntry(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("/etc/passwd"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := extractZip(src, dst); err == nil {
		t.Fatal("expected absolute-entry refusal")
	}
}

func TestExtractZipPreservesExecutableBit(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: "hello-skill/scripts/run.sh", Method: zip.Deflate}
	hdr.SetMode(0o755)
	script, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Write([]byte("#!/bin/sh\necho hi\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "skill.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := extractZip(src, dst); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	info, err := os.Stat(filepath.Join(dst, "hello-skill", "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit lost: %v", info.Mode())
	}
}

func TestDownloadSizeMismatchRejected(t *testing.T) {
	store := newFakeStore(t)
	store.skill.TarballSize = int64(len(store.zipBytes)) + 1
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestInstallWithProgressEmitsPhases(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	var phases []InstallPhase
	sawBytes := false
	item, err := client.InstallWithProgress(t.Context(), 1, func(p InstallProgress) {
		if p.Err != "" {
			t.Fatalf("unexpected progress error: %s", p.Err)
		}
		if p.Phase == PhaseDownloading && p.BytesDone > 0 {
			sawBytes = true
		}
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	})
	if err != nil {
		t.Fatalf("InstallWithProgress: %v", err)
	}
	if item.Name != "hello-skill" {
		t.Fatalf("unexpected item: %+v", item)
	}
	want := []InstallPhase{PhaseResolving, PhaseDownloading, PhaseVerifying, PhaseExtracting, PhaseDone}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phases = %v, want %v", phases, want)
		}
	}
	if !sawBytes {
		t.Fatal("no byte-level progress emitted")
	}
}

func TestInstallWithProgressReportsFailure(t *testing.T) {
	store := newFakeStore(t)
	store.skill.SHA256 = strPtr("0000000000000000000000000000000000000000000000000000000000000000")
	client := newTestClient(t, store)

	sawErr := false
	_, err := client.InstallWithProgress(t.Context(), 1, func(p InstallProgress) {
		if p.Err != "" {
			sawErr = true
		}
	})
	if err == nil || !sawErr {
		t.Fatalf("expected error path with progress error, err=%v sawErr=%v", err, sawErr)
	}
}

func TestInstallWithProgressAlreadyInstalled(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	sawErr := false
	_, err := client.InstallWithProgress(t.Context(), 1, func(p InstallProgress) {
		if bytes.Contains([]byte(p.Err), []byte("already exists")) {
			sawErr = true
		}
	})
	if err == nil {
		t.Fatal("expected ErrAlreadyInstalled")
	}
	if !sawErr {
		t.Fatal("expected already-installed error in progress stream")
	}
}
