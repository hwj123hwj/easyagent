package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

type gatedThinkingProvider struct{ finish chan struct{} }

func (*gatedThinkingProvider) Name() string { return "gated-thinking" }
func (p *gatedThinkingProvider) StreamSimple(ctx context.Context, req ai.SimpleStreamRequest) (*ai.EventStream, error) {
	return p.Stream(ctx, ai.StreamRequest{Model: req.Model, Messages: req.Messages})
}
func (p *gatedThinkingProvider) Stream(ctx context.Context, req ai.StreamRequest) (*ai.EventStream, error) {
	stream := ai.NewEventStream(8)
	go func() {
		defer stream.Close()
		_ = stream.Push(ctx, ai.EventThinkingDelta{Delta: "检查输入后再给出答案"})
		select {
		case <-p.finish:
		case <-ctx.Done():
			return
		}
		msg := ai.StreamAssistantMessage{Thinking: "检查输入后再给出答案", Text: "答案", StopReason: ai.StopReasonStop, Usage: ai.Usage{InputTokens: 42, OutputTokens: 8}}
		_ = stream.Push(ctx, ai.EventTextDelta{Delta: msg.Text})
		_ = stream.Push(ctx, ai.EventDone{Message: msg, Reason: msg.StopReason})
		stream.SetResult(msg, nil)
	}()
	return stream, nil
}

func TestThinkingArrivesBeforeProviderFinishesAndSurvivesReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := session.NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	p := &gatedThinkingProvider{finish: make(chan struct{})}
	registry := providers.NewRegistry()
	registry.Register(p)
	ag := New(Options{Registry: registry, Model: ai.Model{ID: "gpt-4o", Provider: p.Name(), ContextWindow: 128000}, Session: session.New(storage)})
	events, err := ag.PromptStream(ctx, ai.NewTextUserMessage("问题"))
	require.NoError(t, err)
	select {
	case e := <-events:
		require.Equal(t, StreamEventContextUsage, e.Type)
	case <-ctx.Done():
		t.Fatal("no context event")
	}
	select {
	case e := <-events:
		require.Equal(t, StreamEventThinkingDelta, e.Type)
		require.Equal(t, "检查输入后再给出答案", e.TextDelta)
	case <-ctx.Done():
		t.Fatal("thinking buffered until done")
	}
	// The provider is still blocked: the UI can display reasoning before a final answer exists.
	select {
	case e := <-events:
		t.Fatalf("unexpected event while provider gated: %v", e.Type)
	default:
	}
	close(p.finish)
	var duration int64
	var measured *ContextUsage
	var ended bool
	for e := range events {
		if e.Type == StreamEventThinkingEnd {
			duration = e.DurationMS
			ended = true
		}
		if e.Type == StreamEventContextUsage {
			measured = e.ContextUsage
		}
	}
	require.True(t, ended)
	require.NotNil(t, measured)
	require.Equal(t, 42, measured.LastRequest.InputTokens)
	require.NoError(t, storage.Close())
	restored := session.NewJSONLStorage(path)
	require.NoError(t, restored.Init())
	defer restored.Close()
	sess := session.New(restored)
	require.NoError(t, sess.InitFromStorage(ctx))
	history, err := sess.BuildContext(ctx)
	require.NoError(t, err)
	msg := history[len(history)-1].(ai.AssistantMessage)
	require.Equal(t, "检查输入后再给出答案", msg.Thinking)
	require.Equal(t, duration, msg.ThinkingDurationMS)
	require.Equal(t, 42, msg.Usage.InputTokens)
}

func TestContextIncludesSystemAndToolsAndKeepsLatestMeasurement(t *testing.T) {
	req := ai.StreamRequest{Model: ai.Model{ID: "unregistered", ContextWindow: 128000}, System: "system prompt", Tools: []ai.ToolDefinition{{Name: "echo", Description: "echo tool"}}, Messages: []ai.Message{ai.NewTextUserMessage("hello")}}
	usage := estimateContext(req, ai.Usage{InputTokens: 80, OutputTokens: 12})
	require.Positive(t, usage.System)
	require.Positive(t, usage.Tools)
	require.Positive(t, usage.Messages)
	require.Equal(t, usage.Messages+usage.System+usage.Tools, usage.EstimatedTokens)
	require.False(t, usage.WindowKnown)
	require.Equal(t, 128000, usage.ContextWindow)
	req.Model.ID = "gpt-4o"
	req.Model.ContextWindow = 0
	require.True(t, estimateContext(req, ai.Usage{}).WindowKnown)
	require.Equal(t, 128000, estimateContext(req, ai.Usage{}).ContextWindow)
}
