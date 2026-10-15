package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 集成测试：确认机制在真实 Agent 调用链上的端到端行为。
// 这些测试不经过 AgentSession，但覆盖了 executeOneTool 的完整路径，
// 验证确认门与分区调度、并行执行、多工具同批等真实场景的交互。

// 需要确认的工具 + 并发安全的只读工具混在一批，验证：
//  1. 需要确认的工具即便同意执行，也不会和别的工具并行（unsafe → 独占串行批次）；
//  2. 确认串行触发，不出现并发冲突。
func TestConfirmation_DangerousToolSerializedEvenWhenApproved(t *testing.T) {
	danger := &concurrentDangerTool{}
	safe := &concurrentSafeTool{}

	mp := &mockTestProvider{responses: []mockTestResponse{
		{toolCalls: []ai.ToolCall{
			{ID: "s1", Name: "safe", Args: `{}`},
			{ID: "d1", Name: "danger", Args: `{}`},
			{ID: "s2", Name: "safe", Args: `{}`},
		}, stop: ai.StopReasonToolUse},
		{text: "done", stop: ai.StopReasonStop},
	}}
	registry := providers.NewRegistry()
	registry.Register(mp)

	ag := New(Options{
		Model:    ai.Model{ID: "t", Name: "t", Provider: "mock_test"},
		Registry: registry,
		System:   "test",
		Tools:    []Tool{danger, safe},
		MaxTurns: 5,
		ConfirmFunc: func(ctx context.Context, req ConfirmationRequest) ConfirmDecision {
			return ConfirmDecision{Approved: true}
		},
	})

	_, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("batch"))
	require.NoError(t, err)

	// 危险工具未实现 ConcurrencySafeChecker → 默认 unsafe → 独占串行批次，
	// 不会和 safe 工具并发。验证它执行了一次、且无并发冲突（maxConcurrent==1）。
	assert.Equal(t, int32(1), danger.executed.Load())
	assert.Equal(t, int32(1), danger.maxConcurrent.Load(),
		"危险工具应串行执行，最大并发数为 1")
}

// 并发安全工具（不实现 ToolWithConfirmation）允许并行，不触发确认门。
type concurrentSafeTool struct {
	executed atomic.Int32
}

func (t *concurrentSafeTool) Name() string        { return "safe" }
func (t *concurrentSafeTool) Description() string { return "safe op" }
func (t *concurrentSafeTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *concurrentSafeTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (t *concurrentSafeTool) IsConcurrencySafe(_ json.RawMessage) bool { return true }
func (t *concurrentSafeTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(PartialResult)) (ToolResult, error) {
	t.executed.Add(1)
	return ToolResult{Content: "ok"}, nil
}

// 需要确认的工具：不实现 ConcurrencySafeChecker（默认 unsafe），实现 ToolWithConfirmation。
// 跟踪执行次数和观测到的最大并发数（应为 1，证明串行）。
type concurrentDangerTool struct {
	mu            sync.Mutex
	executed      atomic.Int32
	inFlight      int32
	maxConcurrent atomic.Int32
}

func (t *concurrentDangerTool) Name() string        { return "danger" }
func (t *concurrentDangerTool) Description() string { return "dangerous op" }
func (t *concurrentDangerTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *concurrentDangerTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (t *concurrentDangerTool) RequiresConfirmation(_ json.RawMessage) (string, bool) {
	return "dangerous", true
}
func (t *concurrentDangerTool) Execute(ctx context.Context, raw json.RawMessage, onUpdate func(PartialResult)) (ToolResult, error) {
	t.mu.Lock()
	t.inFlight++
	if t.inFlight > t.maxConcurrent.Load() {
		t.maxConcurrent.Store(t.inFlight)
	}
	t.mu.Unlock()

	t.executed.Add(1)

	t.mu.Lock()
	t.inFlight--
	t.mu.Unlock()
	return ToolResult{Content: "done"}, nil
}

// Structured input is scoped to one invocation and only executes after reply.
type answeredTool struct {
	concurrentDangerTool
	response json.RawMessage
}

func (*answeredTool) RequiresInteractiveConfirmation() bool { return true }
func (t *answeredTool) ExecuteConfirmed(_ context.Context, _ json.RawMessage, decision ConfirmDecision, _ func(PartialResult)) (ToolResult, error) {
	t.response = decision.Response
	return ToolResult{Content: string(decision.Response)}, nil
}
func TestStructuredConfirmationWaitsAndPassesResponse(t *testing.T) {
	tool := &answeredTool{}
	started, reply := make(chan ConfirmationRequest, 1), make(chan ConfirmDecision, 1)
	ag := New(Options{Tools: []Tool{tool}, ConfirmFunc: func(ctx context.Context, req ConfirmationRequest) ConfirmDecision {
		started <- req
		select {
		case d := <-reply:
			return d
		case <-ctx.Done():
			return ConfirmDecision{Reason: "cancelled"}
		}
	}})
	result := make(chan ai.Message, 1)
	go func() {
		result <- executeOneTool(context.Background(), ag, ai.ToolCall{ID: "question", Name: "danger", Args: `{}`})
	}()
	request := <-started
	require.True(t, request.ForceConfirmation)
	select {
	case <-result:
		t.Fatal("question finished before user answered")
	default:
	}
	response := json.RawMessage(`[{"selected":["A"]}]`)
	reply <- ConfirmDecision{Approved: true, Response: response}
	message := (<-result).(ai.ToolResultMessage)
	require.False(t, message.IsError)
	require.JSONEq(t, string(response), message.Content)
	require.JSONEq(t, string(response), string(tool.response))
	require.Zero(t, tool.executed.Load(), "must not execute the ordinary placeholder")
	missing := New(Options{Tools: []Tool{&answeredTool{}}})
	declined := executeOneTool(context.Background(), missing, ai.ToolCall{ID: "missing", Name: "danger", Args: `{}`}).(ai.ToolResultMessage)
	require.Contains(t, declined.Content, "interactive")
}
