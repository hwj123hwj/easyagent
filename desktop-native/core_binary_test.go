package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCoreBinary_PrefersBundledOverPath reproduces the Linux packaging bug:
// the deb/tar.gz layout places the core next to the UI executable, but the
// old lookup only checked the macOS bundle path (../Resources/easyagent)
// and then fell back to $PATH — where an old ~/.easyagent/bin/easyagent CLI
// (no `serve` subcommand) hijacked the launch and the UI showed 未连接.
// The bundled core next to the exe must always win.
func TestCoreBinary_PrefersBundledOverPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "easyagent-native-preview")
	core := filepath.Join(dir, "easyagent")
	for _, f := range []string{exe, core} {
		if err := os.WriteFile(f, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("EASYAGENT_NATIVE_CORE", "")
	got, err := coreBinaryAt(exe)
	if err != nil {
		t.Fatalf("coreBinaryAt: %v", err)
	}
	if got != core {
		t.Fatalf("coreBinaryAt = %q, want bundled %q", got, core)
	}
}

// TestCoreBinary_MacBundleLayout keeps the macOS .app lookup working:
// EasyAgent.app/Contents/MacOS/<ui> + EasyAgent.app/Contents/Resources/easyagent.
func TestCoreBinary_MacBundleLayout(t *testing.T) {
	dir := t.TempDir()
	exeDir := filepath.Join(dir, "Contents", "MacOS")
	resDir := filepath.Join(dir, "Contents", "Resources")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(exeDir, "easyagent-native-preview")
	core := filepath.Join(resDir, "easyagent")
	for _, f := range []string{exe, core} {
		if err := os.WriteFile(f, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("EASYAGENT_NATIVE_CORE", "")
	got, err := coreBinaryAt(exe)
	if err != nil {
		t.Fatalf("coreBinaryAt: %v", err)
	}
	if got != core {
		t.Fatalf("coreBinaryAt = %q, want bundle core %q", got, core)
	}
}

// TestCoreBinary_RejectsServelessPathCLI ensures a PATH binary without the
// serve subcommand (old CLI) is not silently used — the exact failure users
// saw as an eternal 未连接.
func TestCoreBinary_RejectsServelessPathCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script probe is POSIX-only")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "easyagent-native-preview")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(binDir, "easyagent")
	// A binary that does NOT understand `serve --help` (mirrors the old CLI:
	// flag package prints usage and exits 2).
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("EASYAGENT_NATIVE_CORE", "")
	t.Setenv("PATH", binDir)

	if _, err := coreBinaryAt(exe); err == nil {
		t.Fatal("expected error when only a serve-less CLI is on PATH")
	}
}

// TestCoreBinary_AcceptsServeCapablePathCLI: PATH fallback still works when
// the CLI genuinely supports serve.
func TestCoreBinary_AcceptsServeCapablePathCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script probe is POSIX-only")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "easyagent-native-preview")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(binDir, "easyagent")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n[ \"$1\" = serve ] && exit 0 || exit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("EASYAGENT_NATIVE_CORE", "")
	t.Setenv("PATH", binDir)

	got, err := coreBinaryAt(exe)
	if err != nil {
		t.Fatalf("coreBinaryAt: %v", err)
	}
	if got != cli {
		t.Fatalf("coreBinaryAt = %q, want %q", got, cli)
	}
}

// TestCoreBinary_EnvOverride wins over everything.
func TestCoreBinary_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "custom-core")
	if err := os.WriteFile(core, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EASYAGENT_NATIVE_CORE", core)

	got, err := coreBinaryAt(filepath.Join(dir, "ui"))
	if err != nil {
		t.Fatalf("coreBinaryAt: %v", err)
	}
	if got != core {
		t.Fatalf("coreBinaryAt = %q, want env override %q", got, core)
	}
}

// TestLocalHostName_NotMacWordingOnLinux guards the topbar rename.
func TestLocalHostName_NotMacWordingOnLinux(t *testing.T) {
	name := localHostName()
	switch runtime.GOOS {
	case "darwin":
		if name != "此 Mac" {
			t.Fatalf("on darwin want 此 Mac, got %q", name)
		}
	default:
		if name == "此 Mac" || name == "" {
			t.Fatalf("on %s hostname must not be macOS wording, got %q", runtime.GOOS, name)
		}
	}
}
