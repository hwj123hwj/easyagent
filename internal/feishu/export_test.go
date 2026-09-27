package feishu

import (
	"context"
	"encoding/json"
	"net/http"
)

// StreamChatForTest lets an external test exercise core -> bridge -> card
// without introducing the application's dependency cycle or contacting Feishu.
func StreamChatForTest(ctx context.Context, url, key, prompt string, client *http.Client) (string, error) {
	h := &Handler{piAgentURL: url, piAgentAPIKey: key, httpClient: client}
	return h.streamChat(ctx, "", prompt, nil)
}

// StreamChatWithUpdatesForTest captures card progress without calling Feishu.
func StreamChatWithUpdatesForTest(ctx context.Context, url, key, prompt string, client *http.Client, onUpdate func(string)) (string, error) {
	card := testCard(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Card struct {
				Data string `json:"data"`
			} `json:"card"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		var content struct {
			Body struct {
				Elements []struct {
					Content string `json:"content"`
				} `json:"elements"`
			} `json:"body"`
		}
		if err := json.Unmarshal([]byte(payload.Card.Data), &content); err != nil {
			return nil, err
		}
		if len(content.Body.Elements) > 0 {
			onUpdate(content.Body.Elements[0].Content)
		}
		return cardResponse(`{"code":0}`), nil
	})
	h := &Handler{piAgentURL: url, piAgentAPIKey: key, httpClient: client}
	return h.streamChat(ctx, "", prompt, card)
}
