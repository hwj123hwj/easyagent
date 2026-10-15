package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

func TestMicroCompactionBatchesSmallOutputsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := session.NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	defer func() { storage.Close() }()
	sess := session.New(storage)
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage(strings.Repeat("x", 320000))))
	microCount := 0
	var savings []int
	newAgent := func() *Agent {
		ag := New(Options{Session: sess, Model: ai.Model{ContextWindow: 128000}, CompactionSettings: compaction.DefaultSettings()})
		ag.Subscribe(func(_ context.Context, event AgentEvent) {
			if micro, ok := event.(EventMicroCompacted); ok {
				microCount++
				savings = append(savings, micro.TokensBefore-micro.TokensAfter)
			}
		})
		return ag
	}
	ag := newAgent()
	for i := 0; i < 30; i++ {
		if i == 10 || i == 20 {
			require.NoError(t, storage.Close())
			storage = session.NewJSONLStorage(path)
			require.NoError(t, storage.Init())
			sess = session.New(storage)
			require.NoError(t, sess.InitFromStorage(ctx))
			ag = newAgent()
		}
		id := fmt.Sprintf("bash-%d", i)
		require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "bash", Args: `{}`}}}))
		require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("y", 1000)}))
		history, err := sess.BuildContext(ctx)
		require.NoError(t, err)
		before := microCount
		result := ag.maybeCompact(ctx, history)
		if before == microCount {
			require.Equal(t, history, result, "deferred cleanup must not alter the next request")
		}
		for j := max(0, i-4); j <= i; j++ {
			require.Equal(t, strings.Repeat("y", 1000), result[2+j*2].(ai.ToolResultMessage).Content)
		}
		persisted, err := sess.BuildContext(ctx)
		require.NoError(t, err)
		require.Equal(t, result, persisted)
	}
	require.Equal(t, 2, microCount, "30 small tool rounds should produce two batches, not 25 tiny cleanups")
	for _, saved := range savings {
		require.GreaterOrEqual(t, saved, 2048)
	}
	t.Logf("30 small tool rounds: %d micro-compaction events", microCount)
}

func TestMicroCompactionDeferralDoesNotBlockFullCompaction(t *testing.T) {
	history := []ai.Message{ai.NewTextUserMessage("older task"), ai.AssistantMessage{Text: strings.Repeat("x", 480000)}}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("bash-%d", i)
		history = append(history, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "bash"}}}, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("y", 1000)})
	}
	summarized := false
	ag := New(Options{Model: ai.Model{ContextWindow: 128000}, CompactionSettings: compaction.DefaultSettings(),
		SummarizeFunc: func(context.Context, []ai.Message, []ai.Message, string) (string, error) {
			summarized = true
			return "summary", nil
		}})
	result := ag.maybeCompact(context.Background(), history)
	require.True(t, summarized, "small pending cleanup must not postpone the full-context safety check")
	require.Less(t, compaction.EstimateTokens(result), compaction.EstimateTokens(history))
}

func TestMicroCompactionSavingsPolicy(t *testing.T) {
	custom := compaction.DefaultSettings()
	custom.MinSavingsTokens = 100
	for _, tc := range []struct {
		name     string
		settings compaction.Settings
		oldSize  int
		want     bool
	}{
		{"small result deferred", compaction.DefaultSettings(), 500, false},
		{"large result reclaimed immediately", compaction.DefaultSettings(), 12000, true},
		{"zero minimum uses default", compaction.Settings{Enabled: true, MicroKeepRecent: 5}, 500, false},
		{"explicit SDK minimum", custom, 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := []ai.Message{ai.NewTextUserMessage(strings.Repeat("x", 320000))}
			for i := 0; i < 6; i++ {
				id := fmt.Sprintf("read-%d", i)
				size := 500
				if i == 0 {
					size = tc.oldSize
				}
				history = append(history, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "read"}}}, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("y", size)})
			}
			ag := New(Options{Model: ai.Model{ContextWindow: 128000}, CompactionSettings: tc.settings})
			micro := false
			ag.Subscribe(func(_ context.Context, event AgentEvent) {
				if _, ok := event.(EventMicroCompacted); ok {
					micro = true
				}
			})
			result := ag.maybeCompact(context.Background(), history)
			require.Equal(t, tc.want, micro)
			if !tc.want {
				require.Equal(t, history, result)
			} else {
				require.Less(t, compaction.EstimateTokens(result), compaction.EstimateTokens(history))
			}
		})
	}
}
