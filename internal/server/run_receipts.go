package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// Receipts persist admission and the final result, not the live event stream.
// An interrupted process must never replay a prompt that may have side effects.
type runReceipt struct {
	UserEntryID      string              `json:"user_entry_id,omitempty"`
	AssistantEntryID string              `json:"assistant_entry_id,omitempty"`
	Usage            ai.Usage            `json:"usage,omitempty"` // cumulative tokens across all turns
	Inputs           promptInputs        `json:"inputs,omitempty"`
	RunID            string              `json:"run_id"`
	RequestID        string              `json:"request_id"`
	SessionID        string              `json:"session_id"`
	Prompt           string              `json:"prompt"`
	State            string              `json:"state"`
	StartedAt        time.Time           `json:"started_at"`
	EndedAt          *time.Time          `json:"ended_at,omitempty"`
	Error            string              `json:"error,omitempty"`
	Result           ai.AssistantMessage `json:"result"`
}

func (s *Server) receiptPath(requestID string) string {
	hash := sha256.Sum256([]byte(requestID))
	return filepath.Join(s.app.Config().DataDir, "requests", hex.EncodeToString(hash[:])+".json")
}

func (s *Server) saveRunReceipt(run *sessionRun) error {
	data, err := json.Marshal(runReceipt{UserEntryID: run.UserEntryID, AssistantEntryID: run.AssistantEntryID, Usage: run.Usage, Inputs: run.Inputs, RunID: run.ID, RequestID: run.RequestID, SessionID: run.SessionID, Prompt: run.Prompt, State: run.State, StartedAt: run.StartedAt, EndedAt: run.EndedAt, Error: run.Error, Result: run.last})
	if err != nil {
		return err
	}
	path := s.receiptPath(run.RequestID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s *Server) restoreRunReceipts() {
	files, err := os.ReadDir(filepath.Join(s.app.Config().DataDir, "requests"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		slog.Error("cannot list run receipts", "error", err)
		return
	}
	runs := []*sessionRun{}
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.app.Config().DataDir, "requests", file.Name()))
		if err != nil {
			continue
		}
		var receipt runReceipt
		if json.Unmarshal(data, &receipt) != nil || !validRunReceipt(receipt) || filepath.Base(s.receiptPath(receipt.RequestID)) != file.Name() {
			continue
		}
		run := restoreReceipt(receipt)
		if run.State == "interrupted" {
			if err := s.saveRunReceipt(run); err != nil {
				slog.Error("cannot save interrupted receipt", "request_id", run.RequestID, "error", err)
			}
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.Before(runs[j].StartedAt) })
	for _, run := range runs {
		s.runs.requests[run.RequestID] = run
		s.runs.requestOrder = append(s.runs.requestOrder, run.RequestID)
		s.runs.sessionLocked(run.SessionID).run = run
	}
	s.runs.pruneRequestsLocked()
}

func validRunReceipt(receipt runReceipt) bool {
	if receipt.RequestID == "" || len(receipt.RequestID) > 128 || receipt.SessionID == "" || receipt.RunID == "" {
		return false
	}
	switch receipt.State {
	case "running", "waiting_confirmation", "completed", "failed", "cancelled", "interrupted":
		return true
	}
	return false
}

func restoreReceipt(receipt runReceipt) *sessionRun {
	run := &sessionRun{UserEntryID: receipt.UserEntryID, AssistantEntryID: receipt.AssistantEntryID, Usage: receipt.Usage, Inputs: receipt.Inputs, ID: receipt.RunID, RequestID: receipt.RequestID, SessionID: receipt.SessionID, Prompt: receipt.Prompt, State: receipt.State, StartedAt: receipt.StartedAt, EndedAt: receipt.EndedAt, Error: receipt.Error, last: receipt.Result, restored: true, done: make(chan struct{}), pending: map[string]*pendingConfirmation{}}
	if runActive(run) {
		run.State, run.Error = "interrupted", "服务重启导致任务中断；已接受的消息不会自动重复执行，请检查历史后决定是否重新发送"
		ended := time.Now()
		run.EndedAt = &ended
	}
	close(run.done)
	return run
}

func (s *Server) hydrateRestoredRun(run *sessionRun) error {
	if !s.app.SessionManager().Exists(run.SessionID) {
		run.baseline = []map[string]any{}
		return nil
	}
	sess, err := s.resolveSession(s.ctx, run.SessionID)
	if err != nil {
		return err
	}
	messages, err := sess.Session().BuildDisplayContext(s.ctx)
	if err != nil {
		return err
	}
	run.baseline = serializeSessionContext(sess, messages)
	return nil
}

// loadRunReceiptLocked restores only proof of acceptance. Active receipts are
// terminally interrupted: a human may send a new request, but retries of the
// accepted request cannot cause a second execution after a core restart.
func (s *Server) loadRunReceiptLocked(requestID string) (*sessionRun, error) {
	data, err := os.ReadFile(s.receiptPath(requestID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取消息接受凭据；为避免重复执行，消息尚未执行")
	}
	var receipt runReceipt
	if json.Unmarshal(data, &receipt) != nil || receipt.RequestID != requestID || !validRunReceipt(receipt) {
		return nil, fmt.Errorf("消息接受凭据无效；为避免重复执行，消息尚未执行")
	}
	run := restoreReceipt(receipt)
	if run.State == "interrupted" {
		if err := s.saveRunReceipt(run); err != nil {
			return nil, fmt.Errorf("无法保存中断状态；为避免重复执行，消息尚未执行")
		}
	}
	s.runs.requests[requestID] = run
	s.runs.requestOrder = append(s.runs.requestOrder, requestID)
	s.runs.pruneRequestsLocked()
	state := s.runs.sessionLocked(run.SessionID)
	if state.run == nil {
		state.run = run
	}
	return run, nil
}

// ─── GET /usage/summary ─────────────────────────────────────────────────

// usageBucket aggregates token usage for one session (or the grand total).
type usageBucket struct {
	SessionID         string     `json:"session_id,omitempty"` // "" in the totals bucket
	RunCount          int        `json:"run_count"`
	InputTokens       int        `json:"input_tokens"`
	OutputTokens      int        `json:"output_tokens"`
	CachedInputTokens *int       `json:"cached_input_tokens,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
}

// usageSummaryResponse is the payload of GET /usage/summary.
type usageSummaryResponse struct {
	Since    time.Time     `json:"since"`
	Until    time.Time     `json:"until"`
	Total    usageBucket   `json:"total"`
	Sessions []usageBucket `json:"sessions"`
}

// usageSummary aggregates token usage from run receipts in [since, until).
// Receipts are the source of truth: they persist across restarts and cover
// every accepted prompt, unlike in-memory runs which vanish on shutdown.
func (s *Server) usageSummary(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	since := now.AddDate(0, 0, -30)
	until := now
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			since = parsed
		}
	}
	if raw := r.URL.Query().Get("until"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			until = parsed
		}
	}

	total := usageBucket{}
	perSession := map[string]*usageBucket{}

	forEachRunReceipt(r.Context(), s.app.Config().DataDir, func(receipt runReceipt) bool {
		started := receipt.StartedAt
		if started.Before(since) || !started.Before(until) {
			return true // keep scanning
		}
		bucket := perSession[receipt.SessionID]
		if bucket == nil {
			bucket = &usageBucket{SessionID: receipt.SessionID}
			perSession[receipt.SessionID] = bucket
		}
		for _, bucket := range []*usageBucket{bucket, &total} {
			bucket.RunCount++
			bucket.InputTokens += receipt.Usage.InputTokens
			bucket.OutputTokens += receipt.Usage.OutputTokens
			if receipt.Usage.CachedInputTokens != nil {
				if bucket.CachedInputTokens == nil {
					cached := *receipt.Usage.CachedInputTokens
					bucket.CachedInputTokens = &cached
				} else {
					*bucket.CachedInputTokens += *receipt.Usage.CachedInputTokens
				}
			}
			if bucket.LastUsedAt == nil || started.After(*bucket.LastUsedAt) {
				stamp := started
				bucket.LastUsedAt = &stamp
			}
		}
		return true
	})

	sessions := make([]usageBucket, 0, len(perSession))
	for _, bucket := range perSession {
		sessions = append(sessions, *bucket)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].InputTokens+sessions[i].OutputTokens > sessions[j].InputTokens+sessions[j].OutputTokens
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(usageSummaryResponse{Since: since, Until: until, Total: total, Sessions: sessions})
}

// forEachRunReceipt streams every persisted run receipt, skipping unreadable
// files. cancel-return stops the scan early.
func forEachRunReceipt(ctx context.Context, dataDir string, visit func(runReceipt) bool) {
	entries, err := os.ReadDir(filepath.Join(dataDir, "requests"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dataDir, "requests", entry.Name()))
		if err != nil {
			continue
		}
		var receipt runReceipt
		if json.Unmarshal(data, &receipt) != nil || !validRunReceipt(receipt) {
			continue
		}
		if !visit(receipt) {
			return
		}
	}
}
