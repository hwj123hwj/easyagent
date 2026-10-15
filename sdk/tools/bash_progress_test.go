package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
	"github.com/stretchr/testify/require"
)

type waitingBash struct{}

func (waitingBash) Run(ctx context.Context, _ operations.RunRequest) (operations.RunResult, error) {
	<-ctx.Done()
	return operations.RunResult{Output: []byte("partial output"), ExitCode: -1}, ctx.Err()
}
func TestBashProgressBeforeCompletionAndStopsOnCancel(t *testing.T) {
	tool := NewBashTool(WithBashOperations(waitingBash{}))
	tool.progressInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	update := make(chan agent.PartialResult, 100)
	result := make(chan agent.ToolResult, 1)
	go func() {
		r, _ := tool.Execute(ctx, json.RawMessage(`{"command":"silent","timeout":30}`), func(p agent.PartialResult) { update <- p })
		result <- r
	}()
	select {
	case p := <-update:
		require.Contains(t, p.Content, "仍在执行")
		require.Contains(t, p.Content, "30s")
	case <-time.After(time.Second):
		t.Fatal("no progress before command returned")
	}
	cancel()
	select {
	case r := <-result:
		require.True(t, r.IsError)
		require.Contains(t, r.Content, "partial output")
	case <-time.After(time.Second):
		t.Fatal("cancel did not complete")
	}
	for len(update) > 0 {
		<-update
	}
	select {
	case <-update:
		t.Fatal("progress after tool returned")
	case <-time.After(20 * time.Millisecond):
	}
}
