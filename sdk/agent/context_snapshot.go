package agent

import (
	"context"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/hwj123hwj/easyagent/sdk/session"
)

// ContextSnapshot is the current logical provider input, before provider-specific
// serialization and before a new draft/queued message is appended. It is not a
// capture of the last HTTP request. Use runtime.AgentSession for idle-only access.
type ContextSnapshot struct {
	Model       ai.Model                   `json:"model"`
	System      string                     `json:"system"`
	Messages    []ContextMessage           `json:"messages"`
	Tools       []ai.ToolDefinition        `json:"tools"`
	Usage       ContextUsage               `json:"usage"`
	Compactions []session.CompactionRecord `json:"compactions"`
}

type ContextMessage struct {
	Role    ai.Role    `json:"role"`
	Message ai.Message `json:"message"`
}

func (a *Agent) ContextSnapshot(ctx context.Context) (ContextSnapshot, error) {
	var history []ai.Message
	records := make([]session.CompactionRecord, 0)
	if a.session != nil {
		var err error
		history, err = a.session.BuildContext(ctx)
		if err != nil {
			return ContextSnapshot{}, err
		}
		records, err = a.session.Compactions(ctx)
		if err != nil {
			return ContextSnapshot{}, err
		}
	}
	req := a.llmRequest(history)
	messages := make([]ContextMessage, 0, len(history))
	for _, msg := range history {
		messages = append(messages, ContextMessage{Role: msg.Role(), Message: msg})
	}
	tools := req.Tools
	if tools == nil {
		tools = make([]ai.ToolDefinition, 0)
	}
	return ContextSnapshot{Model: req.Model, System: req.System, Messages: messages, Tools: tools, Usage: estimateContext(req, latestRequestUsage(history)), Compactions: records}, nil
}

func compactionInfo(trigger, instructions string, history []ai.Message, summary string, recent []ai.Message) *session.CompactionInfo {
	after := append([]ai.Message{ai.NewTextUserMessage("Context summary from previous conversation:\n\n" + summary)}, recent...)
	return &session.CompactionInfo{Trigger: trigger, Instructions: instructions, MessagesBefore: len(history), MessagesAfter: len(after), TokensBefore: compaction.EstimateTokens(history), TokensAfter: compaction.EstimateTokens(after)}
}
