package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type queuedMessage struct {
	Inputs    promptInputs `json:"inputs,omitempty"`
	ID        string       `json:"id"`
	Prompt    string       `json:"prompt"`
	CreatedAt time.Time    `json:"created_at"`
}
type messageQueue struct {
	Items    []queuedMessage `json:"items"`
	Paused   bool            `json:"paused"`
	Revision uint64          `json:"revision"`
	Seen     []string        `json:"-"`
}
type queueMutation struct {
	Inputs promptInputs `json:"inputs,omitempty"`
	Action string       `json:"action"`
	ID     string       `json:"id"`
	Prompt string       `json:"prompt"`
}
type queueFile struct {
	Items    []queuedMessage `json:"items"`
	Paused   bool            `json:"paused"`
	Revision uint64          `json:"revision"`
	Seen     []string        `json:"seen"`
}

func (s *Server) queueLocked(id string) (*messageQueue, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) || !s.app.SessionManager().Exists(id) {
		return nil, fmt.Errorf("session not found")
	}
	if s.queues == nil {
		s.queues = map[string]*messageQueue{}
	}
	fi, statErr := os.Lstat(filepath.Dir(s.queuePath(id)))
	if statErr != nil || !fi.IsDir() {
		return nil, fmt.Errorf("invalid session directory")
	}
	if q := s.queues[id]; q != nil {
		return q, nil
	}
	q := &messageQueue{Items: []queuedMessage{}}
	data, err := os.ReadFile(s.queuePath(id))
	if err == nil {
		var f queueFile
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("invalid persisted message queue")
		}
		q.Items, q.Seen, q.Revision = f.Items, f.Seen, f.Revision
		// Never silently replay pending external actions after a process restart.
		q.Paused = len(q.Items) > 0 || f.Paused
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if q.Items == nil {
		q.Items = []queuedMessage{}
	}
	s.queues[id] = q
	return q, nil
}
func (s *Server) queuePath(id string) string {
	return filepath.Join(s.app.SessionManager().SessionsDir(), id, "queue.json")
}
func (s *Server) saveQueue(id string, q *messageQueue) error {
	data, err := json.Marshal(queueFile{Items: q.Items, Paused: q.Paused, Revision: q.Revision, Seen: q.Seen})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.queuePath(id)), ".queue-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), s.queuePath(id))
}
func copyQueue(q *messageQueue) messageQueue {
	return messageQueue{Items: append([]queuedMessage{}, q.Items...), Paused: q.Paused, Revision: q.Revision, Seen: append([]string{}, q.Seen...)}
}
func (s *Server) readQueue(id string) (messageQueue, error) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	q, err := s.queueLocked(id)
	if err != nil {
		return messageQueue{}, err
	}
	return copyQueue(q), nil
}
func (s *Server) publishQueue(id string, q messageQueue) {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	s.runs.publishLocked(s.runs.sessionLocked(id), wsServerMessage{Type: "queue", SessionID: id, Queue: &q})
}
func (s *Server) changeQueue(id string, mutation queueMutation) (messageQueue, error) {
	s.queueMu.Lock()
	q, err := s.queueLocked(id)
	if err != nil {
		s.queueMu.Unlock()
		return messageQueue{}, err
	}
	next := copyQueue(q)
	index := -1
	for i, item := range next.Items {
		if item.ID == mutation.ID {
			index = i
			break
		}
	}
	switch mutation.Action {
	case "add":
		if mutation.ID == "" || len(mutation.ID) > 120 || strings.TrimSpace(mutation.Prompt) == "" || len(mutation.Prompt) > 128*1024 {
			err = fmt.Errorf("invalid queued message")
			break
		}
		known := index >= 0
		for _, previous := range next.Seen {
			if previous == mutation.ID {
				known = true
			}
		}
		if !known {
			sess, resolveErr := s.resolveSession(s.ctx, id)
			if resolveErr != nil {
				err = resolveErr
				break
			}
			if _, contextErr := s.buildPromptMessage(sess, mutation.Prompt, mutation.Inputs); contextErr != nil {
				err = contextErr
				break
			}
			if len(next.Items) >= 32 {
				err = fmt.Errorf("queue is full (32 messages)")
				break
			}
			next.Items = append(next.Items, queuedMessage{ID: mutation.ID, Prompt: mutation.Prompt, Inputs: mutation.Inputs, CreatedAt: time.Now()})
			next.Seen = append(next.Seen, mutation.ID)
		}
	case "edit":
		if index < 0 {
			err = fmt.Errorf("message already started or removed")
			break
		}
		if strings.TrimSpace(mutation.Prompt) == "" || len(mutation.Prompt) > 128*1024 {
			err = fmt.Errorf("invalid queued message")
			break
		}
		next.Items[index].Prompt = mutation.Prompt
	case "remove":
		if index >= 0 {
			next.Items = append(next.Items[:index], next.Items[index+1:]...)
		}
	case "pause":
		next.Paused = true
	case "resume":
		next.Paused = false
	case "send_now":
		if index < 0 {
			err = fmt.Errorf("message already started or removed")
			break
		}
		item := next.Items[index]
		next.Items = append([]queuedMessage{item}, append(next.Items[:index:index], next.Items[index+1:]...)...)
		next.Paused = false
	default:
		err = fmt.Errorf("unknown queue action")
	}
	if err == nil {
		next.Revision++
		err = s.saveQueue(id, &next)
	}
	if err == nil {
		*q = next
	}
	result := copyQueue(q)
	s.queueMu.Unlock()
	if err != nil {
		return result, err
	}
	s.publishQueue(id, result)
	if mutation.Action == "send_now" {
		s.runs.mu.Lock()
		state := s.runs.sessionLocked(id)
		if state.run != nil && runActive(state.run) {
			state.run.cancel()
		}
		s.runs.mu.Unlock()
	}
	go s.pumpQueue(id)
	return result, nil
}
func (s *Server) pumpQueue(id string) {
	s.queueMu.Lock()
	q, err := s.queueLocked(id)
	if err != nil || q.Paused || len(q.Items) == 0 || s.ctx.Err() != nil {
		s.queueMu.Unlock()
		return
	}
	item := q.Items[0]
	run, duplicate, err := s.startRun(id, item.Prompt, queueRequestID(id, item.ID), item.Inputs)
	if err != nil {
		var failure *runAdmissionError
		if errors.As(err, &failure) && failure.code == "invalid_context" {
			q.Paused = true
			q.Revision++
			_ = s.saveQueue(id, q)
		}
		result := copyQueue(q)
		s.queueMu.Unlock()
		if result.Paused {
			s.publishQueue(id, result)
		}
		var admission *runAdmissionError
		if !errors.As(err, &admission) || admission.code != "busy" {
			slog.Warn("queue admission delayed", "session", id, "error", err)
		}
		return
	}
	next := copyQueue(q)
	next.Items = next.Items[1:]
	next.Revision++
	if duplicate && run.State == "interrupted" {
		next.Paused = true
	}
	err = s.saveQueue(id, &next)
	if err == nil {
		*q = next
	}
	result := copyQueue(q)
	s.queueMu.Unlock()
	if err != nil {
		slog.Error("cannot persist dispatched queue", "session", id, "error", err)
		return
	}
	s.publishQueue(id, result)
	if duplicate && !runActive(run) && !result.Paused {
		go s.pumpQueue(id)
	}
}
func (s *Server) messageQueueHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var q messageQueue
	var err error
	if r.Method == http.MethodGet {
		q, err = s.readQueue(id)
	} else {
		var mutation queueMutation
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 160*1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&mutation) != nil {
			writeError(w, 400, "invalid queue operation")
			return
		}
		q, err = s.changeQueue(id, mutation)
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(q)
}

func queueRequestID(session, id string) string {
	hash := sha256.Sum256([]byte(session + "\x00" + id))
	return "queue:" + hex.EncodeToString(hash[:])
}
