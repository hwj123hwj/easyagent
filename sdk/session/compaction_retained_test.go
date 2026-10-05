package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
)

func TestCompactionRetainedTailSurvivesRepeatedCompactionReloadAndBranch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	sess := New(storage)
	history := []ai.Message{
		ai.NewTextUserMessage("old task"), ai.AssistantMessage{Text: "old answer"},
		ai.UserMessage{DisplayText: "read image", Content: []ai.ContentBlock{{Type: "image", Image: &ai.ImageBlock{Data: []byte{1, 2, 3}, MediaType: "image/png"}}}},
		ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "a", Name: "read", Args: `{"path":"a"}`}, {ID: "b", Name: "bash", Args: `{"command":"pwd"}`}}},
		ai.ToolResultMessage{ToolCallID: "a", Content: "original tool output"},
		ai.ToolResultMessage{ToolCallID: "b", Content: "/workspace"},
	}
	for _, msg := range history {
		require.NoError(t, sess.AppendMessage(ctx, msg))
	}
	original, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	recent := append([]ai.Message(nil), history[2:]...)
	recent[2] = ai.ToolResultMessage{ToolCallID: "a", Content: "[older tool result cleared to save context]"}
	info := &CompactionInfo{Trigger: "manual", Instructions: "preserve tools", MessagesBefore: 6, MessagesAfter: 5}
	require.NoError(t, sess.AppendCompactionKeeping(ctx, "first summary", recent, info))
	messages, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, recent, messages[1:])
	entries, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	require.Equal(t, original[2].ID, entries[len(entries)-1].FirstKeptEntryID)
	require.Equal(t, original[4].ID, entries[len(entries)-1].Retained[2].ID)
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "tools complete"}))
	messages, err = sess.BuildContext(ctx)
	require.NoError(t, err)
	require.NoError(t, sess.AppendCompactionKeeping(ctx, "second summary", messages[1:], &CompactionInfo{Trigger: "automatic"}))
	before, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.NoError(t, storage.Close())
	reloaded := NewJSONLStorage(path)
	require.NoError(t, reloaded.Init())
	defer reloaded.Close()
	restored := New(reloaded)
	require.NoError(t, restored.InitFromStorage(ctx))
	after, err := restored.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	records, err := restored.Compactions(ctx)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "preserve tools", records[0].Info.Instructions)
	require.Greater(t, records[0].Timestamp, int64(0))
	require.NoError(t, restored.MoveTo(ctx, original[1].ID, ""))
	branch, err := restored.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, history[:2], branch)
	records, err = restored.Compactions(ctx)
	require.NoError(t, err)
	require.Empty(t, records)
}
