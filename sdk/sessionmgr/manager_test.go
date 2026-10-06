package sessionmgr

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempDir(t *testing.T) string {
	dir := t.TempDir()
	return dir
}

func TestForkInheritsMicroCompactionWithoutChangingSourceOrOriginalOutput(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(t.TempDir())
	id, _, err := mgr.Create(ctx)
	require.NoError(t, err)
	source, _, err := mgr.Open(ctx, id)
	require.NoError(t, err)
	defer source.Storage().Close()
	history := []ai.Message{ai.NewTextUserMessage("read file"), ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "read", Name: "read"}}}, ai.ToolResultMessage{ToolCallID: "read", Content: "original output"}, ai.AssistantMessage{Text: "done"}}
	for _, msg := range history {
		require.NoError(t, source.AppendMessage(ctx, msg))
	}
	ids, err := source.BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	cleaned := append([]ai.Message(nil), history...)
	cleaned[2] = ai.ToolResultMessage{ToolCallID: "read", Content: "cleared output"}
	require.NoError(t, source.AppendMicroCompaction(ctx, cleaned))
	fullID, _, err := mgr.ForkAt(ctx, id, nil)
	require.NoError(t, err)
	full, _, err := mgr.Open(ctx, fullID)
	require.NoError(t, err)
	defer full.Storage().Close()
	actual, err := full.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, cleaned, actual)
	display, err := full.BuildDisplayContext(ctx)
	require.NoError(t, err)
	require.Equal(t, history, display)
	fullIDs, err := full.BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, ids, fullIDs)
	// An earlier cut must not import a later branch's cleanup marker.
	cutID, _, err := mgr.ForkAt(ctx, id, &ids[3])
	require.NoError(t, err)
	cut, _, err := mgr.Open(ctx, cutID)
	require.NoError(t, err)
	defer cut.Storage().Close()
	actual, err = cut.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, history, actual)
	require.NoError(t, full.AppendCompactionKeeping(ctx, "fork summary", nil, &session.CompactionInfo{Trigger: "manual"}))
	actual, err = source.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, cleaned, actual, "fork writes must not affect source context")
}

func TestManager_Create(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	id, path, err := mgr.Create(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Contains(t, path, id)
	assert.FileExists(t, path)
}

func TestManager_Open(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	id, _, err := mgr.Create(context.Background())
	require.NoError(t, err)

	sess, path, err := mgr.Open(context.Background(), id)
	require.NoError(t, err)
	assert.NotNil(t, sess)
	assert.Contains(t, path, id)
}

func TestManager_Open_NotFound(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	_, _, err := mgr.Open(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestManager_List_Empty(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	sessions, err := mgr.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, sessions, 0)
}

func TestManager_List_WithSessions(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	id1, _, _ := mgr.Create(context.Background())
	id2, _, _ := mgr.Create(context.Background())

	sessions, err := mgr.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, sessions, 2)

	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.ID] = true
	}
	assert.True(t, ids[id1])
	assert.True(t, ids[id2])
}

func TestManager_Delete(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	id, _, _ := mgr.Create(context.Background())
	assert.True(t, mgr.Exists(id))

	err := mgr.Delete(id)
	require.NoError(t, err)
	assert.False(t, mgr.Exists(id))
}

func TestManager_Delete_NotFound(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	// os.RemoveAll on nonexistent path doesn't error, but
	// our Delete wraps the path inside sessions dir
	err := mgr.Delete("nonexistent")
	// This may or may not error depending on os.RemoveAll behavior
	// The important thing is it doesn't panic
	_ = err
}

func TestManager_Exists(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	assert.False(t, mgr.Exists("nonexistent"))

	id, _, _ := mgr.Create(context.Background())
	assert.True(t, mgr.Exists(id))
}

func TestManager_Fork(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	sourceID, _, err := mgr.Create(context.Background())
	require.NoError(t, err)

	newID, newPath, err := mgr.Fork(context.Background(), sourceID, "")
	require.NoError(t, err)
	assert.NotEqual(t, sourceID, newID)
	assert.FileExists(t, newPath)
}

func TestManager_Fork_NotFound(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)

	_, _, err := mgr.Fork(context.Background(), "nonexistent", "")
	assert.Error(t, err)
}

func TestManager_SessionPath(t *testing.T) {
	mgr := NewManager("/data")
	expected := filepath.Join("/data", "sessions", "test123", "session.jsonl")
	assert.Equal(t, expected, mgr.SessionPath("test123"))
}

func TestManager_SessionsDir(t *testing.T) {
	mgr := NewManager("/data")
	expected := filepath.Join("/data", "sessions")
	assert.Equal(t, expected, mgr.SessionsDir())
}

func TestManager_MessageCount(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir)
	ctx := context.Background()

	id, _, err := mgr.Create(ctx)
	require.NoError(t, err)

	// Open and add a message
	sess, _, err := mgr.Open(ctx, id)
	require.NoError(t, err)

	// Write a message entry manually to the session file
	sessionPath := mgr.SessionPath(id)
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	f.WriteString(`{"id":"e1","type":"message","timestamp":1234,"parent_id":""}` + "\n")
	f.WriteString(`{"id":"e2","type":"leaf","timestamp":1235,"target_id":"e1"}` + "\n")
	f.Close()
	sess.Storage().Close()

	// List and check count
	sessions, err := mgr.List(ctx)
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	// MessageCount should be 1 (only EntryTypeMessage)
	assert.Equal(t, 1, sessions[0].MessageCount)
}

func TestForkRetainedSnapshotKeepsClearedToolsAndIndependentPreferences(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(t.TempDir())
	sourceID, _, err := mgr.Create(ctx)
	require.NoError(t, err)
	require.NoError(t, mgr.SaveMeta(sourceID, "/project", "coding"))
	title, flag := "source", true
	require.NoError(t, mgr.UpdatePreferences(sourceID, PreferencePatch{Title: &title, Pinned: &flag, Archived: &flag}))
	source, _, err := mgr.Open(ctx, sourceID)
	require.NoError(t, err)
	defer source.Storage().Close()
	original := []ai.Message{ai.NewTextUserMessage("question"), ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "call", Name: "read"}}}, ai.ToolResultMessage{ToolCallID: "call", Content: "large original output"}}
	for _, msg := range original {
		require.NoError(t, source.AppendMessage(ctx, msg))
	}
	ids, err := source.BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	retained := append([]ai.Message(nil), original...)
	retained[2] = ai.ToolResultMessage{ToolCallID: "call", Content: "[cleared]"}
	require.NoError(t, source.AppendCompactionKeeping(ctx, "summary", retained, nil))
	newID, _, err := mgr.ForkAt(ctx, sourceID, &ids[2])
	require.NoError(t, err)
	fork, _, err := mgr.Open(ctx, newID)
	require.NoError(t, err)
	defer fork.Storage().Close()
	messages, err := fork.BuildContext(ctx)
	require.NoError(t, err)
	require.Len(t, messages, 4)
	require.Equal(t, "[cleared]", messages[3].(ai.ToolResultMessage).Content)
	infos, err := mgr.List(ctx)
	require.NoError(t, err)
	for _, info := range infos {
		if info.ID == newID {
			require.False(t, info.Archived)
			require.False(t, info.Pinned)
			require.Equal(t, "/project", info.Workspace)
			require.Equal(t, sourceID, info.ForkedFrom)
		}
	}
	invalid := "absent"
	_, _, err = mgr.ForkAt(ctx, sourceID, &invalid)
	require.Error(t, err)
	infos, err = mgr.List(ctx)
	require.NoError(t, err)
	require.Len(t, infos, 2)
}
