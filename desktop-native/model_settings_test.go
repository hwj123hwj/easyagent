package main

import (
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo/ui"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGlobalModelSettingsLayoutAndEditing(t *testing.T) {
	for _, width := range []int{900, 1240} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := alignmentPreview(t)
			a.Settings.Open = true
			a.Settings.Tab = "models"
			a.Live.Data["models"] = map[string]any{"current": map[string]any{"id": "global-default"}}
			a.current().Model = "session-override"
			tt := ui.NewTester(a.View, width, 820)
			for _, text := range []string{"全局模型配置", "主机默认模型", "global-default", "当前会话模型", "session-override"} {
				if !tt.HasText(text) {
					t.Fatal("missing context", text)
				}
			}
			click(t, tt, "配置全局模型")
			if a.Settings.Model != "global-default" {
				t.Fatal("default was copied from session instead of host")
			}
			if _, ok := tt.Find("全局模型服务地址"); !ok {
				t.Fatal("missing global connection form")
			}
			a.Live.Loading["model-save"] = true
			tt.Frame()
			click(t, tt, "全局默认模型")
			tt.Type("must-not-edit")
			if a.Settings.Model != "global-default" {
				t.Fatal("busy form still editable")
			}
			a.Live.Loading["model-save"] = false
			tt.Frame()
			a.Live.Data["model-check"] = map[string]any{"message": "old catalog", "data": []any{map[string]any{"id": "old-model"}}}
			click(t, tt, "全局模型服务地址")
			tt.Type("http://localhost:9999")
			tt.Frame()
			if a.Live.Data["model-check"] != nil {
				t.Fatalf("stale catalog survived URL edit: url=%q", a.Settings.BaseURL)
			}
		})
	}
}

func TestModelCatalogCanBeDiscoveredBeforeChoosingModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"catalog-choice"}]}`)
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.Settings.Mode = "在桌面客户端配置"
	a.Settings.Model = ""
	a.Settings.BaseURL = srv.URL
	a.Settings.APIKey = "fixture-key"
	a.checkLiveModel()
	waitNative(t, a, func() bool { return !a.Live.Loading["model-check"] })
	if a.Live.Error != "" || len(list(object(a.Live.Data["model-check"])["data"])) != 1 {
		t.Fatal("discovery required a model", a.Live.Error)
	}
	if a.Settings.Model != "" {
		t.Fatal("discovery changed default without selection")
	}
}

func TestNewSessionUsesHostDefaultNotPreviousOverride(t *testing.T) {
	for _, scenario := range []string{"existing", "blank", "chosen"} {
		t.Run(scenario, func(t *testing.T) {
			request := make(chan map[string]any, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && r.URL.Path == "/sessions" {
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					request <- body
					fmt.Fprint(w, `{"id":"new-session"}`)
					return
				}
				fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()
			a := alignmentPreview(t)
			a.Live.Client, _ = newBackendClient(srv.URL, "")
			a.current().Model = "previous-override"
			if scenario != "existing" {
				a.current().RemoteID = ""
			}
			if scenario == "chosen" {
				a.switchLiveModel("chosen-model")
			}
			a.createLiveSession("")
			body := <-request
			expected := ""
			if scenario == "chosen" {
				expected = "chosen-model"
			}
			if str(body["model"]) != expected {
				t.Fatal("incorrect model for new session", body["model"])
			}
			waitNative(t, a, func() bool { return !a.Live.Loading["create-session"] })
		})
	}
}

func TestGlobalModelApplyWithRealCore(t *testing.T) {
	a, provider, _, calls := nativeCoreFixture(t)
	a.createLiveSession("")
	waitNative(t, a, func() bool { return a.current().RemoteID != "" && a.current().Model != "" })
	id := a.current().RemoteID
	a.switchLiveModel("fixture-other")
	waitNative(t, a, func() bool { return a.current().Model == "fixture-other" })
	a.Settings.Mode = "在桌面客户端配置"
	a.Settings.Provider = "OpenAI 兼容服务 / 网关"
	a.Settings.BaseURL = provider.URL
	a.Settings.Model = "fixture-global"
	a.Settings.APIKey = "fixture-key"
	a.applyLiveModel()
	waitNative(t, a, func() bool {
		return a.Live.Config.Model.Model == "fixture-global" && a.Live.Connected && !a.Live.Connecting
	})
	a.refreshLiveSessions(id)
	waitNative(t, a, func() bool { return a.current().RemoteID == id && a.current().Model == "fixture-other" })
	a.createLiveSession("")
	waitNative(t, a, func() bool { return a.current().RemoteID != id && a.current().Model == "fixture-global" })
	if calls.Load() != 0 {
		t.Fatal("configuration triggered inference")
	}
}

func TestModelDiscoveryDropsResultsAfterLeavingSettings(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"data":[{"id":"stale"}]}`)
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.Settings.Tab = "models"
	a.Settings.Mode = "在桌面客户端配置"
	a.Settings.BaseURL = srv.URL
	a.Settings.APIKey = "fixture-key"
	a.checkLiveModel()
	<-started
	a.closeSettings()
	close(release)
	waitNative(t, a, func() bool { return !a.Live.Loading["model-check"] })
	if a.Live.Data["model-check"] != nil {
		t.Fatal("late model discovery repopulated closed settings")
	}
}
