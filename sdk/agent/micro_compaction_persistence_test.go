package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

func TestMicroCompactionSurvivesNextPromptAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := session.NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	sess := session.New(storage)
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("read-%d", i)
		require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "read", Args: `{}`}}}))
		require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("x", 4000)}))
	}
	settings := compaction.Settings{Enabled: true, ReserveTokens: 2000, KeepRecentTokens: 2000, MicroKeepRecent: 2}
	var firstRequest []ai.Message
	registry := providers.NewRegistry()
	provider := &capturingProvider{inner: &mockTestProvider{responses: []mockTestResponse{
		{text: "first answer", stop: ai.StopReasonStop},
		{text: "second answer", stop: ai.StopReasonStop},
		{text: "after restart", stop: ai.StopReasonStop},
		{text: "after new tools", stop: ai.StopReasonStop},
	}}}
	registry.Register(provider)
	ag := New(Options{Session: sess, Registry: registry, Model: ai.Model{ID: "test", Provider: "mock_test", ContextWindow: 10000}, CompactionSettings: settings})
	provider.onMessages = func(messages []ai.Message) {
		if firstRequest == nil {
			firstRequest = append([]ai.Message(nil), messages...)
		}
		for i := 0; i < 5; i++ {
			require.Less(t, len(messages[i*2+1].(ai.ToolResultMessage).Content), 100)
		}
	}
	prompt := func(agent *Agent, wantCleared int) {
		t.Helper()
		events, err := agent.PromptStream(ctx, ai.NewTextUserMessage("continue"))
		require.NoError(t, err)
		micro, done := 0, false
		for event := range events {
			require.NotEqual(t, StreamEventError, event.Type)
			if event.Type == StreamEventMicroCompacted {
				micro++
				require.Equal(t, wantCleared, event.ClearedCount)
			}
			if event.Type == StreamEventDone {
				done = true
			}
		}
		require.True(t, done)
		if wantCleared > 0 {
			require.Equal(t, 1, micro)
		} else {
			require.Zero(t, micro)
		}
	}
	prompt(ag, 5)
	persisted, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, firstRequest, persisted[:len(firstRequest)], "the next request must reuse the cleaned context")
	prompt(ag, 0)
	require.NoError(t, storage.Close())
	reloaded := session.NewJSONLStorage(path)
	require.NoError(t, reloaded.Init())
	defer reloaded.Close()
	restored := session.New(reloaded)
	require.NoError(t, restored.InitFromStorage(ctx))
	ag = New(Options{Session: restored, Registry: registry, Model: ai.Model{ID: "test", Provider: "mock_test", ContextWindow: 10000}, CompactionSettings: settings})
	prompt(ag, 0)
	// New large tool outputs still trigger a new cleanup; the old five must
	// not be counted again, even after reloading the session.
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("new-read-%d", i)
		require.NoError(t, restored.AppendMessage(ctx, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "read"}}}))
		require.NoError(t, restored.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("y", 4000)}))
	}
	prompt(ag, 7)
}

type failingMicroCompactionStorage struct{ session.SessionStorage }

func (s failingMicroCompactionStorage) Append(ctx context.Context, entry session.Entry) error {
	if entry.Type == session.EntryTypeMicroCompaction {
		return errors.New("disk unavailable")
	}
	return s.SessionStorage.Append(ctx, entry)
}

func TestFailedMicroCompactionKeepsOriginalContextAndWarns(t *testing.T) {
	ctx := context.Background()
	storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
	require.NoError(t, storage.Init())
	defer storage.Close()
	sess := session.New(failingMicroCompactionStorage{storage})
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("read-%d", i)
		require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "read"}}}))
		require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: id, Content: strings.Repeat("x", 4000)}))
	}
	before, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	ag := New(Options{Session: sess, Model: ai.Model{ContextWindow: 10000}, CompactionSettings: compaction.Settings{Enabled: true, MicroKeepRecent: 2}})
	var events []AgentEvent
	ag.Subscribe(func(_ context.Context, event AgentEvent) { events = append(events, event) })
	result := ag.maybeCompact(ctx, before)
	require.Equal(t, before, result, "a failed save must not mutate the caller's context")
	after, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Len(t, events, 1)
	require.IsType(t, EventCompactionFailed{}, events[0])
	require.Contains(t, events[0].(EventCompactionFailed).Error, "disk unavailable")
}

func TestMicroCompactionIgnoresTinySavings(t *testing.T) {
	ctx := context.Background()
	storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
	require.NoError(t, storage.Init())
	defer storage.Close()
	sess := session.New(storage)
	// 写入几个极其微小的 tool results（比 clearedPlaceholder 只多几个字符）
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("ls-%d", i)
		require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: id, Name: "ls"}}}))
		require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: id, Content: "file1.txt file2.txt file3.txt"}))
	}
	history, err := sess.BuildContext(ctx)
	require.NoError(t, err)

	// Settings 中 MinSavingsTokens 为 500
	settings := compaction.Settings{Enabled: true, MicroCompactRatio: 0.1, MicroKeepRecent: 2, MinSavingsTokens: 500}
	ag := New(Options{Session: sess, Model: ai.Model{ContextWindow: 1000}, CompactionSettings: settings})
	var events []AgentEvent
	ag.Subscribe(func(_ context.Context, event AgentEvent) { events = append(events, event) })

	result := ag.maybeCompact(ctx, history)
	// 因为每次只能省几个 token（远小于 500），不应触发持久化或事件通知
	require.Equal(t, history, result)
	require.Empty(t, events)
}
