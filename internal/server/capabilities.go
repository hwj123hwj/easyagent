package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) getCapabilities(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.app.SessionManager().Exists(id) {
		writeError(w, 404, "session not found")
		return
	}
	sess, err := s.resolveSession(r.Context(), id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	skills := []map[string]any{}
	for _, skill := range sess.LoadedSkills() {
		scope := "personal"
		rel, err := filepath.Rel(sess.Workspace(), skill.FilePath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			scope = "project"
		}
		skills = append(skills, map[string]any{"name": skill.Name, "description": skill.Description, "source": skill.FilePath, "scope": scope, "model_invocation": !skill.DisableModelInvocation})
	}
	workflows := []any{}
	for _, run := range s.app.DynamicWorkflows().List() {
		if run.Parent == id {
			workflows = append(workflows, run)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"workspace": sess.Workspace(), "skills": skills, "workflows": workflows, "workflow_entry": "/workflow", "tools": sess.ToolNames()})
}
