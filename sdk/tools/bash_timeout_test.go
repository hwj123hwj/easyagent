package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/operations"
	"github.com/stretchr/testify/require"
)

type captureBashTimeout struct{ request operations.RunRequest }

func (b *captureBashTimeout) Run(_ context.Context, req operations.RunRequest) (operations.RunResult, error) {
	b.request = req
	return operations.RunResult{}, nil
}

func TestBashTimeoutDefaultsAndExplicitLongCommand(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		seconds   int
	}{
		{"default", `{"command":"go test ./..."}`, 120},
		{"zero", `{"command":"go test ./...","timeout":0}`, 120},
		{"long build", `{"command":"go test ./...","timeout":600}`, 600},
		{"short explicit", `{"command":"echo ok","timeout":5}`, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := &captureBashTimeout{}
			tool := NewBashTool(WithBashOperations(ops), WithBashWorkspace("/project"))
			args, err := tool.Validate(json.RawMessage(tc.raw))
			require.NoError(t, err)
			_, err = tool.Execute(context.Background(), args, nil)
			require.NoError(t, err)
			require.Equal(t, time.Duration(tc.seconds)*time.Second, ops.request.Timeout)
			require.Equal(t, "/project", ops.request.WorkDir)
		})
	}
	// Direct SDK invocation uses the same default even without Validate.
	ops := &captureBashTimeout{}
	_, err := NewBashTool(WithBashOperations(ops)).Execute(context.Background(), json.RawMessage(`{"command":"build"}`), nil)
	require.NoError(t, err)
	require.Equal(t, 120*time.Second, ops.request.Timeout)
}
