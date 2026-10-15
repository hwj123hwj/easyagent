package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/mcp"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestRunReceiptsSurviveCoreRestartWithoutRepeatedToolEffects(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupted=%t", interrupted), func(t *testing.T) {
			var toolCalls, providerCalls atomic.Int32
			mcpServer := protocol.NewServer(&protocol.Implementation{Name: "counter", Version: "1"}, nil)
			mcpServer.AddTool(&protocol.Tool{Name: "increment", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
				toolCalls.Add(1)
				return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: "effect committed"}}}, nil
			})
			toolServer := httptest.NewServer(protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return mcpServer }, nil))
			t.Cleanup(func() { toolServer.CloseClientConnections(); toolServer.Close() })
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"data":[]}`)
					return
				}
				providerCalls.Add(1)
				var request struct {
					Messages []struct {
						Role string `json:"role"`
					} `json:"messages"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				hasResult := false
				for _, message := range request.Messages {
					hasResult = hasResult || message.Role == "tool"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if hasResult {
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"increment-1\",\"type\":\"function\",\"function\":{\"name\":\"mcp__counter__increment\",\"arguments\":\"{}\"}}]}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			t.Cleanup(provider.Close)
			cfg := config.Default()
			dir := t.TempDir()
			cfg.DataDir, cfg.Workspace, cfg.MCPConfigPath = filepath.Join(dir, "data"), dir, filepath.Join(dir, "mcp.json")
			cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
			data, err := json.Marshal(map[string]any{"mcpServers": map[string]mcp.MCPServerConfig{"counter": {URL: toolServer.URL, Trust: true}}})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(cfg.MCPConfigPath, data, 0600))
			firstApp, err := app.New(app.AppOptions{Config: cfg})
			require.NoError(t, err)
			first := New(firstApp, nil)
			t.Cleanup(func() { first.cancel(); firstApp.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			run, _, err := first.startRun("", "perform one effect", "durable-request")
			require.NoError(t, err)
			result, err := first.waitRun(ctx, run)
			require.NoError(t, err)
			require.Equal(t, "done", result.Text)
			require.EqualValues(t, 1, toolCalls.Load())
			require.EqualValues(t, 2, providerCalls.Load())
			info, err := os.Stat(first.receiptPath(run.RequestID))
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			if interrupted {
				// Simulate the last durable state before an abrupt process loss: the
				// effect may have committed while the terminal receipt was not written.
				require.NoError(t, first.saveRunReceipt(&sessionRun{ID: run.ID, RequestID: run.RequestID, SessionID: run.SessionID, Prompt: run.Prompt, State: "waiting_confirmation", StartedAt: run.StartedAt, last: ai.AssistantMessage{}}))
			}
			first.cancel()
			require.NoError(t, firstApp.Close())
			secondApp, err := app.New(app.AppOptions{Config: cfg})
			require.NoError(t, err)
			second := New(secondApp, nil)
			t.Cleanup(func() { second.cancel(); secondApp.Close() })
			retried, duplicate, err := second.startRun(run.SessionID, run.Prompt, run.RequestID)
			require.NoError(t, err)
			require.True(t, duplicate)
			require.Equal(t, run.ID, retried.ID)
			if interrupted {
				require.Equal(t, "interrupted", retried.State)
				_, err = second.waitRun(ctx, retried)
				require.ErrorContains(t, err, "服务重启")
			} else {
				require.Equal(t, "completed", retried.State)
				result, err = second.waitRun(ctx, retried)
				require.NoError(t, err)
				require.Equal(t, "done", result.Text)
			}
			require.EqualValues(t, 1, toolCalls.Load(), "a restarted core must not repeat an accepted tool effect")
			require.EqualValues(t, 2, providerCalls.Load())
			snapshot, err := second.runSnapshot(run.SessionID, 100, run.ID, nil)
			require.NoError(t, err)
			require.True(t, snapshot.Reset, "a core restart explicitly resets the event cursor")
			require.Empty(t, snapshot.Run.Prompt, "authoritative persisted history already contains the user prompt")
			require.NotEmpty(t, snapshot.Messages)
			require.Empty(t, snapshot.Events)
			body, err := json.Marshal(ChatRequest{SessionID: run.SessionID, Prompt: run.Prompt, RequestID: run.RequestID})
			require.NoError(t, err)
			response := httptest.NewRecorder()
			second.Handler().ServeHTTP(response, localReq(http.MethodPost, "/chat/stream", strings.NewReader(string(body))))
			require.Equal(t, http.StatusOK, response.Code)
			if interrupted {
				require.Contains(t, response.Body.String(), "服务重启")
			} else {
				require.Contains(t, response.Body.String(), `"text":"done"`, "SSE duplicates return the persisted final result")
			}
			require.EqualValues(t, 1, toolCalls.Load())
			_, _, err = second.startRun(run.SessionID, "changed effect", run.RequestID)
			require.ErrorContains(t, err, "request_id")
		})
	}
}
