package session

import (
	"context"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// AppendMicroCompaction stores only changed tool contents on the active branch.
// Source messages remain intact for history display and earlier branch points.
func (s *Session) AppendMicroCompaction(ctx context.Context, history []ai.Message) error {
	entries, err := s.contextEntries(ctx)
	if err != nil {
		return err
	}
	if len(entries) != len(history) {
		return fmt.Errorf("micro-compaction context does not match stored history")
	}
	var replacements []ToolResultReplacement
	for i, msg := range history {
		if tool, ok := msg.(ai.ToolResultMessage); ok {
			original := entries[i].Tool
			if original == nil || original.ToolCallID != tool.ToolCallID {
				return fmt.Errorf("micro-compaction tool does not match stored entry")
			}
			if original.Content != tool.Content {
				replacements = append(replacements, ToolResultReplacement{EntryID: entries[i].ID, Content: tool.Content})
			}
		}
	}
	if len(replacements) == 0 {
		return nil
	}
	if err := s.storage.Append(ctx, Entry{Type: EntryTypeMicroCompaction, ParentID: s.leafID, ToolReplacements: replacements}); err != nil {
		return err
	}
	leaf, err := s.storage.GetLeaf(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leaf after micro-compaction: %w", err)
	}
	s.leafID = leaf
	return s.storage.SetLeaf(ctx, leaf)
}

// BuildDisplayContext keeps the same message positions and source IDs as
// BuildContext, but exposes original tool output rather than cleaned input.
func (s *Session) BuildDisplayContext(ctx context.Context) ([]ai.Message, error) {
	entries, err := s.buildContextEntries(ctx, false)
	if err != nil {
		return nil, err
	}
	messages := make([]ai.Message, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, entryToMessages(entry)...)
	}
	return messages, nil
}
