package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hwj123hwj/easyagent/sdk/agent"
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

// wsClientMessage represents a message from the client to the server.
type wsClientMessage struct {
	Type      string `json:"type"`       // "prompt", "cancel", "switch_model", "ping"
	SessionID string `json:"session_id"` // Target session
	Prompt    string `json:"prompt,omitempty"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"` // For switch_model: optional provider change
}

// wsServerMessage represents a message from the server to the client.
type wsServerMessage struct {
	Type      string `json:"type"` // "event", "session_id", "status", "model_info", "error", "pong"
	SessionID string `json:"session_id,omitempty"`
	Event     any    `json:"event,omitempty"`     // AgentStreamEvent when type="event"
	Streaming bool   `json:"streaming,omitempty"` // When type="status"
	Provider  string `json:"provider,omitempty"`  // When type="model_info"
	Model     string `json:"model,omitempty"`     // When type="model_info"
	Message   string `json:"message,omitempty"`   // When type="error"
}

// wsConn wraps a WebSocket connection with a mutex for safe concurrent writes.
type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsConn) writeJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *wsConn) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

// handleWebSocket handles the WebSocket connection at GET /ws.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级前鉴权（WS 路由绕过 REST 中间件）；与 REST 同一访问控制模型
	if !s.wsAuthorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	conn, err := s.newUpgrader().Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	slog.Info("websocket connected", "remote", conn.RemoteAddr(), "origin", r.Header.Get("Origin"))

	ws := &wsConn{conn: conn}
	defer ws.close()

	// Track active prompts for cancellation
	var (
		mu         sync.Mutex
		cancelFunc context.CancelFunc
	)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			// Client disconnected or error
			slog.Info("websocket read ended", "error", err)
			break
		}

		var msg wsClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			slog.Debug("websocket invalid message", "error", err)
			continue
		}

		switch msg.Type {
		case "ping":
			_ = ws.writeJSON(wsServerMessage{Type: "pong"})

		case "prompt":
			s.handleWSPrompt(ws, msg, &mu, &cancelFunc)

		case "cancel":
			s.handleWSCancel(&mu, &cancelFunc, msg.SessionID)

		case "switch_model":
			s.handleWSSwitchModel(ws, msg)

		default:
			_ = ws.writeJSON(wsServerMessage{
				Type:    "error",
				Message: "unknown message type: " + msg.Type,
			})
		}
	}

	// Clean up any running prompt
	mu.Lock()
	if cancelFunc != nil {
		cancelFunc()
	}
	mu.Unlock()
}

// handleWSPrompt processes a "prompt" message from the client.
func (s *Server) handleWSPrompt(ws *wsConn, msg wsClientMessage, mu *sync.Mutex, cancelFuncPtr *context.CancelFunc) {
	if s.app.DynamicWorkflows().ActiveActorSession(msg.SessionID) {
		_ = ws.writeJSON(wsServerMessage{Type: "error", Message: "此 Actor 正由工作流管理，请在工作流页取消或等待完成"})
		return
	}
	if msg.Prompt == "" {
		_ = ws.writeJSON(wsServerMessage{
			Type:    "error",
			Message: "prompt is empty",
		})
		return
	}

	// Cancel any existing prompt for this connection
	mu.Lock()
	if *cancelFuncPtr != nil {
		(*cancelFuncPtr)()
	}
	mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	mu.Lock()
	*cancelFuncPtr = cancel
	mu.Unlock()

	// Resolve session
	sess, err := s.resolveSession(ctx, msg.SessionID)
	if err != nil {
		cancel()
		_ = ws.writeJSON(wsServerMessage{
			Type:    "error",
			Message: "session error: " + err.Error(),
		})
		return
	}

	sessionID := sess.SessionID()

	// Send session_id to client (in case it was auto-created)
	_ = ws.writeJSON(wsServerMessage{
		Type:      "session_id",
		SessionID: sessionID,
	})

	// Send streaming status
	_ = ws.writeJSON(wsServerMessage{
		Type:      "status",
		SessionID: sessionID,
		Streaming: true,
	})

	// Start streaming
	stream, err := sess.PromptStream(ctx, msg.Prompt)
	if err != nil {
		cancel()
		errMsg := err.Error()
		if err == agent.ErrAgentBusy {
			errMsg = "agent is busy processing another request"
		}
		_ = ws.writeJSON(wsServerMessage{
			Type:      "error",
			SessionID: sessionID,
			Message:   errMsg,
		})
		return
	}

	// Stream events to client in a goroutine
	go func() {
		defer func() {
			cancel()
			mu.Lock()
			*cancelFuncPtr = nil
			mu.Unlock()

			// Send streaming done status
			_ = ws.writeJSON(wsServerMessage{
				Type:      "status",
				SessionID: sessionID,
				Streaming: false,
			})
		}()

		for event := range stream {
			serverMsg := wsServerMessage{
				Type:      "event",
				SessionID: sessionID,
				Event:     event,
			}
			if err := ws.writeJSON(serverMsg); err != nil {
				slog.Debug("websocket write failed (client disconnected?)", "error", err)
				return
			}
		}
	}()
}

// handleWSCancel cancels an in-progress prompt.
func (s *Server) handleWSCancel(mu *sync.Mutex, cancelFuncPtr *context.CancelFunc, sessionID string) {
	mu.Lock()
	defer mu.Unlock()
	if *cancelFuncPtr != nil {
		(*cancelFuncPtr)()
		*cancelFuncPtr = nil
		slog.Info("cancelled prompt via websocket", "session", sessionID)
	}
}

// handleWSSwitchModel changes the model for a session.
func (s *Server) handleWSSwitchModel(ws *wsConn, msg wsClientMessage) {
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
