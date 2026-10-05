package session

import (
	"context"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

type EntryType string

const (
	EntryTypeMessage       EntryType = "message"
	EntryTypeModelChange   EntryType = "model_change"
	EntryTypeCompaction    EntryType = "compaction"
	EntryTypeBranchSummary EntryType = "branch_summary"
	EntryTypeLeaf          EntryType = "leaf"
)

// CompactionInfo describes a persisted full compaction. Token counts are estimates.
type CompactionInfo struct {
	Trigger        string `json:"trigger"`
	Instructions   string `json:"instructions,omitempty"`
	MessagesBefore int    `json:"messages_before"`
	MessagesAfter  int    `json:"messages_after"`
	TokensBefore   int    `json:"tokens_before"`
	TokensAfter    int    `json:"tokens_after"`
}

type CompactionRecord struct {
	ID        string          `json:"id"`
	Timestamp int64           `json:"timestamp"`
	Summary   string          `json:"summary"`
	Info      *CompactionInfo `json:"info,omitempty"`
}

type Entry struct {
	ID               string                `json:"id"`
	Type             EntryType             `json:"type"`
	ParentID         string                `json:"parent_id,omitempty"`
	Timestamp        int64                 `json:"timestamp"`
	User             *ai.UserMessage       `json:"user,omitempty"`
	Assistant        *ai.AssistantMessage  `json:"assistant,omitempty"`
	Tool             *ai.ToolResultMessage `json:"tool,omitempty"`
	Model            string                `json:"model,omitempty"`
	Summary          string                `json:"summary,omitempty"`
	TargetID         string                `json:"target_id,omitempty"`
	Retained         []Entry               `json:"retained,omitempty"`
	Compaction       *CompactionInfo       `json:"compaction,omitempty"`
	FirstKeptEntryID string                `json:"first_kept_entry_id,omitempty"`
}

type SessionStorage interface {
	Init() error
	Close() error
	Append(ctx context.Context, entry Entry) error
	GetPathToRoot(ctx context.Context, leafID string) ([]Entry, error)
	SetLeaf(ctx context.Context, targetID string) error
	GetLeaf(ctx context.Context) (string, error)
	Fork(ctx context.Context, targetID string) (SessionStorage, error)
}
