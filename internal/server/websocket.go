package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// newUpgrader 返回带同源校验的 upgrader：无 Origin（原生客户端）放行；
// 同源放行；其余仅 EA_ALLOWED_ORIGINS 白名单放行。防止恶意网页
// 借用户浏览器连接受害者本机的 WS（鉴权之外的第二道防线）。
func (s *Server) newUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			if s.allowedOrigins != nil {
				for _, allowed := range s.allowedOrigins {
					if allowed == origin {
						return true
					}
				}
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return u.Host == r.Host
		},
	}
}

// Messages carry both run identity and session-local event sequence.
type wsClientMessage struct {
	Inputs         promptInputs `json:"inputs,omitempty"`
	Type           string       `json:"type"`
	SessionID      string       `json:"session_id"`
	Prompt         string       `json:"prompt,omitempty"`
	RequestID      string       `json:"request_id,omitempty"`
	RunID          string       `json:"run_id,omitempty"`
	AfterSeq       uint64       `json:"after_seq,omitempty"`
	ConfirmationID string       `json:"confirmation_id,omitempty"`
	Approved       bool         `json:"approved"`
	Reason         string       `json:"reason,omitempty"`
	Model          string       `json:"model,omitempty"`
	Provider       string       `json:"provider,omitempty"`
}
type wsServerMessage struct {
	Prompt               string                 `json:"prompt,omitempty"`
	Queue                *messageQueue          `json:"queue,omitempty"`
	Type                 string                 `json:"type"`
	SessionID            string                 `json:"session_id,omitempty"`
	RunID                string                 `json:"run_id,omitempty"`
	RequestID            string                 `json:"request_id,omitempty"`
	Seq                  uint64                 `json:"seq,omitempty"`
	Reset                bool                   `json:"reset,omitempty"`
	State                string                 `json:"state,omitempty"`
	Duplicate            bool                   `json:"duplicate,omitempty"`
	Code                 string                 `json:"code,omitempty"`
	Event                any                    `json:"event,omitempty"`
	Events               any                    `json:"events,omitempty"`
	Run                  *sessionRun            `json:"run,omitempty"`
	Messages             []map[string]any       `json:"messages,omitempty"`
	PendingConfirmations []*pendingConfirmation `json:"pending_confirmations,omitempty"`
	Confirmation         *pendingConfirmation   `json:"confirmation,omitempty"`
	Streaming            bool                   `json:"streaming"`
	Retryable            bool                   `json:"retryable,omitempty"`
	Provider             string                 `json:"provider,omitempty"`
	Model                string                 `json:"model,omitempty"`
	Message              string                 `json:"message,omitempty"`
}

// A slow/disconnected subscriber is detached, never allowed to cancel or stall its run.
type wsConn struct {
	conn     *websocket.Conn
	outgoing chan []byte
	done     chan struct{}
	once     sync.Once
	legacy   bool
}

func (w *wsConn) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case <-w.done:
		return errors.New("websocket closed")
	default:
	}
	select {
	case w.outgoing <- data:
		return nil
	default:
		w.close()
		return errors.New("websocket subscriber too slow")
	}
}
func (w *wsConn) close() {
	w.once.Do(func() {
		close(w.done)
		if w.conn != nil {
			_ = w.conn.Close()
		}
	})
}
func (w *wsConn) writeLoop() {
	defer w.close()
	for {
		select {
		case <-w.done:
			return
		case data := <-w.outgoing:
			_ = w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if w.conn.WriteMessage(websocket.TextMessage, data) != nil {
				return
			}
		}
	}
}
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.wsAuthorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	conn, err := s.newUpgrader().Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	conn.SetReadLimit(1024 * 1024)
	ws := &wsConn{conn: conn, outgoing: make(chan []byte, 512), done: make(chan struct{})}
	go ws.writeLoop()
	defer ws.close()
	defer s.unsubscribeRun(ws, "")
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg wsClientMessage
		if json.Unmarshal(data, &msg) != nil {
			_ = ws.writeJSON(wsServerMessage{Type: "error", Code: "invalid_json", Message: "invalid JSON"})
			continue
		}
		switch msg.Type {
		case "ping":
			_ = ws.writeJSON(wsServerMessage{Type: "pong"})
		case "prompt":
			s.handleWSPrompt(ws, msg)
		case "subscribe":
			if _, err := s.runSnapshot(msg.SessionID, msg.AfterSeq, msg.RunID, ws); err != nil {
				s.writeWSError(ws, msg, err)
			}
		case "unsubscribe":
			s.unsubscribeRun(ws, msg.SessionID)
			_ = ws.writeJSON(wsServerMessage{Type: "unsubscribed", SessionID: msg.SessionID})
		case "cancel":
			if err := s.cancelRun(msg.SessionID, msg.RunID); err != nil {
				s.writeWSError(ws, msg, err)
			} else {
				_ = ws.writeJSON(wsServerMessage{Type: "cancelled", SessionID: msg.SessionID, RunID: msg.RunID})
			}
		case "confirm":
			if err := s.confirmRun(msg.SessionID, msg.RunID, msg.ConfirmationID, msg.Approved, msg.Reason); err != nil {
				s.writeWSError(ws, msg, err)
			} else {
				_ = ws.writeJSON(wsServerMessage{Type: "confirmed", SessionID: msg.SessionID, RunID: msg.RunID})
			}
		case "switch_model":
			s.handleWSSwitchModel(ws, msg)
		default:
			s.writeWSError(ws, msg, &runAdmissionError{"unknown_type", "unknown message type: " + msg.Type})
		}
	}
}
func (s *Server) writeWSError(ws *wsConn, msg wsClientMessage, err error) {
	response := wsServerMessage{Type: "error", SessionID: msg.SessionID, RunID: msg.RunID, RequestID: msg.RequestID, Message: err.Error()}
	var admission *runAdmissionError
	if errors.As(err, &admission) {
		response.Code = admission.code
		response.Retryable = admission.code == "updating"
	}
	_ = ws.writeJSON(response)
}
func (s *Server) handleWSPrompt(ws *wsConn, msg wsClientMessage) {
	ws.legacy = msg.RequestID == ""
	run, duplicate, err := s.startRun(msg.SessionID, msg.Prompt, msg.RequestID, msg.Inputs)
	if err != nil {
		s.writeWSError(ws, msg, err)
		return
	}
	s.runs.mu.Lock()
	ack := wsServerMessage{Type: "accepted", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID, State: run.State, Duplicate: duplicate, Prompt: displayRunPrompt(run)}
	_ = ws.writeJSON(ack)
	// Existing web/bridge clients keep receiving the original session/status/event messages.
	_ = ws.writeJSON(wsServerMessage{Type: "session_id", SessionID: run.SessionID, RunID: run.ID, RequestID: run.RequestID})
	_ = ws.writeJSON(wsServerMessage{Type: "status", SessionID: run.SessionID, RunID: run.ID, State: run.State, Streaming: runActive(run)})
	s.runs.mu.Unlock()
	_, _ = s.runSnapshot(run.SessionID, 0, "", ws)
}

// handleWSSwitchModel changes the model for a session.
func (s *Server) handleWSSwitchModel(ws *wsConn, msg wsClientMessage) {
	release, ok := s.activity.begin()
	if !ok {
		_ = ws.writeJSON(wsServerMessage{Type: "error", SessionID: msg.SessionID, Message: "服务正在更新，请稍后重试"})
		return
	}
	defer release()
	if msg.Model == "" {
		_ = ws.writeJSON(wsServerMessage{
			Type:    "error",
			Message: "model is empty",
		})
		return
	}

	ctx := context.Background()

	sess, err := s.resolveSession(ctx, msg.SessionID)
	if err != nil {
		_ = ws.writeJSON(wsServerMessage{
			Type:    "error",
			Message: "session error: " + err.Error(),
		})
		return
	}

	if err := sess.SwitchModel(ctx, msg.Model, msg.Provider); err != nil {
		_ = ws.writeJSON(wsServerMessage{
			Type:      "error",
			SessionID: sess.SessionID(),
			Message:   "switch model failed: " + err.Error(),
		})
		return
	}

	provider, modelID := sess.ModelInfo()
	_ = ws.writeJSON(wsServerMessage{
		Type:      "model_info",
		SessionID: sess.SessionID(),
		Provider:  provider,
		Model:     modelID,
	})
}
