package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
)

func TestMicroCompactionKeepsOriginalHistoryAndBranchLocalContext(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	sess := New(storage)
	history := []ai.Message{
		ai.NewTextUserMessage("inspect files"),
		ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "read-1", Name: "read"}, {ID: "read-2", Name: "read"}}},
		ai.ToolResultMessage{ToolCallID: "read-1", Content: "original first output", IsError: true},
		ai.ToolResultMessage{ToolCallID: "read-2", Content: "original second output"},
		ai.AssistantMessage{Text: "done"},
	}
	for _, msg := range history {
		require.NoError(t, sess.AppendMessage(ctx, msg))
	}
	ids, err := sess.BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	cleaned := append([]ai.Message(nil), history...)
	tool := cleaned[2].(ai.ToolResultMessage)
	tool.Content = "cleared first"
	cleaned[2] = tool
	require.NoError(t, sess.AppendMicroCompaction(ctx, cleaned))
	firstLeaf, err := storage.GetLeaf(ctx)
	require.NoError(t, err)
	tool = cleaned[3].(ai.ToolResultMessage)
	tool.Content = "cleared second"
	cleaned[3] = tool
	require.NoError(t, sess.AppendMicroCompaction(ctx, cleaned))
	contextMessages, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, cleaned, contextMessages)
	display, err := sess.BuildDisplayContext(ctx)
	require.NoError(t, err)
	require.Equal(t, history, display)
	contextIDs, err := sess.BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, ids, contextIDs, "markers must not shift message IDs or positions")
	entries, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	require.Len(t, entries, len(history)+2)
	require.Len(t, entries[len(entries)-1].ToolReplacements, 1, "only newly cleaned outputs are stored")
	require.NoError(t, sess.AppendMicroCompaction(ctx, cleaned))
	again, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	require.Equal(t, entries, again, "unchanged context must not append another marker")

	// Rewinding to before the marker restores the original context. Rewinding
	// to the first marker inherits only its replacements, not later ones.
	require.NoError(t, sess.MoveTo(ctx, ids[len(ids)-1], ""))
	branch, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, history, branch)
	require.NoError(t, sess.MoveTo(ctx, firstLeaf, ""))
	branch, err = sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, "cleared first", branch[2].(ai.ToolResultMessage).Content)
	require.Equal(t, "original second output", branch[3].(ai.ToolResultMessage).Content)
	// A full compaction retains the cleaned context, while its display still
	// resolves the original outputs from their source entries.
	require.NoError(t, sess.AppendCompactionKeeping(ctx, "summary", branch[1:], &CompactionInfo{Trigger: "manual"}))
	before, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	tool = before[3].(ai.ToolResultMessage)
	tool.Content = "cleared after full compaction"
	before[3] = tool
	require.NoError(t, sess.AppendMicroCompaction(ctx, before))
	actual, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, before, actual, "micro-compaction must also patch retained snapshot IDs")
	display, err = sess.BuildDisplayContext(ctx)
	require.NoError(t, err)
	require.Equal(t, "original first output", display[2].(ai.ToolResultMessage).Content)
	require.NoError(t, storage.Close())
	reopened := NewJSONLStorage(path)
	require.NoError(t, reopened.Init())
	defer reopened.Close()
	restored := New(reopened)
	require.NoError(t, restored.InitFromStorage(ctx))
	after, err := restored.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	afterDisplay, err := restored.BuildDisplayContext(ctx)
	require.NoError(t, err)
	require.Equal(t, display, afterDisplay)
}

func TestMicroCompactionRecoversCursorWithoutLeafRecord(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	sess := New(storage)
	require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: "read", Content: "original output"}))
	ids, err := sess.BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	// Simulate stopping after the durable marker but before SetLeaf.
	require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMicroCompaction, ParentID: ids[0], ToolReplacements: []ToolResultReplacement{{EntryID: ids[0], Content: "cleared"}}}))
	require.NoError(t, storage.Close())
	reopened := NewJSONLStorage(path)
	require.NoError(t, reopened.Init())
	defer reopened.Close()
	restored := New(reopened)
	require.NoError(t, restored.InitFromStorage(ctx))
	require.NoError(t, restored.AppendMessage(ctx, ai.NewTextUserMessage("next")))
	msgs, err := restored.BuildContext(ctx)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "cleared", msgs[0].(ai.ToolResultMessage).Content)
}
