package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
)

func TestOpenAIStreamEOFWithoutTerminalFails(t *testing.T) {
	for _, tools := range []bool{false, true} {
		name := "text"
		frame := textChunk("partial answer")
		if tools {
			name, frame = "tool", tcChunk(0, "call", "echo", `{"message":"hello"}`)
		}
		t.Run(name, func(t *testing.T) {
			srv := sseServer(t, frame, usageChunk())
			events, msg, err := collectStream(t, NewOpenAIProvider("test", srv.URL))
			require.Error(t, err)
			require.Contains(t, err.Error(), "terminal")
			require.Equal(t, ai.StopReasonError, msg.StopReason)
			require.Equal(t, 10, msg.Usage.InputTokens)
			for _, event := range events {
				_, done := event.(ai.EventDone)
				require.False(t, done)
			}
			if tools {
				require.Len(t, msg.ToolCalls, 1, "provider result retains partial diagnostics")
			} else {
				require.Equal(t, "partial answer", msg.Text)
			}
		})
	}
}

func TestAnthropicErrorEventClosesBeforeEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: {\"error\":{\"message\":\"test provider rejected request\"}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := NewAnthropicProvider("test", srv.URL).Stream(ctx, ai.StreamRequest{Model: ai.Model{ID: "test"}})
	require.NoError(t, err)
	errorsSeen := 0
	for event := range stream.Events() {
		if _, ok := event.(ai.EventError); ok {
			errorsSeen++
		}
	}
	_, err = stream.Result()
	require.Error(t, err)
	require.Contains(t, err.Error(), "test provider rejected request")
	require.Equal(t, 1, errorsSeen)
	require.NoError(t, ctx.Err(), "terminal provider errors must close without waiting for HTTP EOF")
}

func TestAnthropicStreamEOFWithoutMessageStopFails(t *testing.T) {
	for _, delta := range []bool{false, true} {
		name := "without-stop-reason"
		lines := []string{
			`event: message_start`,
			`data: {"message":{"usage":{"input_tokens":10}}}`,
			`event: content_block_start`,
			`data: {"index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`,
			`data: {"index":0,"delta":{"type":"text_delta","text":"partial answer"}}`,
		}
		if delta {
			name = "after-message-delta"
			lines = append(lines, `event: message_delta`, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
		}
		t.Run(name, func(t *testing.T) {
			srv := anthropicSSEServer(t, lines...)
			provider := NewAnthropicProvider("test", srv.URL)
			stream, err := provider.Stream(context.Background(), ai.StreamRequest{Model: ai.Model{ID: "test"}})
			require.NoError(t, err)
			for event := range stream.Events() {
				_, done := event.(ai.EventDone)
				require.False(t, done)
			}
			msg, err := stream.Result()
			require.Error(t, err)
			require.Contains(t, err.Error(), "message_stop")
			require.Equal(t, ai.StopReasonError, msg.StopReason)
			require.Equal(t, "partial answer", msg.Text)
			require.Equal(t, 10, msg.Usage.InputTokens)
		})
	}
}
