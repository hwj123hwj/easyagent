package feishu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamChatRejectsInvalidResponses(t *testing.T) {
	cases := []struct {
		name                             string
		status                           int
		contentType, body, want, errPart string
	}{
		{"404", 404, "text/plain", "404 page not found", "", "HTTP 404"},
		{"unauthorized", 401, "application/json", `{"error":"unauthorized"}`, "", "HTTP 401"},
		{"server error", 500, "text/plain", "error", "", "HTTP 500"},
		{"html fallback", 200, "text/html", "<html>app</html>", "", "not an event stream"},
		{"empty stream", 200, "text/event-stream", "", "", "before completion"},
		{"truncated", 200, "text/event-stream", "event: text_delta\ndata: {\"text_delta\":\"partial\"}\n\n", "partial", "before completion"},
		{"empty completion", 200, "text/event-stream", "event: done\ndata: {}\n\n", "", "without reply text"},
		{"agent error", 200, "text/event-stream", "event: error\ndata: {\"error\":\"busy\"}\n\n", "", "agent error"},
		{"deltas", 200, "text/event-stream; charset=utf-8", "event: text_delta\ndata: {\"text_delta\":\"你好\"}\n\nevent: text_delta\ndata: {\"text_delta\":\"！\"}\n\nevent: done\ndata: {\"final_message\":{\"text\":\"你好！\"}}\n\n", "你好！", ""},
		{"final only", 200, "text/event-stream", "event:done\ndata:{\"final_message\":{\"text\":\"最终回复\"}}\n\n", "最终回复", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/stream" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("incorrect request")
				}
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer upstream.Close()
			h := &Handler{piAgentURL: upstream.URL, piAgentAPIKey: "test-key", httpClient: upstream.Client()}
			got, err := h.streamChat(context.Background(), "test-session", "在吗", nil)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if tt.errPart == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.errPart) {
				t.Fatalf("expected %q, got %v", tt.errPart, err)
			}
		})
	}
}
