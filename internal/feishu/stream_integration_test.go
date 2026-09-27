package feishu_test

import (
	"context"
	"fmt"
	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/internal/feishu"
	"github.com/hwj123hwj/easyagent/internal/server"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise the actual outer HTTP mux through the bridge, not just a mock SSE
// endpoint. Missing /chat/ routing used to silently produce an empty final card.
func TestStreamChatThroughCoreHandler(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"reply\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"在的，可以正常回复。\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"reply\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer llm.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Workspace = t.TempDir()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "fake"
	cfg.OpenAIBaseURL = llm.URL + "/v1"
	cfg.OpenAIModel = "test-model"
	application, err := app.New(app.AppOptions{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	core := server.New(application, nil)
	core.SetAPIKey("test-key")
	endpoint := httptest.NewServer(core.Handler())
	defer endpoint.Close()
	text, err := feishu.StreamChatForTest(context.Background(), endpoint.URL, "test-key", "在吗", endpoint.Client())
	if err != nil {
		t.Fatal(err)
	}
	if text != "在的，可以正常回复。" {
		t.Fatalf("lost reply: %q", text)
	}
	card := feishu.BuildFinalCard(text, &feishu.FooterMetrics{Status: "已完成"})
	content := card["body"].(map[string]any)["elements"].([]any)[0].(map[string]any)["content"]
	if content != text {
		t.Fatalf("card lost reply: %v", content)
	}
}
