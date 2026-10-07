package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/internal/computer"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/require"
)

func TestComputerSettingsTogglePersistsAcrossRestart(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	dataDir := s.app.Config().DataDir

	// Default is off.
	{
		req := httptest.NewRequest("GET", "/computer/settings", nil)
		rec := httptest.NewRecorder()
		s.getComputerSettings(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var view struct {
			Enabled  bool   `json:"enabled"`
			Provider string `json:"provider"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		require.False(t, view.Enabled, "experimental feature must default to off")
		require.Equal(t, "macos:cua", view.Provider)
	}

	// Toggle on: response flips and settings.json records the override.
	{
		req := httptest.NewRequest("POST", "/computer/settings",
			bytes.NewReader([]byte(`{"enabled":true}`)))
		rec := httptest.NewRecorder()
		s.updateComputerSettings(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var view struct {
			Enabled bool `json:"enabled"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		require.True(t, view.Enabled)

		data, err := os.ReadFile(filepath.Join(dataDir, "settings.json"))
		require.NoError(t, err)
		require.Contains(t, string(data), `"enable_computer_use": true`)

		// In-memory config flipped too: new sessions will expose the tool.
		require.True(t, s.app.Config().EnableComputerUse)
	}

	// Simulated restart: fresh config load applies the override on top of env.
	{
		fresh := config.Config{}
		fresh.LoadRuntimeOverrides(dataDir)
		require.True(t, fresh.EnableComputerUse, "override must survive restart")
	}

	// Approval policy: switch to auto, invalid values rejected, clear apps.
	{
		req := httptest.NewRequest("POST", "/computer/settings",
			bytes.NewReader([]byte(`{"approval_policy":"auto"}`)))
		rec := httptest.NewRecorder()
		s.updateComputerSettings(rec, req)
		var view struct {
			ApprovalPolicy string `json:"approval_policy"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		require.Equal(t, "auto", view.ApprovalPolicy)

		// 非法策略被拒且不落盘
		req = httptest.NewRequest("POST", "/computer/settings",
			bytes.NewReader([]byte(`{"approval_policy":"yolo"}`)))
		rec = httptest.NewRecorder()
		s.updateComputerSettings(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)

		// 清空已批应用
		req = httptest.NewRequest("POST", "/computer/settings",
			bytes.NewReader([]byte(`{"clear_approved_apps":true}`)))
		rec = httptest.NewRecorder()
		s.updateComputerSettings(rec, req)
		var view2 struct {
			ApprovedApps []string `json:"approved_apps"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view2))
		require.Empty(t, view2.ApprovedApps)
	}

	// Toggle back off.
	{
		req := httptest.NewRequest("POST", "/computer/settings",
			bytes.NewReader([]byte(`{"enabled":false}`)))
		rec := httptest.NewRecorder()
		s.updateComputerSettings(rec, req)

		var view struct {
			Enabled bool `json:"enabled"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		require.False(t, view.Enabled)
		require.False(t, s.app.Config().EnableComputerUse)
	}
}

func TestComputerToolSchemaIsReadOnly(t *testing.T) {
	tool := computer.NewTool(t.TempDir())

	// Only read-only actions pass validation.
	valid, err := tool.Validate(json.RawMessage(`{"action":"screenshot"}`))
	require.NoError(t, err)
	require.NotNil(t, valid)

	_, err = tool.Validate(json.RawMessage(`{"action":"get_app_state"}`))
	require.NoError(t, err)

	// Control actions go through strict batch validation (PR-2), and always
	// demand a user confirmation before Execute.
	normalized, err := tool.Validate(json.RawMessage(
		`{"action":"perform_action","actions":[{"type":"click","x":10,"y":20}]}`))
	require.NoError(t, err)
	desc, needConfirm := tool.RequiresConfirmation(normalized)
	require.True(t, needConfirm, "perform_action must require per-batch approval")
	require.Contains(t, desc, "左键点击 (10, 20)")

	// Headless entrypoints must refuse the control tool entirely.
	require.True(t, tool.RequiresConfirmationAvailable())

	// Unknown or missing actions stay rejected.
	_, err = tool.Validate(json.RawMessage(`{"action":"click"}`))
	require.Error(t, err)
	_, err = tool.Validate(json.RawMessage(`{}`))
	require.ErrorContains(t, err, "action")

	// Malformed control batches are rejected too.
	_, err = tool.Validate(json.RawMessage(`{"action":"perform_action"}`))
	require.ErrorContains(t, err, "actions 数组")
}
