package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamChatReportsWaitingForServerApprovalWithoutExposingArguments(t *testing.T) {
	for _, test := range []struct {
		name, toolName string
		showTool       bool
	}{
		{"named tool", "mcp__test__write", true},
		{"unsafe markup", "</font>\n@all [private](https://example.invalid)", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			waiting := make(chan string, 1)
			card := testCard(func(r *http.Request) (*http.Response, error) {
				var request struct {
					Card struct {
						Data string `json:"data"`
					} `json:"card"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					return nil, err
				}
				var cardData struct {
					Body struct {
						Elements []struct {
							Content string `json:"content"`
						} `json:"elements"`
					} `json:"body"`
				}
				if err := json.Unmarshal([]byte(request.Card.Data), &cardData); err != nil {
					return nil, err
				}
				if len(cardData.Body.Elements) != 3 {
					return nil, fmt.Errorf("unexpected progress card elements")
				}
				footer := cardData.Body.Elements[2].Content
				if strings.Contains(footer, "等待工具审批，请在网页或桌面批准") {
					if cardData.Body.Elements[0].Content != "正在准备" {
						t.Errorf("approval status lost existing reply text")
					}
					select {
					case waiting <- footer:
					default:
					}
				}
				if strings.Contains(request.Card.Data, "fake-secret-argument") || strings.Contains(request.Card.Data, "fake-private-description") {
					t.Error("approval progress exposed private arguments or description")
				}
				return cardResponse(`{"code":0}`), nil
			})
			observed := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/stream" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("incorrect authenticated core request")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: text_delta\ndata: {\"text_delta\":\"正在准备\"}\n\n")
				confirmation, _ := json.Marshal(map[string]any{"confirmation_id": "server-owned", "tool_name": test.toolName, "args": map[string]string{"token": "fake-secret-argument"}, "description": "fake-private-description"})
				fmt.Fprintf(w, "event: confirmation\ndata: %s\n\n", confirmation)
				w.(http.Flusher).Flush()
				// Approval progress must reach the card while the core is waiting,
				// rather than only appearing after the stream has completed.
				select {
				case footer := <-waiting:
					observed <- footer
				case <-r.Context().Done():
					return
				}
				fmt.Fprint(w, "event: text_delta\ndata: {\"text_delta\":\"，审批后完成\"}\n\nevent: done\ndata: {}\n\n")
			}))
			defer upstream.Close()
			handler := &Handler{piAgentURL: upstream.URL, piAgentAPIKey: "test-key", httpClient: upstream.Client()}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			text, err := handler.streamChat(ctx, "test-session", "prompt", card)
			if err != nil {
				t.Fatal(err)
			}
			if text != "正在准备，审批后完成" {
				t.Fatalf("lost response: %q", text)
			}
			footer := awaitCard(t, observed)
			if test.showTool && !strings.Contains(footer, "`"+test.toolName+"`") {
				t.Fatalf("safe tool name missing: %s", footer)
			}
			if !test.showTool && (strings.Contains(footer, "@all") || strings.Contains(footer, "example.invalid") || strings.Contains(footer, "</font>\n")) {
				t.Fatalf("unsafe tool name reached the footer: %s", footer)
			}
		})
	}
}
