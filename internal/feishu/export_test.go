package feishu

import (
	"context"
	"net/http"
)

// StreamChatForTest lets an external test exercise core -> bridge -> card
// without introducing the application's dependency cycle or contacting Feishu.
func StreamChatForTest(ctx context.Context, url, key, prompt string, client *http.Client) (string, error) {
	h := &Handler{piAgentURL: url, piAgentAPIKey: key, httpClient: client}
	return h.streamChat(ctx, "", prompt, nil)
}
