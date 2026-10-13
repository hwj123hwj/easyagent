package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/stretchr/testify/require"
)

type failingMessageStorage struct {
	session.SessionStorage
	err   error
	stage string
}

func (s *failingMessageStorage) GetPathToRoot(ctx context.Context, id string) ([]session.Entry, error) {
	if s.stage == "context" {
		return nil, s.err
	}
	return s.SessionStorage.GetPathToRoot(ctx, id)
}
func (s *failingMessageStorage) Append(ctx context.Context, entry session.Entry) error {
	if s.stage == "user" && entry.User != nil || s.stage == "assistant" && entry.Assistant != nil {
		return s.err
	}
	return s.SessionStorage.Append(ctx, entry)
}

func TestPromptPersistenceFailureStopsBeforeModelOrTool(t *testing.T) {
	for _, stage := range []string{"context", "user", "assistant"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
			require.NoError(t, storage.Init())
			defer storage.Close()
			failure := errors.New("simulated storage failure")
			backend := &failingMessageStorage{SessionStorage: storage, err: failure, stage: stage}
			provider := &mockTestProvider{responses: []mockTestResponse{
				{stop: ai.StopReasonToolUse, toolCalls: []ai.ToolCall{{ID: "call", Name: "echo", Args: `{"message":"write"}`}}},
				{stop: ai.StopReasonStop, text: "done"},
			}}
			registry := providers.NewRegistry()
			registry.Register(provider)
			toolCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			tool := &cancelTestTool{cancel: cancel}
			ag := New(Options{Registry: registry, Model: ai.Model{Provider: provider.Name()}, Session: session.New(backend), Tools: []Tool{tool}, MaxTurns: 2})
			_, err := ag.Prompt(toolCtx, ai.NewTextUserMessage("任务"))
			require.ErrorIs(t, err, failure)
			require.Zero(t, tool.calls, "unpersisted calls must never execute")
			wantCalls := 0
			if stage == "assistant" {
				wantCalls = 1
			}
			require.Equal(t, wantCalls, provider.callIndex)
		})
	}
}

func TestFailedStreamPartialPersistencePreservesBothErrors(t *testing.T) {
	storage := session.NewJSONLStorage(filepath.Join(t.TempDir(), "session.jsonl"))
	require.NoError(t, storage.Init())
	defer storage.Close()
	storageErr, providerErr := errors.New("storage unavailable"), errors.New("provider interrupted")
	backend := &failingMessageStorage{SessionStorage: storage, err: storageErr, stage: "assistant"}
	provider := &failedStreamProvider{
		partial:  ai.StreamAssistantMessage{Text: "partial", StopReason: ai.StopReasonError},
		err:      providerErr,
		finished: make(chan struct{}),
	}
	registry := providers.NewRegistry()
	registry.Register(provider)
	ag := New(Options{Registry: registry, Model: ai.Model{Provider: provider.Name()}, Session: session.New(backend)})
	result, err := ag.Prompt(context.Background(), ai.NewTextUserMessage("任务"))
	require.ErrorIs(t, err, providerErr)
	require.ErrorIs(t, err, storageErr)
	require.Equal(t, "partial", result.Text)
	require.Equal(t, ai.StopReasonError, result.StopReason)
}
