package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
)

type forkRequest struct {
	EntryID            *string `json:"entry_id,omitempty"`
	BeforeMessageIndex *int    `json:"before_message_index,omitempty"`
}
type forkResponse struct {
	ID                 string `json:"id"`
	SourceID           string `json:"source_id"`
	KeptMessages       int    `json:"kept_messages"`
	KeptUserMessages   int    `json:"kept_user_messages"`
	DroppedMessages    int    `json:"dropped_messages"`
	BranchUserMsgIndex int    `json:"branch_user_message_index"`
}

func (s *Server) forkSession(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("id")
	if s.rejectActiveWorkflowActor(w, sourceID) {
		return
	}
	var req forkRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, 400, "invalid fork request")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, 400, "invalid fork request")
		return
	}
	if req.EntryID != nil && req.BeforeMessageIndex != nil {
		writeError(w, 400, "select one fork point")
		return
	}
	// Share admission lock with prompts/deletion. Runtime Fork also protects
	// writes from other entrypoints and manual compaction.
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	if state := s.runs.sessions[sourceID]; state != nil && state.run != nil && runActive(state.run) {
		writeError(w, 409, "会话正在运行，请等待完成后分叉")
		return
	}
	source, err := s.app.LoadSession(r.Context(), sourceID)
	if err != nil {
		writeError(w, 404, "session not found")
		return
	}
	if source.IsBusy() {
		writeError(w, 409, "会话正在运行，请等待完成后分叉")
		return
	}
	messages, err := source.Session().BuildContext(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ids, err := source.Session().BuildContextEntryIDs(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	records, err := source.Session().Compactions(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// The compaction header is a synthetic input, not a user turn.
	if len(records) > 0 && len(messages) > 0 {
		messages, ids = messages[1:], ids[1:]
	}
	target := req.EntryID
	cut := len(messages)
	if req.BeforeMessageIndex != nil {
		index := *req.BeforeMessageIndex
		if index < 0 {
			writeError(w, 400, "before_message_index must be >= 0")
			return
		}
		seen := 0
		for i, message := range messages {
			if message.Role() != ai.RoleUser {
				continue
			}
			if seen == index {
				cut = i
				value := ""
				if i > 0 {
					value = ids[i-1]
				} else if len(records) > 0 {
					value = records[len(records)-1].ID
				}
				if index == 0 {
					value = ""
				}
				target = &value
				break
			}
			seen++
		}
	} else if target != nil {
		cut = -1
		if *target == "" {
			cut = 0
		}
		for i, id := range ids {
			if id == *target {
				cut = i + 1
				break
			}
		}
		if cut < 0 {
			writeError(w, 400, "entry not found in current context")
			return
		}
	}
	newID, _, err := source.Fork(r.Context(), target)
	if errors.Is(err, agent.ErrAgentBusy) {
		writeError(w, 409, "会话正在运行，请等待完成后分叉")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	users := 0
	for _, message := range messages[:cut] {
		if message.Role() == ai.RoleUser {
			users++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(forkResponse{ID: newID, SourceID: sourceID, KeptMessages: cut, KeptUserMessages: users, DroppedMessages: len(messages) - cut, BranchUserMsgIndex: users})
}
