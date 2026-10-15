//go:build !windows

package feishu

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBrowserLauncherCanBeCancelled(t *testing.T) {
	launcher := "xdg-open"
	if runtime.GOOS == "darwin" {
		launcher = "open"
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, launcher), []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := OpenBrowserContext(ctx, "https://example.com/test-only"); err == nil {
		t.Fatal("cancelled launcher succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("headless launcher ignored cancellation")
	}
}
