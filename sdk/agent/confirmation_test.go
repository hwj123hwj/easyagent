package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// confirmableTool 实现完整 Tool + ToolWithConfirmation，用于测试确认拦截。
type confirmableTool struct {
	executed atomic.Int32
}

type approvalRequiredTool struct{ confirmableTool }

func (*approvalRequiredTool) RequiresConfirmationAvailable() bool { return true }

func TestExternalToolRequiresApprovalChannel(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_channel", true: "explicit_approval"}[approved], func(t *testing.T) {
			registry := providers.NewRegistry()
			registry.Register(&mockTestProvider{responses: []mockTestResponse{
				{toolCalls: []ai.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}, stop: ai.StopReasonToolUse},
				{text: "done", stop: ai.StopReasonStop},
			}})
			tool := &approvalRequiredTool{}
			var confirm ConfirmFunc
			if approved {
				confirm = func(context.Context, ConfirmationRequest) ConfirmDecision { return ConfirmDecision{Approved: true} }
			}
			ag := New(Options{Model: ai.Model{ID: "test", Provider: "mock_test"}, Registry: registry, Tools: []Tool{tool}, ConfirmFunc: confirm, MaxTurns: 5})
			_, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("do it"))
			require.NoError(t, err)
			if approved {
				assert.Equal(t, int32(1), tool.executed.Load())
			} else {
				assert.Zero(t, tool.executed.Load())
			}
		})
	}
}

func (t *confirmableTool) Name() string        { return "danger" }
func (t *confirmableTool) Description() string { return "A tool that needs confirmation" }
func (t *confirmableTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *confirmableTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (t *confirmableTool) RequiresConfirmation(_ json.RawMessage) (string, bool) {
	return "即将执行危险操作", true
}
func (t *confirmableTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(PartialResult)) (ToolResult, error) {
	t.executed.Add(1)
	return ToolResult{Content: "executed"}, nil
}

func newConfirmableAgent(confirm ConfirmFunc) (*Agent, *confirmableTool) {
	mp := &mockTestProvider{responses: []mockTestResponse{
		{
			toolCalls: []ai.ToolCall{{ID: "call_1", Name: "danger", Args: `{}`}},
			stop:      ai.StopReasonToolUse,
		},
		{text: "acknowledged.", stop: ai.StopReasonStop},
	}}
	registry := providers.NewRegistry()
	registry.Register(mp)

	tool := &confirmableTool{}
	ag := New(Options{
		Model:       ai.Model{ID: "test", Name: "test", Provider: "mock_test"},
		Registry:    registry,
		System:      "test",
		Tools:       []Tool{tool},
		MaxTurns:    5,
		ConfirmFunc: confirm,
	})
	return ag, tool
}

// 用户拒绝时：工具不执行，"user declined" 回告 LLM（IsError=false）。
func TestConfirmation_DeclinedBlocksAndReportsToLLM(t *testing.T) {
	// 捕获回告给 LLM 的 tool result 内容：检查第二轮 provider 收到的 messages。
	var seenToolResult string
	var seenDetails any
	mp := &mockTestProvider{responses: []mockTestResponse{
		{toolCalls: []ai.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}, stop: ai.StopReasonToolUse},
		{text: "ok", stop: ai.StopReasonStop},
	}}
	wrapped := &capturingProvider{inner: mp, onMessages: func(msgs []ai.Message) {
		for _, m := range msgs {
			if tr, ok := m.(ai.ToolResultMessage); ok {
				seenToolResult = tr.Content
				seenDetails = tr.Details
				assert.False(t, tr.IsError, "拒绝不是系统错误")
			}
		}
	}}
	registry := providers.NewRegistry()
	registry.Register(wrapped)

	tool := &confirmableTool{}
	ag := New(Options{
		Model:    ai.Model{ID: "test", Name: "test", Provider: "mock_test"},
		Registry: registry,
		System:   "test",
		Tools:    []Tool{tool},
		MaxTurns: 5,
		ConfirmFunc: func(ctx context.Context, req ConfirmationRequest) ConfirmDecision {
			return ConfirmDecision{Approved: false, Reason: "too risky"}
		},
	})

	_, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("do it"))
	require.NoError(t, err)

	assert.Equal(t, int32(0), tool.executed.Load(), "拒绝时工具不应执行")
	assert.Contains(t, seenToolResult, "user declined", "回告 LLM 的内容应含拒绝信息")
	assert.Contains(t, seenToolResult, "too risky", "拒绝理由应回告 LLM")
	assert.Equal(t, map[string]any{"approval": "declined"}, seenDetails)
}

// 用户同意时：工具执行，正常继续。
func TestConfirmation_ApprovedExecutes(t *testing.T) {
	ag, tool := newConfirmableAgent(func(ctx context.Context, req ConfirmationRequest) ConfirmDecision {
		return ConfirmDecision{Approved: true}
	})

	_, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("do it"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), tool.executed.Load(), "同意时工具应执行一次")
}

// 未注入 ConfirmFunc（serve/feishu）：需要确认的工具默认放行。
func TestConfirmation_NilConfirmDefaultsToAllow(t *testing.T) {
	ag, tool := newConfirmableAgent(nil)

	_, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("do it"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), tool.executed.Load(), "未注入 ConfirmFunc 应默认放行")
}

// 不实现 ToolWithConfirmation 的工具：无论是否注入 ConfirmFunc 都不触发确认。
func TestConfirmation_NonConfirmingToolSkipsGate(t *testing.T) {
	mp := &mockTestProvider{responses: []mockTestResponse{
		{toolCalls: []ai.ToolCall{{ID: "c1", Name: "echo", Args: `{"message":"hi"}`}}, stop: ai.StopReasonToolUse},
		{text: "done", stop: ai.StopReasonStop},
	}}
	registry := providers.NewRegistry()
	registry.Register(mp)

	confirmCalled := false
	ag := New(Options{
		Model:    ai.Model{ID: "test", Name: "test", Provider: "mock_test"},
		Registry: registry,
		System:   "test",
		Tools:    []Tool{&echoTool{}},
		MaxTurns: 5,
		ConfirmFunc: func(ctx context.Context, req ConfirmationRequest) ConfirmDecision {
			confirmCalled = true
			return ConfirmDecision{Approved: true}
		},
	})

	_, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("echo"))
	require.NoError(t, err)
	assert.False(t, confirmCalled, "echo 工具不实现 ToolWithConfirmation，不应触发确认")
}

// ToolResult.DisplayText 优先 UserFacing，空则 fallback Content。
func TestToolResult_DisplayText(t *testing.T) {
	assert.Equal(t, "for-user", ToolResult{Content: "for-model", UserFacing: "for-user"}.DisplayText())
	assert.Equal(t, "shared", ToolResult{Content: "shared"}.DisplayText())
	assert.Equal(t, "", ToolResult{}.DisplayText())
}

// 捕获传给 provider 的 messages，用于断言回告 LLM 的 tool result。
type capturingProvider struct {
	inner      *mockTestProvider
	onMessages func([]ai.Message)
}

func (c *capturingProvider) Name() string { return "mock_test" }
func (c *capturingProvider) StreamSimple(ctx context.Context, req ai.SimpleStreamRequest) (*ai.EventStream, error) {
	return c.Stream(ctx, ai.StreamRequest{Model: req.Model, Messages: req.Messages, System: req.System, Tools: req.Tools})
}
func (c *capturingProvider) Stream(ctx context.Context, req ai.StreamRequest) (*ai.EventStream, error) {
	if c.onMessages != nil {
		c.onMessages(req.Messages)
	}
	return c.inner.Stream(ctx, req)
}
