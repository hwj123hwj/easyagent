package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/stretchr/testify/require"
)

type compactionSchemaTool struct{ echoTool }

func (compactionSchemaTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "description": strings.Repeat("schema", 10000)}
}

func TestFullCompactionIncludesToolSchemas(t *testing.T) {
	history := []ai.Message{ai.NewTextUserMessage("task"), ai.AssistantMessage{Text: strings.Repeat("x", 360000)}, ai.NewTextUserMessage("continue")}
	summarized := false
	ag := New(Options{Tools: []Tool{&compactionSchemaTool{}}, Model: ai.Model{ContextWindow: 128000}, CompactionSettings: compaction.DefaultSettings(),
		SummarizeFunc: func(context.Context, []ai.Message, []ai.Message, string) (string, error) {
			summarized = true
			return "summary", nil
		}})
	usage := estimateContext(ag.llmRequest(history), ai.Usage{})
	require.Greater(t, usage.Tools, 0)
	require.False(t, compaction.ShouldCompact(usage.Messages, 128000, compaction.DefaultSettings()))
	require.True(t, compaction.ShouldCompact(usage.EstimatedTokens, 128000, compaction.DefaultSettings()))
	ag.maybeCompact(context.Background(), history)
	require.True(t, summarized)
}

func TestFullCompactionIncludesSystemAndToolsAndPreemptsMicro(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("summaryFails=%v", fail), func(t *testing.T) {
			history := []ai.Message{ai.NewTextUserMessage(strings.Repeat("x", 280000))}
			for i := 0; i < 7; i++ {
				id := fmt.Sprintf("read-%d", i)
				history = append(history, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "read"}}}, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("y", 12000)})
			}
			summarized := false
			ag := New(Options{System: strings.Repeat("s", 60000), Model: ai.Model{ContextWindow: 128000}, CompactionSettings: compaction.DefaultSettings(),
				SummarizeFunc: func(_ context.Context, old, recent []ai.Message, _ string) (string, error) {
					summarized = true
					require.Contains(t, old[len(old)-1].(ai.ToolResultMessage).Content, strings.Repeat("y", 12000), "summary sees original tool output")
					if fail {
						return "", errors.New("summary unavailable")
					}
					return "task summary", nil
				}})
			usage := estimateContext(ag.llmRequest(history), ai.Usage{})
			require.False(t, compaction.ShouldCompact(usage.Messages, 128000, compaction.DefaultSettings()))
			require.True(t, compaction.ShouldCompact(usage.EstimatedTokens, 128000, compaction.DefaultSettings()))
			micro := 0
			ag.Subscribe(func(_ context.Context, e AgentEvent) {
				if _, ok := e.(EventMicroCompacted); ok {
					micro++
				}
			})
			result := ag.maybeCompact(context.Background(), history)
			require.True(t, summarized)
			require.Zero(t, micro, "high-water cleanup must go straight to LLM summary")
			if fail {
				require.Equal(t, history, result)
			} else {
				require.Less(t, compaction.EstimateTokens(result), compaction.EstimateTokens(history))
			}
		})
	}
}
