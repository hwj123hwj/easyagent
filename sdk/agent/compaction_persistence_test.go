package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

type failingCompactionStorage struct {
	session.SessionStorage
	fail bool
}

func (s *failingCompactionStorage) Append(ctx context.Context, entry session.Entry) error {
	if s.fail && entry.Type == session.EntryTypeCompaction {
		return errors.New("disk unavailable")
	}
	return s.SessionStorage.Append(ctx, entry)
}

func TestManualAndAutoCompactionMatchNextPromptAndRestart(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "manual"
		if automatic {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "context.jsonl")
			storage := session.NewJSONLStorage(path)
			require.NoError(t, storage.Init())
			sess := session.New(storage)
			for i := 0; i < 10; i++ {
				require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage(strings.Repeat("任务", 100))))
				require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: strings.Repeat("回复", 100)}))
			}
			summarizer := func(_ context.Context, old, recent []ai.Message, instructions string) (string, error) {
				if !automatic {
					require.Equal(t, "保留未完成测试", instructions)
				}
				require.NotEmpty(t, old)
				require.NotEmpty(t, recent)
				return "缓存约定与未完成测试", nil
			}
			settings := compaction.Settings{Enabled: true, ReserveTokens: 100, KeepRecentTokens: 350}
			ag := New(Options{Session: sess, Model: ai.Model{ID: "test", ContextWindow: 1000}, System: "system rules", SummarizeFunc: summarizer, CompactionSettings: settings})
			before, err := sess.BuildContext(ctx)
			require.NoError(t, err)
			var live []ai.Message
			if automatic {
				live = ag.maybeCompact(ctx, before)
			} else {
				_, from, to, err := ag.CompactNow(ctx, "保留未完成测试")
				require.NoError(t, err)
				require.Equal(t, 20, from)
				require.Greater(t, to, 1)
			}
			persisted, err := sess.BuildContext(ctx)
			require.NoError(t, err)
			require.Greater(t, len(persisted), 1)
			if automatic {
				require.Equal(t, live, persisted)
			}
			snapshot, err := ag.ContextSnapshot(ctx)
			require.NoError(t, err)
			require.Equal(t, ag.llmRequest(persisted).System, snapshot.System)
			require.Len(t, snapshot.Messages, len(persisted))
			require.Equal(t, persisted[len(persisted)-1], snapshot.Messages[len(snapshot.Messages)-1].Message)
			require.Len(t, snapshot.Compactions, 1)
			require.Equal(t, name, snapshot.Compactions[0].Info.Trigger)
			require.Equal(t, 20, snapshot.Compactions[0].Info.MessagesBefore)
			require.NoError(t, storage.Close())
			reloaded := session.NewJSONLStorage(path)
			require.NoError(t, reloaded.Init())
			defer reloaded.Close()
			restored := session.New(reloaded)
			require.NoError(t, restored.InitFromStorage(ctx))
			next, err := restored.BuildContext(ctx)
			require.NoError(t, err)
			require.Equal(t, persisted, next)
			require.NoError(t, restored.AppendMessage(ctx, ai.NewTextUserMessage("next request")))
			next, err = restored.BuildContext(ctx)
			require.NoError(t, err)
			require.Equal(t, persisted, next[:len(next)-1])
		})
	}
}

func TestFailedCompactionLeavesOriginalContextAndEmitsNoSuccess(t *testing.T) {
	for _, failSummary := range []bool{false, true} {
		t.Run(map[bool]string{false: "storage", true: "summarizer"}[failSummary], func(t *testing.T) {
			ctx := context.Background()
			storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
			require.NoError(t, storage.Init())
			defer storage.Close()
			backend := &failingCompactionStorage{SessionStorage: storage}
			sess := session.New(backend)
			for i := 0; i < 5; i++ {
				require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage(strings.Repeat("x", 1000))))
				require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "done"}))
			}
			before, err := sess.BuildContext(ctx)
			require.NoError(t, err)
			backend.fail = !failSummary
			ag := New(Options{Session: sess, Model: ai.Model{ContextWindow: 1000}, CompactionSettings: compaction.Settings{Enabled: true, ReserveTokens: 100, KeepRecentTokens: 300}, SummarizeFunc: func(context.Context, []ai.Message, []ai.Message, string) (string, error) {
				if failSummary {
					return "", errors.New("model unavailable")
				}
				return "summary", nil
			}})
			var events []AgentEvent
			ag.Subscribe(func(_ context.Context, event AgentEvent) { events = append(events, event) })
			result := ag.maybeCompact(ctx, before)
			require.Equal(t, before, result)
			after, err := sess.BuildContext(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Len(t, events, 1)
			require.IsType(t, EventCompactionFailed{}, events[0])
			_, _, _, err = ag.CompactNow(ctx, "")
			require.Error(t, err)
			after, err = sess.BuildContext(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestCompactionFailureIsNonFatalInPromptStream(t *testing.T) {
	ctx := context.Background()
	storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
	require.NoError(t, storage.Init())
	defer storage.Close()
	sess := session.New(storage)
	for i := 0; i < 5; i++ {
		require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage(strings.Repeat("x", 1000))))
		require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "done"}))
	}
	registry := providers.NewRegistry()
	registry.Register(&mockTestProvider{responses: []mockTestResponse{{text: "answer after failed compaction", stop: ai.StopReasonStop}}})
	ag := New(Options{Session: sess, Registry: registry, Model: ai.Model{Provider: "mock_test", ContextWindow: 1000}, CompactionSettings: compaction.Settings{Enabled: true, ReserveTokens: 100, KeepRecentTokens: 300}, SummarizeFunc: func(context.Context, []ai.Message, []ai.Message, string) (string, error) {
		return "", errors.New("summarizer unavailable")
	}})
	events, err := ag.PromptStream(ctx, ai.NewTextUserMessage("new task"))
	require.NoError(t, err)
	warned, done := false, false
	for event := range events {
		require.NotEqual(t, StreamEventError, event.Type, "a recoverable compaction failure must not fail the whole run")
		if event.Type == StreamEventCompactionFailed {
			warned = true
		}
		if event.Type == StreamEventDone {
			done = true
			require.Equal(t, "answer after failed compaction", event.FinalMessage.Text)
		}
	}
	require.True(t, warned)
	require.True(t, done)
}
