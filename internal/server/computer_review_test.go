package server

import (
	"bytes"
	"github.com/hwj123hwj/easyagent/internal/agents/coding"
	"github.com/hwj123hwj/easyagent/internal/computer"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvalidComputerPatchDoesNotPartiallyPersist(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	rec := httptest.NewRecorder()
	s.updateComputerSettings(rec, httptest.NewRequest("POST", "/computer/settings", bytes.NewBufferString(`{"enabled":true,"approval_policy":"invalid"}`)))
	require.Equal(t, 400, rec.Code)
	cfg := config.Default()
	cfg.LoadRuntimeOverrides(s.app.Config().DataDir)
	require.False(t, cfg.EnableComputerUse)
}
func TestComputerSettingsAuditUpdatesImmediately(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	require.NoError(t, config.RecordApprovedApp(s.app.Config().DataDir, "test.editor"))
	rec := httptest.NewRecorder()
	s.getComputerSettings(rec, httptest.NewRequest("GET", "/computer/settings", nil))
	require.Contains(t, rec.Body.String(), "test.editor")
}

func TestComputerSettingsReachNewApplicationSnapshot(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	old := s.app.ResolveApplication("coding")
	rec := httptest.NewRecorder()
	s.updateComputerSettings(rec, httptest.NewRequest("POST", "/computer/settings", strings.NewReader(`{"enabled":true,"approval_policy":"auto"}`)))
	require.Equal(t, 200, rec.Code)
	current := s.app.ResolveApplication("coding")
	require.True(t, current.(coding.CodingApplication).Cfg.EnableComputerUse)
	require.False(t, old.(coding.CodingApplication).Cfg.EnableComputerUse)
	found := false
	for _, tool := range current.BuildTools(runtime.ToolBuildOptions{SessionID: "test", Workspace: t.TempDir()}) {
		if tool.Name() == "computer" {
			found = true
			_, asks := tool.(*computer.Tool).RequiresConfirmation([]byte(`{"action":"perform_action","actions":[{"type":"key","key":"cmd+s"}]}`))
			require.False(t, asks)
		}
	}
	require.True(t, found)
}
