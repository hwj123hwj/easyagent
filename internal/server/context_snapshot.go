package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

func (s *Server) getContextSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.rejectActiveWorkflowActor(w, id) || s.rejectActiveRun(w, id) {
		return
	}
	sess, err := s.app.LoadSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	snapshot, err := sess.ContextSnapshot(r.Context())
	if errors.Is(err, agent.ErrAgentBusy) {
		writeError(w, http.StatusConflict, "会话正在运行，请等待完成后查看上下文")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(snapshot)
}

// Display compaction records as records, rather than rendering the synthetic
// summary header as if it were a new user message.
func serializeSessionContext(sess *runtime.AgentSession, messages []ai.Message) []map[string]any {
	records, err := sess.Session().Compactions(context.Background())
	if err != nil || len(records) == 0 {
		return serializeRunMessages(messages)
	}
	result := make([]map[string]any, 0, len(records)+len(messages))
	for _, record := range records {
		result = append(result, map[string]any{"role": "compaction", "content": record.Summary, "compaction": record})
	}
	if len(messages) > 0 {
		messages = messages[1:]
	}
	return append(result, serializeRunMessages(messages)...)
}
