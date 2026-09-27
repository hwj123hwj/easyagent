package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anthropicSSEServer 按给定事件/data 行输出 Anthropic 格式 SSE。
func anthropicSSEServer(t *testing.T, lines ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			fmt.Fprintf(w, "%s\n\n", l)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func anthropicCollect(t *testing.T, p *AnthropicProvider) (ai.StreamAssistantMessage, error) {
	t.Helper()
	stream, err := p.Stream(context.Background(), ai.StreamRequest{Model: ai.Model{ID: "test-model"}})
	require.NoError(t, err)
	for range stream.Events() {
	}
	msg, err := stream.Result()
	return msg, err
}

// TestAnthropicStreamThinkingAccumulation：Anthropic thinking 块累积进
// partial.Thinking（openai 侧的 reasoning_content 处理与此对齐）。
func TestAnthropicStreamThinkingAccumulation(t *testing.T) {
	srv := anthropicSSEServer(t,
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10}}}`,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先想想。"}}`,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答案是 42。"}}`,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
	)
	p := NewAnthropicProvider("test-key", srv.URL)

	msg, err := anthropicCollect(t, p)

	require.NoError(t, err)
	assert.Equal(t, "先想想。", msg.Thinking)
	assert.Equal(t, "答案是 42。", msg.Text)
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
}

// TestAnthropicStreamIdleTimeout：上游停滞超空闲阈值时报错，不伪装正常完成。
func TestAnthropicStreamIdleTimeout(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	p := NewAnthropicProvider("test-key", srv.URL)

	msg, err := anthropicCollect(t, p)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "idle")
	assert.Equal(t, ai.StopReasonError, msg.StopReason)
}

// ─── 回归：断流重试（2026-09-27 踩坑，openai 侧同修）────────────────────────────
// 上游长思考静默期被前置 LB 掐断 → 客户端读到 unexpected EOF（无 message_stop）。

// TestAnthropicStreamRetriesEmptyThenSucceeds：首请求空流断开，重试后正常完成。
func TestAnthropicStreamRetriesEmptyThenSucceeds(t *testing.T) {
	shortRetry(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if atomic.AddInt32(&hits, 1) == 1 {
			return // 空流直接断开
		}
		for _, l := range []string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10}}}`,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"重试成功"}}`,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
		} {
			fmt.Fprintf(w, "%s\n\n", l)
		}
	}))
	t.Cleanup(srv.Close)
	p := NewAnthropicProvider("test-key", srv.URL)

	msg, err := anthropicCollect(t, p)

	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits))
	assert.Equal(t, "重试成功", msg.Text)
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
}

// TestAnthropicStreamEmptyExhaustsRetries：连续空流，重试耗尽后必须报错。
func TestAnthropicStreamEmptyExhaustsRetries(t *testing.T) {
	shortRetry(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		atomic.AddInt32(&hits, 1)
	}))
	t.Cleanup(srv.Close)
	p := NewAnthropicProvider("test-key", srv.URL)

	msg, err := anthropicCollect(t, p)

	require.Error(t, err)
	assert.Equal(t, int32(streamRetryAttempts), atomic.LoadInt32(&hits))
	assert.Equal(t, ai.StopReasonError, msg.StopReason)
}
