package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/session"
)

// Restore the selected branch's full transcript, including compacted messages.
// BuildContext is intentionally not used: it omits history already summarized
// for the model, but that history still belongs in the user's conversation.
func (m *TuiModel) restoreHistory() {
	m.messages = nil
	m.err = nil
	m.inputTokens, m.outputTokens = 0, 0
	m.input.history = nil
	m.input.histIdx = -1
	if m.session == nil || m.session.Session() == nil {
		return
	}
	entries, err := m.session.Session().Storage().GetPathToRoot(context.Background(), "")
	if err != nil {
		m.err = err
		m.messages = []ChatMessage{{Role: "system", Content: fmt.Sprintf("Could not restore history: %v", err), Timestamp: time.Now()}}
		m.viewport.Clear()
		m.viewport.SetMessages(m.messages)
		return
	}
	type location struct{ message, tool int }
	calls := make(map[string]location)
	owner := -1
	for _, entry := range entries {
		at := time.UnixMilli(entry.Timestamp)
		switch {
		case entry.User != nil:
			var blocks []string
			for _, block := range entry.User.Content {
				if block.Type == "text" {
					blocks = append(blocks, block.Text)
				} else if block.Type == "image" {
					blocks = append(blocks, "[image]")
				}
			}
			text := strings.Join(blocks, "\n")
			m.messages = append(m.messages, ChatMessage{Role: "user", Content: text, Timestamp: at})
			m.input.AddHistory(text)
			owner = len(m.messages) - 1
		case entry.Assistant != nil:
			a := entry.Assistant
			if a.Text != "" {
				m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: a.Text, Timestamp: at})
			}
			m.inputTokens += a.Usage.InputTokens
			m.outputTokens += a.Usage.OutputTokens
			if len(a.ToolCalls) > 0 && owner < 0 {
				m.messages = append(m.messages, ChatMessage{Role: "assistant", Timestamp: at})
				owner = len(m.messages) - 1
			}
			for _, call := range a.ToolCalls {
				msg := &m.messages[owner]
				calls[call.ID] = location{owner, len(msg.Tools)}
				msg.Tools = append(msg.Tools, ToolCallInfo{ID: call.ID, Name: call.Name, Args: call.Args, Collapsed: true, Streaming: true})
			}
		case entry.Tool != nil:
			if loc, ok := calls[entry.Tool.ToolCallID]; ok {
				tool := &m.messages[loc.message].Tools[loc.tool]
				tool.Result, tool.IsError, tool.Streaming = entry.Tool.Content, entry.Tool.IsError, false
				if tool.IsError {
					tool.Collapsed = false
				}
				delete(calls, entry.Tool.ToolCallID)
			} else {
				m.messages = append(m.messages, ChatMessage{Role: "system", Content: "Tool result:\n" + entry.Tool.Content, Timestamp: at})
			}
		case entry.Type == session.EntryTypeCompaction:
			m.messages = append(m.messages, ChatMessage{Role: "system", Content: "Context compacted", Timestamp: at})
		}
	}
	for _, loc := range calls {
		tool := &m.messages[loc.message].Tools[loc.tool]
		tool.Streaming, tool.IsError, tool.Collapsed = false, true, false
		tool.Result = "[Previous run ended without a stored tool result]"
	}
	m.finishToolGroups(false)
	m.viewport.Clear()
	m.viewport.SetMessages(m.messages)
	m.viewport.GotoBottom()
}
