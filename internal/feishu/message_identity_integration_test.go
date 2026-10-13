package feishu_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/internal/feishu"
	"github.com/hwj123hwj/easyagent/internal/server"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/require"
)

func TestInboundRetryRestoresReceiptAfterCoreRestartAndNewSession(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"saved result\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	cfg := config.Default()
	cfg.DataDir, cfg.Workspace = t.TempDir(), t.TempDir()
	cfg.MCPConfigPath = filepath.Join(cfg.DataDir, "mcp.json")
	cfg.Provider, cfg.OpenAIAPIKey, cfg.OpenAIBaseURL = "openai", "fake", provider.URL
	open := func() (*app.App, *server.Server, *httptest.Server) {
		a, err := app.New(app.AppOptions{Config: cfg})
		require.NoError(t, err)
		s := server.New(a, nil)
		s.SetAPIKey("test-key")
		endpoint := httptest.NewServer(s.Handler())
		t.Cleanup(func() { endpoint.Close(); a.Close() })
		return a, s, endpoint
	}
	first, core, endpoint := open()
	session, err := first.NewSession(context.Background())
	require.NoError(t, err)
	text, err := feishu.StreamInboundChatForTest(context.Background(), endpoint.URL, "test-key", "bot-a", "message-a", session.SessionID(), "task", endpoint.Client())
	require.NoError(t, err)
	require.Equal(t, "saved result", text)
	waitForCoreRunCleanup(t, core)
	endpoint.Close()
	require.NoError(t, first.Close())
	second, core, endpoint := open()
	newSession, err := second.NewSession(context.Background())
	require.NoError(t, err)
	text, err = feishu.StreamInboundChatForTest(context.Background(), endpoint.URL, "test-key", "bot-a", "message-a", newSession.SessionID(), "task", endpoint.Client())
	require.NoError(t, err)
	require.Equal(t, "saved result", text)
	require.EqualValues(t, 1, calls.Load(), "重投只回放旧凭据，不能在 /new 会话重新调用模型")
	_, err = feishu.StreamInboundChatForTest(context.Background(), endpoint.URL, "test-key", "bot-a", "message-a", newSession.SessionID(), "changed task", endpoint.Client())
	require.ErrorContains(t, err, "HTTP 409")
	require.EqualValues(t, 1, calls.Load(), "相同身份的内容变更必须拒绝")
	waitForCoreRunCleanup(t, core)
}
