package providers

import (
	"encoding/json"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestToolImagesPreserveMultiCallOrdering(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "shot", Name: "computer", Args: `{}`}, {ID: "state", Name: "computer", Args: `{}`}}},
		ai.ToolResultMessage{ToolCallID: "shot", Content: "screenshot", Images: []ai.ImageBlock{{MediaType: "image/png", Data: []byte{1, 2, 3}}}},
		ai.ToolResultMessage{ToolCallID: "state", Content: "app state"},
		ai.AssistantMessage{Text: "next step"},
	}
	request := NewOpenAIProvider("", "").buildOpenAIRequest(ai.StreamRequest{Messages: messages}, false)
	require.Len(t, request.Messages, 5)
	require.Equal(t, "assistant", request.Messages[0].Role)
	require.Equal(t, "tool", request.Messages[1].Role)
	require.Equal(t, "tool", request.Messages[2].Role)
	require.Equal(t, "user", request.Messages[3].Role)
	payload, err := json.Marshal(request.Messages[3].Content)
	require.NoError(t, err)
	require.Contains(t, string(payload), "data:image/png;base64,AQID")
	require.Equal(t, "assistant", request.Messages[4].Role)
	require.Len(t, messages, 4)
	// End-of-history tool groups also flush their images before the next model response.
	require.Len(t, expandToolImages(messages[:3]), 4)
	anthropic := convertToAnthropicMessages(messages)
	require.Len(t, anthropic, 3)
	require.Len(t, anthropic[1].Content, 2)
	require.Equal(t, "tool_result", anthropic[1].Content[0].Type)
	contents := anthropic[1].Content[0].Content.([]anthropicContentBlock)
	require.Len(t, contents, 2)
	require.Equal(t, "image", contents[1].Type)
	require.Equal(t, "AQID", contents[1].Source.Data)
}
