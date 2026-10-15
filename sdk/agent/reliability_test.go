package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

type failedStreamProvider struct {
	partial  ai.StreamAssistantMessage
	err      error
	calls    int
	finished chan struct{}
	cancel   context.CancelFunc
}

func (p *failedStreamProvider) Name() string { return "failed-stream" }
func (p *failedStreamProvider) StreamSimple(ctx context.Context, _ ai.SimpleStreamRequest) (*ai.EventStream, error) {
	return p.Stream(ctx, ai.StreamRequest{})
}
func (p *failedStreamProvider) Stream(ctx context.Context, _ ai.StreamRequest) (*ai.EventStream, error) {
	p.calls++
	if p.calls > 1 {
		return nil, errors.New("failed request must not be repeated by the goal loop")
	}
	stream := ai.NewEventStream(1)
	go func() {
		defer close(p.finished)
		defer stream.Close()
		_ = stream.Push(ctx, ai.EventTextDelta{Delta: p.partial.Text, Partial: p.partial})
		_ = stream.Push(ctx, ai.EventError{Reason: "error", Error: p.err.Error()})
		if p.cancel != nil {
			p.cancel()
		}
		// 错误之后仍需读到关闭；提前返回会让终止阶段被有界 channel 卡住。
		for i := 0; i < 16; i++ {
			_ = stream.Push(ctx, ai.EventThinkingDelta{})
		}
		stream.SetResult(p.partial, p.err)
	}()
	return stream, nil
}

func TestCanceledProviderPartialSurvivesReload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage := session.NewJSONLStorage(path)
	require.NoError(t, storage.Init())
	partial := ai.StreamAssistantMessage{Text: "部分回答", Usage: ai.Usage{InputTokens: 23, OutputTokens: 4}, StopReason: ai.StopReasonAborted}
	provider := &failedStreamProvider{partial: partial, err: context.Canceled, finished: make(chan struct{}), cancel: cancel}
	registry := providers.NewRegistry()
	registry.Register(provider)
	ag := New(Options{Registry: registry, Model: ai.Model{Provider: provider.Name()}, Session: session.New(storage)})
	result, err := ag.Prompt(ctx, ai.NewTextUserMessage("任务"))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, ai.StopReasonAborted, result.StopReason)
	require.Equal(t, partial.Text, result.Text)
	require.Equal(t, partial.Usage, result.Usage)
	require.NoError(t, storage.Close())
	reloaded := session.NewJSONLStorage(path)
	require.NoError(t, reloaded.Init())
	defer reloaded.Close()
	history, err := session.New(reloaded).BuildContext(context.Background())
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, result, history[1])
}

type floodStreamProvider struct {
	started  chan struct{}
	finished chan struct{}
}

func (p *floodStreamProvider) Name() string { return "flood-stream" }
func (p *floodStreamProvider) StreamSimple(ctx context.Context, _ ai.SimpleStreamRequest) (*ai.EventStream, error) {
	return p.Stream(ctx, ai.StreamRequest{})
}
func (p *floodStreamProvider) Stream(ctx context.Context, _ ai.StreamRequest) (*ai.EventStream, error) {
	stream := ai.NewEventStream(1)
	go func() {
		defer close(p.finished)
		defer stream.Close()
		close(p.started)
		for i := 0; i < 256; i++ {
			_ = stream.Push(ctx, ai.EventTextDelta{Delta: "x"})
		}
		stream.SetResult(ai.StreamAssistantMessage{Text: "partial", StopReason: ai.StopReasonAborted}, ctx.Err())
	}()
	return stream, nil
}

func TestPromptStreamCancellationDrainsProviderWithoutConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &floodStreamProvider{started: make(chan struct{}), finished: make(chan struct{})}
	registry := providers.NewRegistry()
	registry.Register(provider)
	ag := New(Options{Registry: registry, Model: ai.Model{Provider: provider.Name()}})
	events, err := ag.PromptStream(ctx, ai.NewTextUserMessage("任务"))
	require.NoError(t, err)
	<-provider.started
	require.Eventually(t, func() bool { return len(events) == cap(events) }, time.Second, time.Millisecond)
	cancel()
	require.Eventually(t, func() bool { return ag.State() != StateRunning }, time.Second, time.Millisecond)
	for range events {
	}
	select {
	case <-provider.finished:
	case <-time.After(time.Second):
		t.Fatal("cancelled producer was not drained")
	}
}

func TestProviderStreamFailurePreservesPartialAndStopsGoal(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			name := "sync"
			if streaming {
				name = "stream"
			}
			if empty {
				name += "-empty"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
				require.NoError(t, storage.Init())
				defer storage.Close()
				sess := session.New(storage)
				failure := errors.New("API error 401: test credentials rejected")
				partial := ai.StreamAssistantMessage{StopReason: ai.StopReasonError, ErrorMsg: failure.Error()}
				if !empty {
					partial.Text, partial.Thinking = "已收到的回答", "已收到的思考"
					partial.Usage = ai.Usage{InputTokens: 100, OutputTokens: 7}
					partial.ToolCalls = []ai.ToolCall{{ID: "partial-call", Name: "echo", Args: `{"message":`}}
				}
				provider := &failedStreamProvider{partial: partial, err: failure, finished: make(chan struct{})}
				registry := providers.NewRegistry()
				registry.Register(provider)
				ag := New(Options{Registry: registry, Model: ai.Model{Provider: provider.Name()}, Session: sess, Goal: "完成任务", MaxTurns: 1})
				var result ai.AssistantMessage
				if streaming {
					events, err := ag.PromptStream(ctx, ai.NewTextUserMessage("任务"))
					require.NoError(t, err)
					failures := 0
					for event := range events {
						if event.Type == StreamEventError {
							failures++
							require.Contains(t, event.Error, failure.Error())
						}
						if event.Type == StreamEventDone {
							result = event.FinalMessage
						}
					}
					require.Equal(t, 1, failures)
				} else {
					var err error
					result, err = ag.Prompt(ctx, ai.NewTextUserMessage("任务"))
					require.ErrorIs(t, err, failure)
				}
				require.Equal(t, 1, provider.calls, "Goal must not retry a failed provider turn")
				require.Equal(t, ai.StopReasonError, result.StopReason)
				require.Equal(t, partial.Text, result.Text)
				require.Equal(t, partial.Thinking, result.Thinking)
				require.Equal(t, partial.Usage, result.Usage)
				require.Empty(t, result.ToolCalls, "incomplete stream calls must not become executable history")
				select {
				case <-provider.finished:
				case <-ctx.Done():
					t.Fatal("provider did not finish after its error event")
				}
				history, err := sess.BuildContext(context.Background())
				require.NoError(t, err)
				if empty {
					require.Len(t, history, 1, "empty failures must not create blank assistants")
				} else {
					require.Len(t, history, 2)
					require.Equal(t, result, history[1])
				}
			})
		}
	}
}
