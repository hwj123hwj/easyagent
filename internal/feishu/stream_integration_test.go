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
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	cfg.MCPConfigPath = filepath.Join(cfg.DataDir, "mcp.json")
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

// A finite short response alone cannot prove streaming: net/http flushes it when
// the handler returns. Hold the provider open until each card update arrives.
func TestCardReceivesProgressBeforeProviderCompletes(t *testing.T) {
	firstRead, secondRead := make(chan struct{}), make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		delta := func(text string) {
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", text)
			w.(http.Flusher).Flush()
		}
		delta("第一段")
		select {
		case <-firstRead:
		case <-r.Context().Done():
			return
		}
		delta("，第二段")
		select {
		case <-secondRead:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer llm.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.MCPConfigPath = filepath.Join(cfg.DataDir, "mcp.json")
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan string, 8)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		text, err := feishu.StreamChatWithUpdatesForTest(ctx, endpoint.URL, "test-key", "hello", endpoint.Client(), func(text string) { updates <- text })
		if err == nil && text != "第一段，第二段" {
			err = fmt.Errorf("lost final text: %q", text)
		}
		done <- err
	}()
	for i, want := range []string{"第一段", "第一段，第二段"} {
		select {
		case got := <-updates:
			if got != want {
				t.Fatalf("progress %d = %q; want %q", i, got, want)
			}
		case err := <-done:
			t.Fatalf("finished before progress %d: %v", i, err)
		case <-time.After(2 * time.Second):
			t.Fatalf("progress %d was buffered until completion", i)
		}
		t.Logf("card progress %d before provider completion: %s", i, time.Since(start))
		if i == 0 {
			close(firstRead)
		} else {
			close(secondRead)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completion was not delivered")
	}
}
