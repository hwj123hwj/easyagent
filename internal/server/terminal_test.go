//go:build !windows

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestTerminalAccess(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, token, session, origin string
		status                                int
	}{
		{"unauthenticated", "1", "", "valid", "", 401},
		{"disabled", "", workspaceSessionTestToken, "valid", "", 403},
		{"missing session", "1", workspaceSessionTestToken, "", "", 400},
		{"invalid session", "1", workspaceSessionTestToken, "../escape", "", 404},
		{"foreign origin", "1", workspaceSessionTestToken, "valid", "https://evil.example", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EA_ENABLE_TERMINAL", tc.enabled)
			f := newWorkspaceSessionFixture(t)
			server := httptest.NewServer(f.handler)
			defer server.Close()
			query := url.Values{}
			if tc.session != "" {
				if tc.session == "valid" {
					query.Set("session_id", f.id)
				} else {
					query.Set("session_id", tc.session)
				}
			}
			headers := http.Header{}
			if tc.token != "" {
				headers.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.origin != "" {
				headers.Set("Origin", tc.origin)
			}
			conn, response, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/terminal?"+query.Encode(), headers)
			if conn != nil {
				conn.Close()
			}
			require.Error(t, err)
			require.NotNil(t, response)
			response.Body.Close()
			require.Equal(t, tc.status, response.StatusCode)
		})
	}
	// Even explicit open-access mode cannot expose a manual shell without a key.
	t.Setenv("EA_ENABLE_TERMINAL", "1")
	t.Setenv("EA_ALLOW_NO_AUTH", "1")
	srv := New(newTestApp(t), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq("GET", "/terminal", nil))
	require.Equal(t, 401, w.Code)
}

func TestTerminalInteractiveDirectoryResizeInterruptAndCleanup(t *testing.T) {
	t.Setenv("EA_ENABLE_TERMINAL", "1")
	t.Setenv("SHELL", "/bin/sh")
	f := newWorkspaceSessionFixture(t)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	query := url.Values{"session_id": {f.id}, "cols": {"93"}, "rows": {"17"}}
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/terminal?"+query.Encode(), http.Header{"Authorization": {"Bearer " + workspaceSessionTestToken}})
	require.NoError(t, err)
	defer conn.Close()
	send := func(data string) { require.NoError(t, conn.WriteJSON(terminalMessage{Type: "input", Data: data})) }
	until := func(marker string) string {
		var output strings.Builder
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		for !strings.Contains(output.String(), marker) {
			kind, data, err := conn.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, websocket.BinaryMessage, kind, string(data))
			output.Write(data)
			require.NoError(t, conn.WriteJSON(terminalMessage{Type: "ack"}))
		}
		return output.String()
	}
	// Split marker arguments so an echoed input line cannot satisfy the assertion.
	send("printf 'DIRECTORY:%s\\n' \"$PWD\"; stty size; printf 'SHELLPID:%s\\n' \"$$\"; printf '%s%s\\n' READY _DONE\r")
	output := until("READY_DONE")
	require.Contains(t, output, "DIRECTORY:"+f.project)
	require.Contains(t, output, "17 93")
	match := regexp.MustCompile(`SHELLPID:(\d+)`).FindStringSubmatch(output)
	require.Len(t, match, 2)
	pid, err := strconv.Atoi(match[1])
	require.NoError(t, err)
	require.NoError(t, conn.WriteJSON(terminalMessage{Type: "resize", Cols: 101, Rows: 31}))
	send("stty size; printf '%s%s\\n' RESIZE _DONE\r")
	require.Contains(t, until("RESIZE_DONE"), "31 101")
	send("PS1='EA_TEST_PROMPT> '; sh -c \"printf '%s%s\\n' SLEEP _STARTED; exec sleep 60\"\r")
	until("SLEEP_STARTED")
	send("\x03")
	// Wait for the foreground job to exit: an interrupt flushes queued input.
	until("EA_TEST_PROMPT> ")
	send("printf '%s%s\\n' INTERRUPT _DONE\r")
	until("INTERRUPT_DONE")
	// PTY output is paced by render acknowledgements rather than dropped.
	send("yes output | head -c 100000; printf '%s%s\\n' BULK _DONE\r")
	require.Greater(t, len(until("BULK_DONE")), 100000)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 3*time.Second, 20*time.Millisecond, "shell must exit on disconnect")
}
