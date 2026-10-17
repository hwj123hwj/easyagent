// Build the existing core and package its runtime beside the native client.
// This does not install or restart any existing EasyAgent application/service.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func run(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}
}
func copyFile(from, to string) {
	src, err := os.Open(from)
	if err != nil {
		panic(err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		panic(err)
	}
	if err = os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		panic(err)
	}
	dst, err := os.CreateTemp(filepath.Dir(to), ".resource-*")
	if err != nil {
		panic(err)
	}
	temporary := dst.Name()
	defer os.Remove(temporary)
	if _, err = io.Copy(dst, src); err != nil {
		dst.Close()
		panic(err)
	}
	if err = dst.Chmod(info.Mode().Perm() | 0200); err != nil {
		dst.Close()
		panic(err)
	}
	if err = dst.Close(); err != nil {
		panic(err)
	}
	if err = os.Rename(temporary, to); err != nil {
		panic(err)
	}
}
func main() {
	cwd, _ := os.Getwd()
	root := filepath.Dir(cwd)
	target := filepath.Join(cwd, "resources", runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(target, 0755); err != nil {
		panic(err)
	}
	sha, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		panic(err)
	}
	run(root, "go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=native-"+strings.TrimSpace(string(sha)), "-o", filepath.Join(target, "easyagent"), "./cmd/easyagent")
	modules := []string{"github.com/zkrebbekx/go-mermaid", "github.com/srwiley/oksvg", "github.com/srwiley/rasterx"}
	// 冷缓存下 go list -m 的 {{.Dir}} 可能未解压间接依赖，先显式下载。
	for _, module := range modules {
		run(".", "go", "mod", "download", module)
	}
	for _, module := range modules {
		directory, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module).Output()
		if err != nil {
			panic(err)
		}
		name := filepath.Base(module)
		license := filepath.Join(strings.TrimSpace(string(directory)), "LICENSE")
		if _, err = os.Stat(license); err != nil {
			license = filepath.Join(strings.TrimSpace(string(directory)), "LICENSE.txt")
		}
		copyFile(license, filepath.Join(target, "licenses", name+"-LICENSE"))
	}
	if runtime.GOOS == "darwin" {
		app := filepath.Join(target, "cua-helper/EasyAgent Computer Use.app/Contents")
		_ = os.MkdirAll(filepath.Join(app, "MacOS"), 0755)
		arch := runtime.GOARCH
		if arch == "amd64" {
			arch = "x86_64"
		}
		sdk, err := exec.Command("xcrun", "--sdk", "macosx", "--show-sdk-path").Output()
		if err != nil {
			panic(err)
		}
		run(root, "xcrun", "swiftc", "-O", "-target", arch+"-apple-macosx14.0", "-sdk", strings.TrimSpace(string(sdk)), filepath.Join(root, "native/computer-use/main.swift"), "-o", filepath.Join(app, "MacOS/easyagent-cua-helper"))
		plist := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.hwj.easyagent.cua-helper</string><key>CFBundleName</key><string>EasyAgent Native Computer Use</string><key>CFBundleExecutable</key><string>easyagent-cua-helper</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>0.16.0</string><key>CFBundleVersion</key><string>0.16.0</string><key>LSMinimumSystemVersion</key><string>14.0</string><key>LSUIElement</key><true/></dict></plist>`
		if err = os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plist), 0644); err != nil {
			panic(err)
		}
	}
	fmt.Println("Prepared current EasyAgent core and native computer helper")
}
