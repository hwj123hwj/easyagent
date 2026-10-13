package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/stretchr/testify/require"
)

func TestInboundMessageIdentitySurvivesBridgeRestartAndReplyFallback(t *testing.T) {
	bodies := make(chan map[string]string, 8)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: done\ndata: {\"final_message\":{\"text\":\"done\"}}\n\n")
	}))
	defer core.Close()
	mockHTTP := &http.Client{Transport: cardRoundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "token") {
			return cardResponse(`{"code":0,"tenant_access_token":"test-token","expire":3600}`), nil
		}
		return cardResponse(`{"code":0,"data":{"message_id":"outbound"}}`), nil
	})}
	newHandler := func(appID string) *Handler {
		return &Handler{appID: appID, piAgentURL: core.URL, httpClient: core.Client(), client: &Client{sdk: lark.NewClient(appID, "fake", lark.WithHttpClient(mockHTTP))}}
	}
	card := testCard(func(*http.Request) (*http.Response, error) { return cardResponse(`{"code":0}`), nil })
	newHandler("bot-a").handleWithCard(context.Background(), "chat-a", "message-a", "session-a", "task", card)
	first := <-bodies
	require.NotEmpty(t, first["request_id"], "入站 message ID 必须传到核心的持久化幂等边界")
	newHandler("bot-a").handleWithTextFallback(context.Background(), "chat-a", "message-a", "session-a", "task")
	require.Equal(t, first["request_id"], (<-bodies)["request_id"], "桥接重启和回复方式变化不产生新任务")
	newHandler("bot-b").handleWithCard(context.Background(), "chat-a", "message-a", "session-a", "task", card)
	require.NotEqual(t, first["request_id"], (<-bodies)["request_id"], "不同应用独立命名空间")
	newHandler("bot-a").handleWithCard(context.Background(), "chat-a", "", "session-a", "task", card)
	require.Empty(t, (<-bodies)["request_id"], "系统生成的新任务交给核心生成新身份")
}
