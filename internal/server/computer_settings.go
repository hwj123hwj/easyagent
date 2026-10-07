package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hwj123hwj/easyagent/internal/computer"
	"github.com/hwj123hwj/easyagent/sdk/config"
)

// ─── GET /computer/settings ──────────────────────────────────────────────────

// computerSettingsView is the settings-page payload: the persisted switch plus
// live availability/permission/occupancy so the UI renders one coherent card.
type computerSettingsView struct {
	Enabled      bool     `json:"enabled"`
	Provider     string   `json:"provider"`  // "macos:cua"
	Available    bool     `json:"available"` // helper present + platform supported
	Granted      bool     `json:"granted"`   // TCC permissions granted
	Missing      []string `json:"missing,omitempty"`
	ApprovedApps []string `json:"approved_apps"` // PR-2: per-app approvals
	LeaseHolder  string   `json:"lease_holder"`  // "" = 空闲
	HelperPath   string   `json:"helper_path,omitempty"`
}

func (s *Server) getComputerSettings(w http.ResponseWriter, r *http.Request) {
	view := computerSettingsView{
		Enabled:      s.app.Config().EnableComputerUse,
		Provider:     "macos:cua",
		Available:    computer.Available(),
		HelperPath:   computer.HelperPath(),
		ApprovedApps: []string{},
		LeaseHolder:  computer.Registry.Owner(),
	}
	if view.Available {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		granted, missing, err := computer.CheckPermissions(ctx)
		cancel()
		if err == nil {
			view.Granted = granted
			view.Missing = missing
		} else {
			view.Missing = []string{"helper 探测失败"}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

// ─── POST /computer/settings ────────────────────────────────────────────────

type updateComputerSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

// updateComputerSettings flips the experimental switch. Persisted via the
// config store; takes effect for newly created sessions (loaded sessions keep
// their tool set — same semantics as the web-search toggle).
func (s *Server) updateComputerSettings(w http.ResponseWriter, r *http.Request) {
	var req updateComputerSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg := s.app.Config()
	if cfg.EnableComputerUse != req.Enabled {
		if err := config.SaveRuntimeOverrideComputerUse(s.app.Config().DataDir, req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg.EnableComputerUse = req.Enabled
		s.app.SetConfig(cfg)
	}
	s.getComputerSettings(w, r)
}

// ─── POST /computer/permissions ─────────────────────────────────────────────

// requestComputerPermissions opens the macOS permission panes (Accessibility +
// Screen Recording). The OS requires a user gesture; the desktop shell invokes
// this from the settings button click, satisfying that constraint.
func (s *Server) requestComputerPermissions(w http.ResponseWriter, r *http.Request) {
	if !computer.Available() {
		writeError(w, http.StatusServiceUnavailable, "computer use helper 不可用（仅支持 macOS，且需要 easyagent-cua-helper）")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := computer.RequestPermissions(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
