package computer

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQuitKeyAliasesAlwaysAsk(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigurePolicy("auto", nil)
	for _, key := range []string{"q+cmd", "cmd + q", "shift+cmd+q", "esc+alt+cmd", "cmd+alt+shift+q", "ctrl+cmd+q"} {
		raw, _ := json.Marshal(map[string]any{"action": "perform_action", "actions": []map[string]string{{"type": "key", "key": key}}})
		valid, err := tool.Validate(raw)
		require.NoError(t, err)
		_, ask := tool.RequiresConfirmation(valid)
		require.True(t, ask, key)
	}
}

func TestExecutionLeaseAndFailureRelease(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigureSession("first")
	entered, release := make(chan struct{}), make(chan struct{})
	tool.helper = func(ctx context.Context, name string, args any) (json.RawMessage, error) {
		close(entered)
		select {
		case <-release:
			return nil, errors.New("helper failed")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	done := make(chan agent.ToolResult)
	go func() {
		result, _ := tool.Execute(context.Background(), json.RawMessage(`{"action":"get_app_state"}`), nil)
		done <- result
	}()
	<-entered
	require.Equal(t, "first", Registry.Owner())
	other := NewTool(t.TempDir())
	other.ConfigureSession("second")
	result, _ := other.Execute(context.Background(), json.RawMessage(`{"action":"get_app_state"}`), nil)
	require.True(t, result.IsError)
	require.Contains(t, result.Content, "占用")
	require.ErrorIs(t, Registry.Acquire("first"), ErrLeaseHeld, "same-session concurrent calls must not release each other's ownership")
	close(release)
	require.True(t, (<-done).IsError)
	require.Empty(t, Registry.Owner())
}

func TestExecuteRejectsInvalidAndFailedBatches(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigureSession("test")
	calls := 0
	tool.helper = func(context.Context, string, any) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"success":false,"error":"partial failure","focused_app":"test.editor"}`), nil
	}
	result, _ := tool.Execute(context.Background(), json.RawMessage(`{"action":"perform_action","actions":[{"type":"click"}]}`), nil)
	require.True(t, result.IsError)
	require.Zero(t, calls)
	result, _ = tool.Execute(context.Background(), json.RawMessage(`{"action":"perform_action","actions":[{"type":"key","key":"cmd+s"}]}`), nil)
	require.True(t, result.IsError)
	require.Empty(t, tool.ApprovedApps())
	require.Empty(t, Registry.Owner())
}

func TestHelperMustExplicitlyConfirmBatchSuccess(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigureSession("test")
	for _, out := range []string{`{}`, `null`, `invalid`} {
		tool.helper = func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(out), nil }
		result, _ := tool.Execute(context.Background(), json.RawMessage(`{"action":"perform_action","actions":[{"type":"key","key":"enter"}]}`), nil)
		require.True(t, result.IsError, out)
	}
}
