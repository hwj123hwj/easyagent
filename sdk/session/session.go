package session

import (
	"context"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

type Session struct {
	storage SessionStorage
	leafID  string // 当前 leaf entry ID，用于链式追加和恢复
}

func New(storage SessionStorage) *Session {
	return &Session{storage: storage}
}

// Storage returns the underlying storage backend.
func (s *Session) Storage() SessionStorage {
	return s.storage
}

// InitFromStorage 从 storage 中加载当前 leaf，用于恢复已有会话。
func (s *Session) InitFromStorage(ctx context.Context) error {
	leaf, err := s.storage.GetLeaf(ctx)
	if err != nil {
		return err
	}
	s.leafID = leaf
	return nil
}

func (s *Session) AppendMessage(ctx context.Context, msg ai.Message) error {
	entry := Entry{Type: EntryTypeMessage, ParentID: s.leafID}
	switch m := msg.(type) {
	case ai.UserMessage:
		entry.User = &m
	case ai.AssistantMessage:
		entry.Assistant = &m
	case ai.ToolResultMessage:
		entry.Tool = &m
	default:
		return fmt.Errorf("unsupported message type %T", msg)
	}
	if err := s.storage.Append(ctx, entry); err != nil {
		return err
	}
	// 获取 storage 内部分配的 entry ID（Append 已更新 leafID）
	leaf, err := s.storage.GetLeaf(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leaf after append: %w", err)
	}
	s.leafID = leaf
	// Explicit cursor record preserves compatibility with older readers.
	if err := s.storage.SetLeaf(ctx, leaf); err != nil {
		return fmt.Errorf("failed to set leaf: %w", err)
	}
	return nil
}

func (s *Session) BuildContext(ctx context.Context) ([]ai.Message, error) {
	entries, err := s.storage.GetPathToRoot(ctx, "")
	if err != nil {
		return nil, err
	}

	// Find the last compaction entry (if any).
	var lastCompaction *Entry
	var lastCompactionIdx int
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == EntryTypeCompaction {
			e := entries[i]
			lastCompaction = &e
			lastCompactionIdx = i
			break
		}
	}

	messages := make([]ai.Message, 0, len(entries))

	if lastCompaction != nil {
		// Inject summary as context header.
		summaryText := "Context summary from previous conversation:\n\n" + lastCompaction.Summary
		messages = append(messages, ai.NewTextUserMessage(summaryText))

		// Collect messages after the compaction entry (skip compacted history).
		for _, entry := range entries[lastCompactionIdx+1:] {
			messages = append(messages, entryToMessages(entry)...)
		}
	} else {
		// No compaction — use original logic.
		for _, entry := range entries {
			messages = append(messages, entryToMessages(entry)...)
		}
	}

	return messages, nil
}

// entryToMessages extracts ai.Message values from an Entry.
func entryToMessages(entry Entry) []ai.Message {
	switch {
	case entry.User != nil:
		return []ai.Message{*entry.User}
	case entry.Assistant != nil:
		return []ai.Message{*entry.Assistant}
	case entry.Tool != nil:
		return []ai.Message{*entry.Tool}
	}
	return nil
}

// BuildContextEntryIDs returns the JSONL entry id behind each message
// produced by BuildContext, position-aligned with that output. Entries that
// expand to multiple messages repeat their id; synthetic messages injected
// outside the entry path (e.g. the compaction summary header) carry "".
// Callers use the ids as fork anchors: "branch from this message".
func (s *Session) BuildContextEntryIDs(ctx context.Context) ([]string, error) {
	entries, err := s.storage.GetPathToRoot(ctx, "")
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(entries))
	lastCompactionIdx := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == EntryTypeCompaction {
			lastCompactionIdx = i
			break
		}
	}

	tail := entries
	if lastCompactionIdx >= 0 {
		ids = append(ids, "") // compaction summary header message
		tail = entries[lastCompactionIdx+1:]
	}
	for _, entry := range tail {
		for range entryToMessages(entry) {
			ids = append(ids, entry.ID)
		}
	}
	return ids, nil
}

// AppendCompaction writes a compaction entry to the session storage.
// The summary replaces all prior messages when BuildContext is called.
func (s *Session) AppendCompaction(ctx context.Context, summary string) error {
	entry := Entry{
		Type:     EntryTypeCompaction,
		ParentID: s.leafID,
		Summary:  summary,
	}
	if err := s.storage.Append(ctx, entry); err != nil {
		return err
	}
	leaf, err := s.storage.GetLeaf(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leaf after compaction: %w", err)
	}
	// Persist leaf pointer so compaction survives session reload,
	// matching the pattern in AppendMessage().
	if err := s.storage.SetLeaf(ctx, leaf); err != nil {
		return fmt.Errorf("failed to set leaf after compaction: %w", err)
	}
	s.leafID = leaf
	return nil
}

func (s *Session) MoveTo(ctx context.Context, entryID string, summary string) error {
	if err := s.storage.SetLeaf(ctx, entryID); err != nil {
		return err
	}
	s.leafID = entryID
	if summary != "" {
		if err := s.storage.Append(ctx, Entry{Type: EntryTypeBranchSummary, Summary: summary, ParentID: s.leafID}); err != nil {
			return err
		}
		// summary entry 追加后，leaf 指向该 entry
		leaf, err := s.storage.GetLeaf(ctx)
		if err != nil {
			return fmt.Errorf("failed to get leaf after summary append: %w", err)
		}
		s.leafID = leaf
	}
	return nil
}
