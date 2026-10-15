package computer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTargetRequiredBeforeHelperOrLease(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigureSession("target-test")
	calls := 0
	tool.helper = func(context.Context, string, any) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{}`), nil
	}
	for _, raw := range []string{
		`{"action":"screenshot"}`, `{"action":"get_app_state"}`, `{"action":"perform_action","actions":[{"type":"key","key":"enter"}]}`,
		`{"action":"screenshot","bundle_id":"test.editor","pid":1,"window_id":0}`,
		`{"action":"screenshot","bundle_id":"test.editor","pid":true,"window_id":2}`,
		`{"action":"screenshot","bundle_id":"test.editor","pid":1,"window_id":4294967296}`,
	} {
		result, err := tool.Execute(context.Background(), json.RawMessage(raw), nil)
		require.NoError(t, err)
		require.True(t, result.IsError, raw)
		require.Zero(t, calls)
		require.Empty(t, Registry.Owner())
	}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list_apps"}`), nil)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, 1, calls)
}
func TestTargetForwardingAndWaitAudit(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigureSession("forwarding-test")
	tool.helper = func(_ context.Context, name string, args any) (json.RawMessage, error) {
		raw, err := json.Marshal(args)
		require.NoError(t, err)
		var target struct {
			BundleID string `json:"bundle_id"`
			PID      int    `json:"pid"`
			WindowID int    `json:"window_id"`
		}
		require.NoError(t, json.Unmarshal(raw, &target))
		require.Equal(t, "test.editor", target.BundleID)
		require.Equal(t, 100, target.PID)
		require.Equal(t, 200, target.WindowID)
		return json.RawMessage(`{"success":true,"target":{"bundle_id":"test.editor"}}`), nil
	}
	for _, raw := range []string{
		`{"action":"get_app_state","bundle_id":"test.editor","pid":100,"window_id":200}`,
		`{"action":"perform_action","bundle_id":"test.editor","pid":100,"window_id":200,"actions":[{"type":"wait","ms":0}]}`,
	} {
		result, err := tool.Execute(context.Background(), json.RawMessage(raw), nil)
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Empty(t, tool.ApprovedApps())
	}
	tool.recordControlledApp(json.RawMessage(`{"controlled_app":"test.editor"}`))
	require.Equal(t, []string{"test.editor"}, tool.ApprovedApps())
}

func TestOldHelperNeverReceivesControl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell; native computer use supports macOS")
	}
	for _, version := range []string{`{"protocol_version":1}`, `{}`, `invalid`, `{"protocol_version":2}`} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "helper")
			marker := filepath.Join(dir, "called")
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then printf '%s' '" + version + "'; exit 0; fi\nprintf called > '" + marker + "'\nprintf '%s' '{\"success\":true}'\n"
			require.NoError(t, os.WriteFile(path, []byte(script), 0700))
			_, err := runHelperAtPath(context.Background(), path, "perform_action", map[string]any{"pid": 100, "window_id": 200})
			if version == `{"protocol_version":2}` {
				require.NoError(t, err)
				_, statErr := os.Stat(marker)
				require.NoError(t, statErr)
			} else {
				require.Error(t, err)
				_, statErr := os.Stat(marker)
				require.True(t, os.IsNotExist(statErr), "legacy helper must never receive a tool invocation")
			}
		})
	}
}

func TestApprovalNamesTargetWindow(t *testing.T) {
	tool := NewTool(t.TempDir())
	raw, err := tool.Validate(json.RawMessage(`{"action":"perform_action","bundle_id":"test.editor","pid":100,"window_id":200,"actions":[{"type":"key","key":"cmd+s"}]}`))
	require.NoError(t, err)
	description, ask := tool.RequiresConfirmation(raw)
	require.True(t, ask)
	require.Contains(t, description, "test.editor（进程 100，窗口 200）")
	require.Contains(t, description, "cmd+s")
}
