package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func terminalSize(cols, rows uint16) (uint16, uint16) {
	if cols < 2 || cols > 500 {
		cols = 80
	}
	if rows < 2 || rows > 200 {
		rows = 24
	}
	return cols, rows
}

// A manual shell is opt-in and always authenticated, independent of Agent tools.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if s.apiKey == "" || !s.wsAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "terminal requires API authentication")
		return
	}
	if !s.terminalEnabled {
		writeError(w, http.StatusForbidden, "interactive terminal is not enabled on this host")
		return
	}
	if !r.URL.Query().Has("session_id") {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	cwd, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	select {
	case s.terminalSlots <- struct{}{}:
		defer func() { <-s.terminalSlots }()
	default:
		writeError(w, http.StatusTooManyRequests, "too many terminal sessions")
		return
	}
	upgrader := s.newUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	cols, _ := strconv.ParseUint(r.URL.Query().Get("cols"), 10, 16)
	rows, _ := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 16)
	c, h := terminalSize(uint16(cols), uint16(rows))
	terminal, err := startTerminal(cwd, c, h)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}
	defer terminal.close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-s.ctx.Done():
			_ = conn.Close()
		}
	}()
	// Limit unconsumed output to four chunks; acknowledgements come after rendering.
	credits := make(chan struct{}, 4)
	for i := 0; i < cap(credits); i++ {
		credits <- struct{}{}
	}
	go func() {
		defer conn.Close()
		buffer := make([]byte, 16*1024)
		for {
			select {
			case <-done:
				return
			case <-s.ctx.Done():
				return
			case <-credits:
			}
			n, err := terminal.read(buffer)
			if n > 0 {
				_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
				if conn.WriteMessage(websocket.BinaryMessage, buffer[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	conn.SetReadLimit(64 * 1024)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message terminalMessage
		if json.Unmarshal(data, &message) != nil {
			return
		}
		switch message.Type {
		case "input":
			if terminal.write([]byte(message.Data)) != nil {
				return
			}
		case "resize":
			cols, rows := terminalSize(message.Cols, message.Rows)
			if terminal.resize(cols, rows) != nil {
				return
			}
		case "ack":
			select {
			case credits <- struct{}{}:
			default:
			}
		default:
			return
		}
	}
}
