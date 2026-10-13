package compaction

import (
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestComputerImagesCountAndCompact(t *testing.T) {
	image := ai.ImageBlock{MediaType: "image/png", Data: []byte{1, 2, 3}}
	messages := []ai.Message{
		ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "old", Name: "computer"}, {ID: "new", Name: "computer"}}},
		ai.ToolResultMessage{ToolCallID: "old", Content: "shot", Images: []ai.ImageBlock{image}},
		ai.ToolResultMessage{ToolCallID: "new", Content: "shot", Images: []ai.ImageBlock{image}},
	}
	before := EstimateTokens(messages)
	after, count := MicroCompact(messages, 1)
	require.Equal(t, 1, count)
	require.Empty(t, after[1].(ai.ToolResultMessage).Images)
	require.Len(t, after[2].(ai.ToolResultMessage).Images, 1)
	require.Greater(t, before-EstimateTokens(after), 4000)
}
