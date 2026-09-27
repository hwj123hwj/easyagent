package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/agent"
)

func TestFileToolPathPolicy(t *testing.T) {
	for _, allow := range []bool{false, true} {
		for _, style := range []string{"absolute", "relative", "symlink"} {
			for _, name := range []string{"read", "write", "edit", "grep", "find", "ls", "multiedit", "patch", "delete_file", "read_many_files"} {
				t.Run(name+"/"+style+"/"+map[bool]string{false: "restricted", true: "outside"}[allow], func(t *testing.T) {
					root := t.TempDir()
					ws := filepath.Join(root, "workspace")
					outside := filepath.Join(root, "outside")
					for _, d := range []string{ws, outside} {
						if err := os.MkdirAll(d, 0700); err != nil {
							t.Fatal(err)
						}
					}
					target := filepath.Join(outside, "sample.txt")
					if err := os.WriteFile(target, []byte("policy-marker\n"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
						t.Fatal(err)
					}
					path := target
					dir := outside
					if style == "relative" {
						path = "../outside/sample.txt"
						dir = "../outside"
					}
					if style == "symlink" {
						path = "link/sample.txt"
						dir = "link"
					}
					args := map[string]any{"path": path}
					switch name {
					case "write":
						args["content"] = "changed\n"
					case "edit":
						args["old_string"] = "policy-marker"
						args["new_string"] = "changed"
					case "grep":
						args["pattern"] = "policy-marker"
					case "find", "ls":
						args["path"] = dir
					case "multiedit":
						args = map[string]any{"file_path": path, "edits": []map[string]any{{"old_string": "policy-marker", "new_string": "changed"}}}
					case "patch":
						args = map[string]any{"patchText": "--- " + path + "\n+++ " + path + "\n@@ -1 +1 @@\n-policy-marker\n+changed\n"}
					case "delete_file":
						args = map[string]any{"file_path": path}
					case "read_many_files":
						args = map[string]any{"paths": []string{path}, "allowLocalExecution": true}
					}
					var tool agent.Tool
					for _, x := range BuildList(ListOptions{FileMutationQueue: NewFileMutationQueue(), Workspace: ws, AllowOutsideWorkspace: allow}) {
						if x.Name() == name {
							tool = x
						}
					}
					raw, _ := json.Marshal(args)
					raw, err := tool.Validate(raw)
					if err != nil {
						t.Fatal(err)
					}
					result, err := tool.Execute(context.Background(), raw, nil)
					if allow {
						if err != nil || result.IsError {
							t.Fatalf("outside mode failed: %v %+v", err, result)
						}
						if name == "read_many_files" && !strings.Contains(result.Content, "policy-marker") {
							t.Fatal(result.Content)
						}
					} else {
						if name == "read_many_files" {
							if strings.Contains(result.Content, "policy-marker") {
								t.Fatal("argument bypassed policy")
							}
						} else if err == nil && !result.IsError {
							t.Fatal("outside path accepted")
						}
						data, e := os.ReadFile(target)
						if e != nil || string(data) != "policy-marker\n" {
							t.Fatal("restricted tool modified file")
						}
					}
				})
			}
		}
	}
}

func TestOutsidePolicyPreservesWorkspace(t *testing.T) {
	ws := t.TempDir()
	for _, tool := range BuildList(ListOptions{FileMutationQueue: NewFileMutationQueue(), Workspace: ws, AllowOutsideWorkspace: true}) {
		if tool.Name() != "write" {
			continue
		}
		raw, _ := json.Marshal(map[string]any{"path": "cwd.txt", "content": "workspace"})
		if _, err := tool.Execute(context.Background(), raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(ws, "cwd.txt")); err != nil || string(data) != "workspace" {
		t.Fatal("workspace changed", err)
	}
}
