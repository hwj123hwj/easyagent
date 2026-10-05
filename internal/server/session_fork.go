package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/session"
)

// ─── POST /sessions/{id}/fork ────────────────────────────────────────────────

// forkRequest selects the branch point. Exactly one of EntryID (explicit JSONL
// entry) or BeforeMessageIndex (N-th user message in context order, 0 = before
// everything) is interpreted; EntryID wins when both are set.
type forkRequest struct {
	EntryID            string `json:"entry_id,omitempty"`
	BeforeMessageIndex *int   `json:"before_message_index,omitempty"`
}

// forkResponse mirrors zcode's fork contract: the new session id plus counts
// so the UI can toast "已保留 N 条消息，后面的 M 条留在原会话".
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
	mgr := s.app.SessionManager()
	if !mgr.Exists(sourceID) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var req forkRequest
	// Allow empty body: forking the full transcript is the default.
	_ = json.NewDecoder(r.Body).Decode(&req)

	source, err := s.app.LoadSession(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// NOTE: no Storage().Close() — LoadSession returns the registry cached

	entryID := req.EntryID
	branchIndex := -1
	if entryID == "" && req.BeforeMessageIndex != nil {
		if *req.BeforeMessageIndex < 0 {
			writeError(w, 400, "before_message_index must be >= 0")
			return
		}
		entryID, branchIndex, err = entryIDBeforeUserMessage(r.Context(), source, *req.BeforeMessageIndex)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if entryID != "" {
		// Reject unknown entries before creating the fork directory.
		if _, err := findEntryOnPath(r.Context(), source, entryID); err != nil {
			writeError(w, 400, fmt.Sprintf("entry %q not found in source session", entryID))
			return
		}
	}

	newID, _, err := mgr.Fork(r.Context(), sourceID, entryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	kept, keptUser, dropped, branch := forkStats(r.Context(), source, entryID, branchIndex)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(forkResponse{
		ID:                 newID,
		SourceID:           sourceID,
		KeptMessages:       kept,
		KeptUserMessages:   keptUser,
		DroppedMessages:    dropped,
		BranchUserMsgIndex: branch,
	})
}

// entryIDBeforeUserMessage walks the source session's context path and returns
// the entry id the fork leaf must point at so it contains everything BEFORE
// the beforeIndex-th user message. beforeIndex 0 forks an empty conversation;
// a value past the last user message forks the full transcript (entry "").
func entryIDBeforeUserMessage(ctx context.Context, agentSession *runtime.AgentSession, beforeIndex int) (string, int, error) {
	entries, err := agentSession.Session().Storage().GetPathToRoot(ctx, "")
	if err != nil {
		return "", -1, err
	}
	// GetPathToRoot returns leaf→root; messages arrive in that same order.
	seen := 0
	var cut int // entries kept = entries[:cut]
	for i, e := range entries {
		if e.Type == session.EntryTypeMessage && e.User != nil {
			if seen == beforeIndex {
				cut = i
				return entryIDAtCut(entries, cut), seen, nil
			}
			seen++
		}
	}
	// Past the last user message: fork the full transcript.
	return "", seen, nil
}

// entryIDAtCut converts a cut position into the entry id whose parent chain
// starts after the cut. Moving the leaf to entries[cut-1].ID keeps exactly
// entries[:cut] on the fork's path to root.
func entryIDAtCut(entries []session.Entry, cut int) string {
	if cut <= 0 {
		return "" // empty fork: leaf points at the synthetic root
	}
	return entries[cut-1].ID
}

// findEntryOnPath verifies entryID exists on the source session's current
// path to root, returning the entry when found.
func findEntryOnPath(ctx context.Context, agentSession *runtime.AgentSession, entryID string) (session.Entry, error) {
	entries, err := agentSession.Session().Storage().GetPathToRoot(ctx, "")
	if err != nil {
		return session.Entry{}, err
	}
	for _, e := range entries {
		if e.ID == entryID {
			return e, nil
		}
	}
	return session.Entry{}, os.ErrNotExist
}

// forkStats reports how many messages/user messages the fork kept and how
// many stayed behind, plus the resolved branch index for UI display.
func forkStats(ctx context.Context, agentSession *runtime.AgentSession, entryID string, branchIndex int) (kept, keptUser, dropped, branch int) {
	entries, err := agentSession.Session().Storage().GetPathToRoot(ctx, "")
	if err != nil {
		return 0, 0, 0, branchIndex
	}
	cut := len(entries)
	if entryID != "" {
		cut = -1
		for i, e := range entries {
			if e.ID == entryID {
				cut = i + 1 // entries are leaf→root; fork keeps entries[0:cut]
				break
			}
		}
		if cut < 0 {
			cut = len(entries)
		}
	}
	countUser := func(list []session.Entry) (total, users int) {
		for _, e := range list {
			if e.Type != session.EntryTypeMessage {
				continue
			}
			total++
			if e.User != nil {
				users++
			}
		}
		return total, users
	}
	kept, keptUser = countUser(entries[:cut])
	dropped, _ = countUser(entries[cut:])
	branch = branchIndex
	if branch < 0 {
		_, branch = countUser(entries[cut:])
	}
	return kept, keptUser, dropped, branch
}

// userMessageEntryIDs scans a session JSONL and returns the entry ids of
// message entries carrying a user message, in file (append) order. Exposed
// for tests and the messages endpoint's fork anchors.
func userMessageEntryIDs(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var out []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			User *struct {
				DisplayText string `json:"display_text"`
			} `json:"user,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == string(session.EntryTypeMessage) && entry.User != nil && entry.ID != "" {
			out = append(out, entry.ID)
		}
	}
	return out, scanner.Err()
}
