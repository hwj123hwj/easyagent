package skillmarket

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConcurrentClientsDoNotOverwriteOrRemoveWinner(t *testing.T) {
	store := newFakeStore(t)
	dir := t.TempDir()
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			client := NewClient(dir, WithBaseURL(store.server.URL))
			_, err := client.InstallWithProgress(t.Context(), 1, func(p InstallProgress) {
				if p.Phase == PhaseExtracting {
					ready <- struct{}{}
					<-release
				}
			})
			results <- err
		}()
	}
	<-ready
	<-ready
	close(release)
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, ErrAlreadyInstalled)) || (b == nil && errors.Is(a, ErrAlreadyInstalled))) {
		t.Fatalf("results: %v, %v", a, b)
	}
	list, err := NewClient(dir).ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("winner lost: %v %v", list, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello-skill", "hello-skill", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("staging left behind: %v", entries)
	}
}

func TestUserDirectoryCreatedDuringDownloadSurvives(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)
	target := filepath.Join(client.SkillsDir(), "hello-skill")
	_, err := client.InstallWithProgress(t.Context(), 1, func(p InstallProgress) {
		if p.Phase == PhaseExtracting {
			if err := os.Mkdir(target, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "mine"), []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("got %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "mine"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("lost contents: %s %v", data, err)
	}
}

func TestCancelBeforePublishLeavesNoInstalledSkill(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)
	ctx, cancel := context.WithCancel(t.Context())
	_, err := client.InstallWithProgress(ctx, 1, func(p InstallProgress) {
		if p.Phase == PhaseExtracting {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(client.SkillsDir(), "hello-skill")); !os.IsNotExist(err) {
		t.Fatalf("installed after cancellation: %v", err)
	}
}

func TestExtractionRejectsZipBombAndSymlink(t *testing.T) {
	for _, kind := range []string{"bomb", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			if kind == "bomb" {
				h := &zip.FileHeader{Name: "huge", Method: zip.Store, UncompressedSize64: maxPackageSize + 1, CompressedSize64: 1, Flags: 0}
				w, err := zw.CreateRaw(h)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte("x"))
			} else {
				h := &zip.FileHeader{Name: "link", Method: zip.Store}
				h.SetMode(os.ModeSymlink | 0777)
				w, err := zw.CreateHeader(h)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte("/tmp"))
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			src := filepath.Join(t.TempDir(), "bad.zip")
			_ = os.WriteFile(src, buf.Bytes(), 0644)
			if err := extractZip(src, t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
