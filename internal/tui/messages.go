package tui

import (
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
)

// ── Messages (tea.Msg) ────────────────────────────────────────────────────
// These are the message types that flow through the Bubble Tea Update loop.

// StreamTextMsg carries a text delta from the LLM stream.
type StreamTextMsg struct {
	Delta string
}

// ToolStartMsg signals a tool has started executing.
type ToolStartMsg struct {
	ID   string
	Name string
	Args any
}

// ToolUpdateMsg carries a partial result from a running tool.
type ToolUpdateMsg struct {
	ID     string
	Name   string
	Result any
}

// ToolEndMsg signals a tool has finished executing.
type ToolEndMsg struct {
	ID      string
	Name    string
	Result  any
	IsError bool
}

// StreamDoneMsg signals the agent stream has completed.
type StreamDoneMsg struct {
	InputTokens  int
	OutputTokens int
}

// AgentErrorMsg carries an error from the agent.
type AgentErrorMsg struct {
	Err error
}

// ConfirmationMsg carries a dangerous-tool confirmation request.
type ConfirmationMsg struct {
	Req agent.ConfirmationRequest
}

// CompactionMsg signals context compaction completed.
type CompactionMsg struct {
	Summary string
}

// LoopDetectedMsg signals the agent is in a loop.
type LoopDetectedMsg struct {
	Tool  string
	Count int
}

// TickMsg is used for spinner animation.
type TickMsg struct {
	Time time.Time
}

// ResizeMsg carries terminal resize info.
type ResizeMsg struct {
	Width  int
	Height int
}

// ── Conversation data types ───────────────────────────────────────────────

// ChatMessage represents a single message in the conversation.
type ChatMessage struct {
	Role          string // "user", "assistant", "system"
	Content       string // raw text
	Timestamp     time.Time
	Tools         []ToolCallInfo
	ToolsExpanded bool
	ToolsManual   bool
}

// ToolCallInfo represents a tool call within a message.
type ToolCallInfo struct {
	ID        string // unique tool call ID for reliable matching
	Name      string
	Args      string
	Result    string
	IsError   bool
	Collapsed bool
	Streaming bool
	StartTime time.Time
	EndTime   time.Time
	Manual    bool
}
