package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetResourcesRemovesPreviousRuntimeOnly(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "darwin-arm64")
	stale := filepath.Join(target, "runtime", "node")
	if err := os.MkdirAll(filepath.Dir(stale), 0755); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(root, "other-target")
	for _, path := range []string{stale, filepath.Join(target, "workflow-runtime.mjs"), sibling} {
		if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := resetResources(target); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("stale resources: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "old" {
		t.Fatalf("other target changed: %s, %v", data, err)
	}
}
