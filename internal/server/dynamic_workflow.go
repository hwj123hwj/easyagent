package server

import (
	"context"
	"encoding/json"
	"net/http"
)

func (s *Server) registerDynamicWorkflowRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /dynamic-workflows", func(w http.ResponseWriter, r *http.Request) {
		problem := ""
		if err := s.app.DynamicWorkflows().Available(); err != nil {
			problem = err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"runs": s.app.DynamicWorkflows().List(), "available": problem == "", "error": problem})
	})
	mux.HandleFunc("GET /dynamic-workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, err := s.app.DynamicWorkflows().Get(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(run)
	})
	mux.HandleFunc("POST /dynamic-workflows/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := s.app.DynamicWorkflows().Cancel(r.PathValue("id")); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /dynamic-workflows/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Acknowledge bool `json:"acknowledge_incomplete_actions"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request) != nil || !request.Acknowledge {
			writeError(w, http.StatusBadRequest, "恢复可能重试未完成操作，请先确认执行记录")
			return
		}
		run, err := s.app.DynamicWorkflows().Resume(context.Background(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(run)
	})
}

func (s *Server) rejectActiveWorkflowActor(w http.ResponseWriter, id string) bool {
	if s.app.DynamicWorkflows().ActiveActorSession(id) {
		writeError(w, http.StatusConflict, "此 Actor 正由工作流管理，请在工作流页取消或等待完成")
		return true
	}
	return false
}
