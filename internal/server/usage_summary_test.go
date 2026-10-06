package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/require"
)

func TestUsageSummaryAggregatesTokens(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	dir := s.app.Config().DataDir
	reqDir := filepath.Join(dir, "requests")
	require.NoError(t, os.MkdirAll(reqDir, 0o755))

	now := time.Now()
	cached100 := 100
	receipts := []runReceipt{
		{
			RunID:     "r1",
			RequestID: "req1",
			SessionID: "sess-a",
			State:     "completed",
			StartedAt: now.Add(-2 * time.Hour),
			Usage:     ai.Usage{InputTokens: 500, OutputTokens: 150, CachedInputTokens: &cached100},
		},
		{
			RunID:     "r2",
			RequestID: "req2",
			SessionID: "sess-a",
			State:     "completed",
			StartedAt: now.Add(-1 * time.Hour),
			Usage:     ai.Usage{InputTokens: 300, OutputTokens: 80},
		},
		{
			RunID:     "r3",
			RequestID: "req3",
			SessionID: "sess-b",
			State:     "completed",
			StartedAt: now.Add(-30 * time.Minute),
			Usage:     ai.Usage{InputTokens: 1000, OutputTokens: 400},
		},
	}
	for _, r := range receipts {
		data, err := json.Marshal(r)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(reqDir, r.RequestID+".json"), data, 0o644))
	}

	req := httptest.NewRequest("GET", "/usage/summary", nil)
	rec := httptest.NewRecorder()
	s.usageSummary(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp usageSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Total verification
	require.Equal(t, 3, resp.Total.RunCount)
	require.Equal(t, 1800, resp.Total.InputTokens)
	require.Equal(t, 630, resp.Total.OutputTokens)
	require.NotNil(t, resp.Total.CachedInputTokens)
	require.Equal(t, 100, *resp.Total.CachedInputTokens)

	// Per-session ordering: sess-b (1400 tokens) > sess-a (1030 tokens)
	require.Len(t, resp.Sessions, 2)
	require.Equal(t, "sess-b", resp.Sessions[0].SessionID)
	require.Equal(t, 1, resp.Sessions[0].RunCount)
	require.Equal(t, 1000, resp.Sessions[0].InputTokens)
	require.Equal(t, 400, resp.Sessions[0].OutputTokens)

	require.Equal(t, "sess-a", resp.Sessions[1].SessionID)
	require.Equal(t, 2, resp.Sessions[1].RunCount)
	require.Equal(t, 800, resp.Sessions[1].InputTokens)
	require.Equal(t, 230, resp.Sessions[1].OutputTokens)
	require.Equal(t, 100, *resp.Sessions[1].CachedInputTokens)
}

func TestAddTurnUsageAccumulatesCachedTokens(t *testing.T) {
	total := ai.Usage{}
	cached1, cached3 := 100, 40
	addTurnUsage(&total, ai.Usage{InputTokens: 500, OutputTokens: 150, CachedInputTokens: &cached1})
	addTurnUsage(&total, ai.Usage{InputTokens: 300, OutputTokens: 80})
	addTurnUsage(&total, ai.Usage{InputTokens: 700, OutputTokens: 200, CachedInputTokens: &cached3})
	require.Equal(t, 1500, total.InputTokens)
	require.Equal(t, 430, total.OutputTokens)
	require.NotNil(t, total.CachedInputTokens)
	require.Equal(t, 140, *total.CachedInputTokens)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

// 端到端：自建 provider 让一次 run 跑两轮（write 工具循环），每轮 SSE 末尾
// 带独立 usage 块。验证 run 级累计等于两轮之和且 Done 不重复计末轮，收据
// 落盘同一数值，/usage/summary 能聚合到。
func TestRunUsageAccumulatesAcrossTurnsIntoReceiptAndSummary(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	notePath := filepath.Join(t.TempDir(), "usage-note.txt")
	toolChunk := fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"write-1","type":"function","function":{"name":"write","arguments":%q}}]},"finish_reason":"tool_calls"}]}`,
		mustJSON(t, map[string]string{"path": notePath, "content": "hi"}))
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		mu.Lock()
		calls++
		turn := calls
		mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		messages := body["messages"].([]any)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `{"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`
		if messages[len(messages)-1].(map[string]any)["role"] != "tool" {
			chunk = toolChunk
		}
		usage := fmt.Sprintf(`"usage":{"prompt_tokens":%d,"completion_tokens":%d,"prompt_tokens_details":{"cached_tokens":30}}`, turn*100, turn*10)
		fmt.Fprintf(w, "data: %s\n\ndata: {%s}\n\ndata: [DONE]\n\n", chunk, usage)
	}))
	defer provider.Close()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Workspace = filepath.Dir(notePath)
	cfg.AutoApprove = true
	cfg.MCPConfigPath = filepath.Join(cfg.DataDir, "mcp.json")
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "test-key", provider.URL
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	srv := New(application, nil)
	t.Cleanup(func() {
		srv.cancel()
		srv.runs.mu.Lock()
		var done []<-chan struct{}
		for _, run := range srv.runs.requests {
			done = append(done, run.done)
		}
		srv.runs.mu.Unlock()
		for _, channel := range done {
			select {
			case <-channel:
			case <-time.After(3 * time.Second):
				t.Error("run did not shut down")
			}
		}
		application.Close()
	})

	run, _, err := srv.startRun("", "write the note", "req-usage-e2e")
	require.NoError(t, err)
	_, err = srv.waitRun(context.Background(), run)
	require.NoError(t, err)
	mu.Lock()
	require.Equal(t, 2, calls)
	mu.Unlock()

	// 轮次用量 100/10 + 200/20；若 Done 把末轮重复计入会变成 500/40。
	require.Equal(t, 300, run.Usage.InputTokens)
	require.Equal(t, 30, run.Usage.OutputTokens)
	require.NotNil(t, run.Usage.CachedInputTokens)
	require.Equal(t, 60, *run.Usage.CachedInputTokens)

	data, err := os.ReadFile(srv.receiptPath("req-usage-e2e"))
	require.NoError(t, err)
	var receipt runReceipt
	require.NoError(t, json.Unmarshal(data, &receipt))
	require.Equal(t, 300, receipt.Usage.InputTokens)
	require.Equal(t, 30, receipt.Usage.OutputTokens)

	req := httptest.NewRequest("GET", "/usage/summary", nil)
	rec := httptest.NewRecorder()
	srv.usageSummary(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp usageSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Total.RunCount)
	require.Equal(t, 300, resp.Total.InputTokens)
	require.Equal(t, 30, resp.Total.OutputTokens)
}
