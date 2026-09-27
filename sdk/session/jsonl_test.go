package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLStorage_AppendAndGetPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	defer storage.Close()

	ctx := context.Background()

	// 追加 user message（第一条 entry，无 parent）
	userMsg := ai.NewTextUserMessage("hello")
	entry1 := Entry{Type: EntryTypeMessage, User: &userMsg}
	require.NoError(t, storage.Append(ctx, entry1))
	entry1ID := ""
	for id := range storage.byID {
		entry1ID = id
	}

	// 追加 assistant message（parent 指向 user message）
	assistantMsg := ai.AssistantMessage{Text: "world"}
	require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMessage, Assistant: &assistantMsg, ParentID: entry1ID}))
	var entry2ID string
	for id, e := range storage.byID {
		if e.Assistant != nil {
			entry2ID = id
		}
	}

	// 设置 leaf 指向 assistant message
	require.NoError(t, storage.SetLeaf(ctx, entry2ID))

	// 获取路径 root → leaf
	pathEntries, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	assert.Len(t, pathEntries, 2)

	// 第一条是 user message
	assert.Equal(t, EntryTypeMessage, pathEntries[0].Type)
	assert.NotNil(t, pathEntries[0].User)
	assert.Equal(t, "hello", pathEntries[0].User.Content[0].Text)

	// 第二条是 assistant message
	assert.NotNil(t, pathEntries[1].Assistant)
	assert.Equal(t, "world", pathEntries[1].Assistant.Text)
}

func TestJSONLStorage_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	ctx := context.Background()

	// 写入数据
	storage1 := NewJSONLStorage(path)
	require.NoError(t, storage1.Init())
	userMsg := ai.NewTextUserMessage("test message")
	require.NoError(t, storage1.Append(ctx, Entry{Type: EntryTypeMessage, User: &userMsg}))
	var entry1ID string
	for id := range storage1.byID {
		entry1ID = id
	}

	assistantMsg := ai.AssistantMessage{Text: "test response"}
	require.NoError(t, storage1.Append(ctx, Entry{Type: EntryTypeMessage, Assistant: &assistantMsg, ParentID: entry1ID}))
	var entry2ID string
	for id, e := range storage1.byID {
		if e.Assistant != nil {
			entry2ID = id
		}
	}
	require.NoError(t, storage1.SetLeaf(ctx, entry2ID))
	require.NoError(t, storage1.Close())

	// 重新加载
	storage2 := NewJSONLStorage(path)
	require.NoError(t, storage2.Init())
	defer storage2.Close()

	pathEntries, err := storage2.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	assert.Len(t, pathEntries, 2)
	assert.Equal(t, "test message", pathEntries[0].User.Content[0].Text)
	assert.Equal(t, "test response", pathEntries[1].Assistant.Text)
}

func TestJSONLStorage_GetPathToRoot_NotFound(t *testing.T) {
	dir := t.TempDir()
	storage := NewJSONLStorage(filepath.Join(dir, "test.jsonl"))
	require.NoError(t, storage.Init())
	defer storage.Close()

	_, err := storage.GetPathToRoot(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestJSONLStorage_InitCreatesDir(t *testing.T) {
	dir := t.TempDir()
	nestedPath := filepath.Join(dir, "sub", "dir", "session.jsonl")
	storage := NewJSONLStorage(nestedPath)
	require.NoError(t, storage.Init())
	defer storage.Close()

	_, err := os.Stat(filepath.Join(dir, "sub", "dir"))
	assert.NoError(t, err)
}

// TestSession_AppendMessage_BuildContext 端到端测试：
// 验证通过 Session.AppendMessage 写入的消息可以通过 BuildContext 正确恢复。
func TestSession_AppendMessage_BuildContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	defer storage.Close()

	ctx := context.Background()
	sess := New(storage)

	// 写入 user -> assistant -> tool result 序列
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("hello")))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "world"}))
	require.NoError(t, sess.AppendMessage(ctx, ai.ToolResultMessage{ToolCallID: "tc_1", Content: "ok"}))

	// 通过 BuildContext 恢复
	messages, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 3)

	assert.Equal(t, ai.RoleUser, messages[0].Role())
	assert.Equal(t, ai.RoleAssistant, messages[1].Role())
	assert.Equal(t, ai.RoleTool, messages[2].Role())
}

// TestSession_PersistenceAcrossReload 验证会话持久化后重载能恢复完整历史。
func TestSession_PersistenceAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	ctx := context.Background()

	// 第一次写入
	storage1 := NewJSONLStorage(path)
	require.NoError(t, storage1.Init())
	sess1 := New(storage1)
	require.NoError(t, sess1.AppendMessage(ctx, ai.NewTextUserMessage("first")))
	require.NoError(t, sess1.AppendMessage(ctx, ai.AssistantMessage{Text: "second"}))
	require.NoError(t, storage1.Close())

	// 重新加载
	storage2 := NewJSONLStorage(path)
	require.NoError(t, storage2.Init())
	defer storage2.Close()
	sess2 := New(storage2)
	require.NoError(t, sess2.InitFromStorage(ctx))

	messages, err := sess2.BuildContext(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 2)
	assert.Equal(t, ai.RoleUser, messages[0].Role())
	assert.Equal(t, ai.RoleAssistant, messages[1].Role())
}

// TestSession_Compaction_BuildContext 验证 compaction entry 写入后，
// BuildContext 注入摘要并跳过已压缩的旧消息。
func TestSession_Compaction_BuildContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	defer storage.Close()

	ctx := context.Background()
	sess := New(storage)

	// 写入 4 条消息
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("msg 1")))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "resp 1"}))
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("msg 2")))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "resp 2"}))

	// 压缩前：4 条消息
	before, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	assert.Len(t, before, 4)

	// 写入 compaction entry
	require.NoError(t, sess.AppendCompaction(ctx, "Summary: discussed topics 1 and 2"))

	// 压缩后：只有 summary 消息（compaction 之后没有新消息）
	after, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	assert.Len(t, after, 1)
	assert.Equal(t, ai.RoleUser, after[0].Role())
	// Summary 应包含压缩摘要
	userMsg, ok := after[0].(ai.UserMessage)
	require.True(t, ok)
	assert.Contains(t, userMsg.Content[0].Text, "Summary: discussed topics 1 and 2")
}

// TestSession_Compaction_WithNewMessages 验证 compaction 后的新消息也被 BuildContext 正确恢复。
func TestSession_Compaction_WithNewMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	defer storage.Close()

	ctx := context.Background()
	sess := New(storage)

	// 写入旧消息
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("old msg 1")))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "old resp 1"}))

	// 压缩
	require.NoError(t, sess.AppendCompaction(ctx, "Summary of old conversation"))

	// 写入新消息
	require.NoError(t, sess.AppendMessage(ctx, ai.NewTextUserMessage("new msg")))
	require.NoError(t, sess.AppendMessage(ctx, ai.AssistantMessage{Text: "new resp"}))

	// BuildContext 应返回：summary + 2 条新消息
	messages, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 3)

	// 第一条是 summary
	assert.Equal(t, ai.RoleUser, messages[0].Role())
	userMsg, ok := messages[0].(ai.UserMessage)
	require.True(t, ok)
	assert.Contains(t, userMsg.Content[0].Text, "Summary of old conversation")

	// 第二条是新 user message
	assert.Equal(t, ai.RoleUser, messages[1].Role())

	// 第三条是新 assistant message
	assert.Equal(t, ai.RoleAssistant, messages[2].Role())
}

// TestSession_Compaction_PersistenceAcrossReload 验证 compaction entry 持久化后重载能正确恢复。
func TestSession_Compaction_PersistenceAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	ctx := context.Background()

	// 第一次写入
	storage1 := NewJSONLStorage(path)
	require.NoError(t, storage1.Init())
	sess1 := New(storage1)
	require.NoError(t, sess1.AppendMessage(ctx, ai.NewTextUserMessage("old")))
	require.NoError(t, sess1.AppendMessage(ctx, ai.AssistantMessage{Text: "response"}))
	require.NoError(t, sess1.AppendCompaction(ctx, "Compacted summary"))
	require.NoError(t, sess1.AppendMessage(ctx, ai.NewTextUserMessage("after compact")))
	require.NoError(t, storage1.Close())

	// 重新加载
	storage2 := NewJSONLStorage(path)
	require.NoError(t, storage2.Init())
	defer storage2.Close()
	sess2 := New(storage2)
	require.NoError(t, sess2.InitFromStorage(ctx))

	messages, err := sess2.BuildContext(ctx)
	require.NoError(t, err)
	// summary + "after compact" = 2 messages
	assert.Len(t, messages, 2)
	assert.Contains(t, messages[0].(ai.UserMessage).Content[0].Text, "Compacted summary")
}

// TestSession_Compaction_LastOperation_PersistenceAcrossReload 验证
// compaction 作为最后一个操作时，重载后 leaf 指针正确恢复到 compaction entry，
// BuildContext 能看到摘要（而非旧消息）。
func TestSession_Compaction_LastOperation_PersistenceAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	ctx := context.Background()

	// 第一次写入：消息 → 压缩（压缩是最后一步）
	storage1 := NewJSONLStorage(path)
	require.NoError(t, storage1.Init())
	sess1 := New(storage1)
	require.NoError(t, sess1.AppendMessage(ctx, ai.NewTextUserMessage("old msg 1")))
	require.NoError(t, sess1.AppendMessage(ctx, ai.AssistantMessage{Text: "old resp 1"}))
	require.NoError(t, sess1.AppendMessage(ctx, ai.NewTextUserMessage("old msg 2")))
	require.NoError(t, sess1.AppendMessage(ctx, ai.AssistantMessage{Text: "old resp 2"}))
	// compact 是最后一个操作
	require.NoError(t, sess1.AppendCompaction(ctx, "Summary of old conversation"))
	require.NoError(t, storage1.Close())

	// 重新加载 — 验证 leaf 指向 compaction entry
	storage2 := NewJSONLStorage(path)
	require.NoError(t, storage2.Init())
	defer storage2.Close()
	sess2 := New(storage2)
	require.NoError(t, sess2.InitFromStorage(ctx))

	// BuildContext 应只返回 summary 消息（无旧消息）
	messages, err := sess2.BuildContext(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, ai.RoleUser, messages[0].Role())
	userMsg, ok := messages[0].(ai.UserMessage)
	require.True(t, ok)
	assert.Contains(t, userMsg.Content[0].Text, "Summary of old conversation")
	// 确保旧消息不在
	assert.NotContains(t, userMsg.Content[0].Text, "old msg 1")
	assert.NotContains(t, userMsg.Content[0].Text, "old resp")
}

// TestJSONLStorage_Fork 验证 Fork 创建了独立的分支存储，
// 可以独立追加消息而不影响父分支。
func TestJSONLStorage_Fork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	ctx := context.Background()

	// 创建原始存储并写入消息
	storage := NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	defer storage.Close()

	userMsg := ai.NewTextUserMessage("hello")
	require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMessage, User: &userMsg}))
	var entry1ID string
	for id := range storage.byID {
		entry1ID = id
	}

	assistantMsg := ai.AssistantMessage{Text: "world"}
	require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMessage, Assistant: &assistantMsg, ParentID: entry1ID}))
	var entry2ID string
	for id, e := range storage.byID {
		if e.Assistant != nil {
			entry2ID = id
		}
	}
	require.NoError(t, storage.SetLeaf(ctx, entry2ID))

	// Fork 到 entry1（在 user message 处分叉）
	forked, err := storage.Fork(ctx, entry1ID)
	require.NoError(t, err)
	defer forked.Close()

	// forked 存储应该能看到分叉点的 entry
	pathOnFork, err := forked.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	assert.Len(t, pathOnFork, 1)
	assert.Equal(t, "hello", pathOnFork[0].User.Content[0].Text)

	// 在 fork 上追加一条不同的消息
	forkAssistantMsg := ai.AssistantMessage{Text: "forked world"}
	require.NoError(t, forked.Append(ctx, Entry{Type: EntryTypeMessage, Assistant: &forkAssistantMsg, ParentID: entry1ID}))
	var forkedEntryID string
	for id, e := range forked.(*JSONLStorage).byID {
		if e.Assistant != nil && e.Assistant.Text == "forked world" {
			forkedEntryID = id
		}
	}
	require.NoError(t, forked.SetLeaf(ctx, forkedEntryID))

	// fork 的路径应该包含自己的消息
	forkPath, err := forked.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	assert.Len(t, forkPath, 2)
	assert.Equal(t, "forked world", forkPath[1].Assistant.Text)

	// 父存储不受影响
	parentPath, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	assert.Len(t, parentPath, 2)
	assert.Equal(t, "world", parentPath[1].Assistant.Text)
}

// TestJSONLStorage_SetMaxEntries 验证内存上限限制：
// 当超过 maxEntries 时，不在路径上的最旧 entry 被驱逐。
func TestJSONLStorage_SetMaxEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	ctx := context.Background()

	storage := NewJSONLStorage(path)
	storage.SetMaxEntries(3) // 限制 3 条
	require.NoError(t, storage.Init())
	defer storage.Close()

	// 写入一条 user message（entry1）
	userMsg1 := ai.NewTextUserMessage("msg 1")
	require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMessage, User: &userMsg1}))
	var entry1ID string
	for id, e := range storage.byID {
		if e.User != nil && e.User.Content[0].Text == "msg 1" {
			entry1ID = id
		}
	}

	// 写入 3 条 tool result（不在当前路径上，因为 leaf 还没指向它们）
	for i := 0; i < 3; i++ {
		toolMsg := ai.ToolResultMessage{ToolCallID: "tc", Content: fmt.Sprintf("tool result %d", i)}
		require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMessage, Tool: &toolMsg, ParentID: entry1ID}))
	}

	// 写入第二条 user message（entry2）
	userMsg2 := ai.NewTextUserMessage("msg 2")
	require.NoError(t, storage.Append(ctx, Entry{Type: EntryTypeMessage, User: &userMsg2, ParentID: entry1ID}))
	var entry2ID string
	for id, e := range storage.byID {
		if e.User != nil && e.User.Content[0].Text == "msg 2" {
			entry2ID = id
		}
	}

	// 设置 leaf 指向 entry2，路径为 entry1 → entry2
	require.NoError(t, storage.SetLeaf(ctx, entry2ID))

	// byID 不应超过 3 条（路径上的 entry1 + entry2 + 最多 1 个非路径 entry）
	assert.LessOrEqual(t, len(storage.byID), 4, "byID should not exceed maxEntries + path length")

	// 路径上应该有 2 条
	pathEntries, err := storage.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	assert.Len(t, pathEntries, 2)

	// 最后一条消息应该还在
	assert.Equal(t, "msg 2", pathEntries[1].User.Content[0].Text)
	// entry1 也应该还在（在路径上）
	assert.Equal(t, "msg 1", pathEntries[0].User.Content[0].Text)
}

// Simulate SIGTERM after the message fsync and before the separate leaf write.
func TestReloadRecoversDurableToolResultWithoutLeaf(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	store := NewJSONLStorage(path)
	require.NoError(t, store.Init())
	require.NoError(t, store.Append(ctx, Entry{ID: "call", Type: EntryTypeMessage, Assistant: &ai.AssistantMessage{Text: "running"}}))
	require.NoError(t, store.SetLeaf(ctx, "call"))
	require.NoError(t, store.Append(ctx, Entry{ID: "result", ParentID: "call", Type: EntryTypeMessage, Tool: &ai.ToolResultMessage{ToolCallID: "tc", Content: "command canceled: context canceled", IsError: true}}))
	require.NoError(t, store.Close())
	restored := NewJSONLStorage(path)
	require.NoError(t, restored.Init())
	defer restored.Close()
	leaf, err := restored.GetLeaf(ctx)
	require.NoError(t, err)
	require.Equal(t, "result", leaf)
	entries, err := restored.GetPathToRoot(ctx, "")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.True(t, entries[1].Tool.IsError)
	// An explicit branch navigation must still override automatic cursor recovery.
	require.NoError(t, restored.SetLeaf(ctx, "call"))
	require.NoError(t, restored.Close())
	again := NewJSONLStorage(path)
	require.NoError(t, again.Init())
	defer again.Close()
	leaf, err = again.GetLeaf(ctx)
	require.NoError(t, err)
	require.Equal(t, "call", leaf)
}
