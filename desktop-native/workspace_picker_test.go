package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func workspacePickerFixture(t *testing.T) (*preview, <-chan map[string]any) {
	t.Helper()
	requests := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/sessions" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			requests <- body
			fmt.Fprint(w, `{"id":"chosen-project"}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	a := alignmentPreview(t)
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	a.Live.Connected = true
	a.Live.Config.Workspace = "/previous/default"
	a.current().Draft = "保留旧会话草稿"
	return a, requests
}

func TestNativeLocalFolderPickerCreatesOnlySelectedProject(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			a, requests := workspacePickerFixture(t)
			old := a.current()
			originalConfig := cloneConfig(a.Live.Config)
			var picks atomic.Int32
			options := make(chan mygo.OpenDialogOptions, 2)
			a.Live.PickDirectory = func(o mygo.OpenDialogOptions) ([]string, error) {
				picks.Add(1)
				options <- o
				if cancel {
					return nil, nil
				}
				return []string{"/projects/new project"}, nil
			}
			tt := ui.NewTester(a.View, 1240, 820)
			click(t, tt, "选择项目目录")
			a.openWorkspacePicker()
			if a.Live.NewWorkspace {
				t.Fatal("local folder opens an extra form")
			}
			got := <-options
			if !got.Directory || got.Multiple || got.ButtonLabel != "打开" {
				t.Fatalf("wrong native picker: %+v", got)
			}
			waitNative(t, a, func() bool {
				if cancel {
					return !a.Live.Loading["folder-picker"]
				}
				return a.current().RemoteID == "chosen-project"
			})
			if picks.Load() != 1 {
				t.Fatal("duplicate picker")
			}
			if cancel {
				select {
				case <-requests:
					t.Fatal("cancel created a session")
				default:
				}
				if a.current() != old {
					t.Fatal("cancel changed active session")
				}
			} else {
				body := <-requests
				if str(body["cwd"]) != "/projects/new project" || str(body["application"]) != "" {
					t.Fatalf("wrong project: %v", body)
				}
				if a.current().Draft != "" {
					t.Fatal("new project copied an existing session draft")
				}
			}
			if old.Workspace != "/demo/easyagent" || old.Draft != "保留旧会话草稿" || a.Live.Config.Workspace != originalConfig.Workspace || a.Live.Config.Hosts[0].Workspace != originalConfig.Hosts[0].Workspace {
				t.Fatal("folder selection altered an existing workspace or draft")
			}
			if _, err := os.Stat(filepath.Join(a.Live.Dir, "connections.json")); !os.IsNotExist(err) {
				t.Fatal("folder selection rewrote host configuration")
			}
		})
	}
}

func TestNativeRemoteWorkspaceFormAndCancellation(t *testing.T) {
	for _, width := range []int{1240, 900} {
		for _, theme := range []string{"dark", "light"} {
			t.Run(fmt.Sprintf("%d-%s", width, theme), func(t *testing.T) {
				a, requests := workspacePickerFixture(t)
				a.Live.Config.Hosts = append(a.Live.Config.Hosts, hostConfig{ID: "remote", Name: "开发主机", URL: a.Live.Client.URL})
				a.Live.Config.ActiveHost = "remote"
				a.Settings.Hosts = append(a.Settings.Hosts[:1], demoHost{ID: "remote", Name: "开发主机"})
				a.Settings.Host = 1
				a.Settings.Preferences.Theme = theme
				var picks atomic.Int32
				a.Live.PickDirectory = func(mygo.OpenDialogOptions) ([]string, error) { picks.Add(1); return nil, nil }
				tt := ui.NewTester(a.View, width, 820)
				click(t, tt, "选择项目目录")
				if !a.Live.NewWorkspace || !tt.HasText("选择远程项目") || picks.Load() != 0 {
					t.Fatal("remote picker opened this Mac's filesystem")
				}
				if tt.HasText("浏览目录") || tt.HasText("选择并新建会话") {
					t.Fatal("old intermediate controls remain")
				}
				if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
					saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("remote-folder-%d-%s.png", width, theme)))
				}
				tt.Type("relative/project")
				tt.Key(0, ui.KeyEnter)
				if !a.Live.NewWorkspace || !tt.HasText("请填写绝对路径") {
					t.Fatal("relative path accepted")
				}
				select {
				case <-requests:
					t.Fatal("invalid path sent to server")
				default:
				}
				tt.Key(0, ui.KeyEscape)
				if a.Live.NewWorkspace {
					t.Fatal("Escape did not cancel")
				}
				tt.Frame()
				if !tt.Focused("选择项目目录") {
					t.Fatal("cancel lost opener focus")
				}
				click(t, tt, "选择项目目录")
				tt.Type("  /home/q/projects/new project  ")
				tt.Key(0, ui.KeyEnter)
				waitNative(t, a, func() bool { return a.current().RemoteID == "chosen-project" })
				body := <-requests
				if str(body["cwd"]) != "/home/q/projects/new project" || a.Live.NewWorkspace {
					t.Fatalf("wrong remote folder: %v", body)
				}
			})
		}
	}
}

func TestNativeNewSessionModeKeepsCurrentProject(t *testing.T) {
	a, requests := workspacePickerFixture(t)
	tt := ui.NewTester(a.View, 1240, 820)
	click(t, tt, "选择模式")
	click(t, tt, "音乐")
	waitNative(t, a, func() bool { return a.current().RemoteID == "chosen-project" })
	body := <-requests
	if str(body["application"]) != "music" || str(body["cwd"]) != "/demo/easyagent" {
		t.Fatalf("mode changed current project: %v", body)
	}
}

func TestNativeWorkspacePathFormats(t *testing.T) {
	for _, path := range []string{"/home/q/projects/a", " /project with spaces ", `C:\Users\me\project`, `D:/projects/app`} {
		if !absoluteWorkspace(path) {
			t.Errorf("valid path rejected: %s", path)
		}
	}
	for _, path := range []string{"", "  ", "../project", "~/project", "C:project", "https://host/project"} {
		if absoluteWorkspace(path) {
			t.Errorf("relative path accepted: %s", path)
		}
	}
}

func TestNativeCoreFolderSelectionKeepsPreviousSession(t *testing.T) {
	a, _, previousWorkspace, _ := nativeCoreFixture(t)
	a.createLiveSession("")
	waitNative(t, a, func() bool { return a.current().RemoteID != "" })
	previous := a.current()
	previous.Draft = "仍在编辑的内容"
	target := t.TempDir()
	a.Live.PickDirectory = func(mygo.OpenDialogOptions) ([]string, error) { return []string{target}, nil }
	a.openWorkspacePicker()
	waitNative(t, a, func() bool { return a.current().RemoteID != previous.RemoteID && a.current().Workspace == target })
	if previous.Workspace != previousWorkspace || previous.Draft != "仍在编辑的内容" {
		t.Fatal("opening another folder changed the old session")
	}
	var info map[string]any
	if err := a.Live.Client.request(a.Live.ctx, "GET", sessionPath(a.current().RemoteID)+"/info", nil, &info); err != nil {
		t.Fatal(err)
	}
	if str(info["workspace"]) != target {
		t.Fatalf("backend did not use selected folder: %v", info["workspace"])
	}
}
