package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
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
