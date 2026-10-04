package server

import (
	"encoding/json"
	"github.com/hwj123hwj/easyagent/sdk/sessionmgr"
	"net/http"
)

func (s *Server) patchSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.app.SessionManager().Exists(id) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var patch sessionmgr.PreferencePatch
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session preferences")
		return
	}
	if err := s.app.SessionManager().UpdatePreferences(id, patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
