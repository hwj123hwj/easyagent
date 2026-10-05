package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// AnthropicProvider 通过 HTTP 直接调用 Anthropic Messages API。
// 不依赖 anthropic-sdk-go，减少外部依赖。
type AnthropicProvider struct {
	apiKey  string
	baseURL string // 例如 https://api.anthropic.com 或 https://api.longcat.chat/anthropic
	client  *http.Client
}

func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	// Strip ANSI escape codes that may leak from terminal input corruption.
	baseURL = sanitizeURL(baseURL)
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  newStreamHTTPClient(),
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) StreamSimple(ctx context.Context, req ai.SimpleStreamRequest) (*ai.EventStream, error) {
	return p.Stream(ctx, ai.StreamRequest{
		Model:     req.Model,
		Messages:  req.Messages,
		System:    req.System,
		Tools:     req.Tools,
		MaxTokens: req.MaxTokens,
	})
}

// ─── Anthropic API 请求/响应结构 ──────────────────────────────────────────────────

type anthropicImageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicContentBlock struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	Source    *anthropicImageSource `json:"source,omitempty"`      // image content source
	ID        string                `json:"id,omitempty"`          // tool_use 的 id
	ToolUseID string                `json:"tool_use_id,omitempty"` // tool_result 引用的 tool_use id
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	Content   any                   `json:"content,omitempty"`  // tool_result 的嵌套内容
	IsError   bool                  `json:"is_error,omitempty"` // tool_result 是否为错误
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string                `json:"model"`
	Messages  []anthropicMessage    `json:"messages"`
	System    string                `json:"system,omitempty"`
	MaxTokens int                   `json:"max_tokens"`
	Stream    bool                  `json:"stream"`
	Tools     []anthropicToolSchema `json:"tools,omitempty"`
}

// ─── Stream ───────────────────────────────────────────────────────────────────────

func (p *AnthropicProvider) Stream(ctx context.Context, req ai.StreamRequest) (*ai.EventStream, error) {
	stream := ai.NewEventStream(8)

	anthropicReq := p.buildRequest(req, true)

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 流式请求绑定可取消的派生 context：空闲看门狗超时触发 cancel，
	// 中止停滞的上游连接（总时长不受 http.Client.Timeout 限制）
	streamCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer stream.Close()
		defer cancel()

		partial := ai.StreamAssistantMessage{}
		_ = stream.Push(ctx, ai.EventStart{Partial: partial})

		fail := func(err error) {
			partial.StopReason = ai.StopReasonError
			partial.ErrorMsg = err.Error()
			// ctx 可能已被看门狗取消，终止事件用独立 context 保证送达
			_ = stream.Push(context.Background(), ai.EventError{Reason: "error", Error: err.Error()})
			stream.SetResult(partial, err)
		}

		// 断流重试：上游长思考静默期会被前置 LB 掐断，客户端读到 unexpected EOF。
		// 只要一个内容块都没收到，原样重发是安全的（内容尚未开始）。
		for attempt := 1; ; attempt++ {
			httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
				p.baseURL+"v1/messages", bytes.NewReader(body))
			if err != nil {
				fail(fmt.Errorf("create request: %w", err))
				return
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
			httpReq.Header.Set("anthropic-version", "2023-06-01")

			resp, err := p.client.Do(httpReq)
			if err != nil {
				fail(err)
				return
			}
			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				fail(fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody)))
				return
			}

			wd := startStreamWatchdog(streamCtx, streamIdleTimeout, cancel)
			received, err := p.handleSSE(streamCtx, stream, resp.Body, &partial, wd)
			resp.Body.Close()

			if err == nil && received {
				return // 正常完成，handleSSE 已收尾
			}
			// 空流（EOF 断流）且未被取消 → 退避后重试
			if !received && err == nil && attempt < streamRetryAttempts &&
				ctx.Err() == nil && streamCtx.Err() == nil {
				select {
				case <-time.After(time.Duration(attempt) * streamRetryDelay):
					continue
				case <-ctx.Done():
				}
			}
			if err == nil {
				err = errors.New("upstream returned an empty stream")
			}
			fail(err)
			return
		}
	}()

	return stream, nil
}

// ─── SSE 解析 ────────────────────────────────────────────────────────────────────

func (p *AnthropicProvider) handleSSE(ctx context.Context, stream *ai.EventStream, body io.Reader, partial *ai.StreamAssistantMessage, wd *streamWatchdog) (received bool, err error) {
	scanner := bufio.NewScanner(body)
	// 超长单行真实存在（长思考/长参数），初始小、按需增长到大上限
	scanner.Buffer(make([]byte, 64*1024), 16<<20)

	var currentEvent string
	// 追踪每个 content block index → 类型 ("text" | "tool_use" | "thinking")
	blockTypes := map[int]string{}
	// 追踪每个 tool_use content block index → ToolCall 在 partial.ToolCalls 中的位置
	toolCallIndexMap := map[int]int{}
	toolCallCounter := 0

	for scanner.Scan() {
		line := scanner.Text()
		wd.Kick()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			p.handleSSEEvent(ctx, stream, partial, currentEvent, data, blockTypes, toolCallIndexMap, &toolCallCounter)
			currentEvent = ""
		}
	}

	// 是否收到过任何内容块（文本 / 工具调用 / 思考）
	received = len(partial.ToolCalls) > 0 || partial.Text != "" || partial.Thinking != ""

	// 兜底：流结束时未收到 message_stop。异常结束（含空闲超时）必须报错，
	// 不能把已收到的部分内容伪装成正常完成。
	// 终止事件与结果由调用方统一处理（可能先重试）。
	if err := scanner.Err(); err != nil {
		if wd.Tripped() {
			err = idleTimeoutError(streamIdleTimeout)
		}
		return received, err
	}
	if partial.StopReason == "" {
		// 空流：上游可能被 LB 掐断（连 message_stop 都没发），
		// 不装作正常完成（空消息会污染会话），交给调用方重试/报错
		if !received {
			return false, nil
		}
		partial.StopReason = ai.StopReasonStop
		_ = stream.Push(ctx, ai.EventDone{Reason: partial.StopReason, Message: *partial})
		stream.SetResult(*partial, nil)
	}
	return received, nil
}

func (p *AnthropicProvider) handleSSEEvent(
	ctx context.Context,
	stream *ai.EventStream,
	partial *ai.StreamAssistantMessage,
	eventType, data string,
	blockTypes map[int]string,
	toolCallIndexMap map[int]int,
	toolCallCounter *int,
) {
	switch eventType {
	case "ping":
		return

	case "message_start":
		var msg struct {
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
				Usage struct {
					InputTokens int  `json:"input_tokens"`
					CacheRead   *int `json:"cache_read_input_tokens"`
					CacheWrite  int  `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		_ = json.Unmarshal([]byte(data), &msg)
		if msg.Message.Usage.InputTokens > 0 || msg.Message.Usage.CacheRead != nil || msg.Message.Usage.CacheWrite > 0 {
			u := msg.Message.Usage
			partial.Usage = ai.Usage{InputTokens: u.InputTokens + u.CacheWrite, CachedInputTokens: u.CacheRead}
			if u.CacheRead != nil {
				partial.Usage.InputTokens += *u.CacheRead
			}
		}

	case "content_block_start":
		var raw struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type     string `json:"type"`
				ID       string `json:"id,omitempty"`
				Name     string `json:"name,omitempty"`
				Text     string `json:"text,omitempty"`
				Thinking string `json:"thinking,omitempty"`
				Input    any    `json:"input,omitempty"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			return
		}
		idx := raw.Index
		blockTypes[idx] = raw.ContentBlock.Type

		switch raw.ContentBlock.Type {
		case "text":
			_ = stream.Push(ctx, ai.EventTextStart{ContentIndex: idx, Partial: *partial})
		case "tool_use":
			tc := ai.ToolCall{ID: raw.ContentBlock.ID, Name: raw.ContentBlock.Name}
			partial.ToolCalls = append(partial.ToolCalls, tc)
			toolCallIndexMap[idx] = *toolCallCounter
			*toolCallCounter++
			_ = stream.Push(ctx, ai.EventToolCallStart{ContentIndex: idx, Partial: *partial})
		case "thinking":
			if raw.ContentBlock.Thinking != "" {
				partial.Thinking += raw.ContentBlock.Thinking
				_ = stream.Push(ctx, ai.EventThinkingDelta{Delta: raw.ContentBlock.Thinking})
			}
		}

	case "content_block_delta":
		var raw struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				Thinking    string `json:"thinking,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			return
		}
		idx := raw.Index

		switch raw.Delta.Type {
		case "text_delta":
			partial.Text += raw.Delta.Text
			_ = stream.Push(ctx, ai.EventTextDelta{ContentIndex: idx, Delta: raw.Delta.Text, Partial: *partial})
		case "thinking_delta":
			partial.Thinking += raw.Delta.Thinking
			_ = stream.Push(ctx, ai.EventThinkingDelta{Delta: raw.Delta.Thinking})
		case "input_json_delta":
			if tcIdx, ok := toolCallIndexMap[idx]; ok && tcIdx < len(partial.ToolCalls) {
				partial.ToolCalls[tcIdx].Args += raw.Delta.PartialJSON
			}
			_ = stream.Push(ctx, ai.EventToolCallDelta{ContentIndex: idx, Delta: raw.Delta.PartialJSON, Partial: *partial})
		}

	case "content_block_stop":
		var raw struct {
			Index int `json:"index"`
		}
		_ = json.Unmarshal([]byte(data), &raw)
		idx := raw.Index
		blockType := blockTypes[idx]

		switch blockType {
		case "tool_use":
			if tcIdx, ok := toolCallIndexMap[idx]; ok && tcIdx < len(partial.ToolCalls) {
				_ = stream.Push(ctx, ai.EventToolCallEnd{
					ContentIndex: idx,
					ToolCall:     partial.ToolCalls[tcIdx],
					Partial:      *partial,
				})
			}
		case "text":
			_ = stream.Push(ctx, ai.EventTextEnd{
				ContentIndex: idx,
				Text:         partial.Text,
				Partial:      *partial,
			})
		default:
			// thinking 或其他类型，不推送事件
		}

	case "message_delta":
		var raw struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			return
		}
		switch raw.Delta.StopReason {
		case "end_turn", "stop":
			partial.StopReason = ai.StopReasonStop
		case "tool_use":
			partial.StopReason = ai.StopReasonToolUse
		case "max_tokens":
			partial.StopReason = ai.StopReasonLength
		}
		if raw.Usage.OutputTokens > 0 {
			partial.Usage.OutputTokens = raw.Usage.OutputTokens
		}

	case "message_stop":
		_ = stream.Push(ctx, ai.EventDone{Reason: partial.StopReason, Message: *partial})
		stream.SetResult(*partial, nil)

	case "error":
		var errResp struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(data), &errResp)
		errMsg := errResp.Error.Message
		if errMsg == "" {
			errMsg = data
		}
		partial.StopReason = ai.StopReasonError
		partial.ErrorMsg = errMsg
		_ = stream.Push(ctx, ai.EventError{Reason: "error", Error: errMsg})
		stream.SetResult(*partial, fmt.Errorf("API error: %s", errMsg))
	}
}

// ─── 消息转换 ────────────────────────────────────────────────────────────────────

func (p *AnthropicProvider) buildRequest(req ai.StreamRequest, stream bool) anthropicRequest {
	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	apiReq := anthropicRequest{
		Model:     req.Model.ID,
		MaxTokens: maxTokens,
		Stream:    stream,
		System:    req.System,
		Messages:  convertToAnthropicMessages(req.Messages),
	}

	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			params, _ := json.Marshal(t.Parameters)
			apiReq.Tools = append(apiReq.Tools, anthropicToolSchema{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: params,
			})
		}
	}

	return apiReq
}

func convertToAnthropicMessages(msgs []ai.Message) []anthropicMessage {
	var result []anthropicMessage

	for _, msg := range msgs {
		switch m := msg.(type) {
		case ai.UserMessage:
			var blocks []anthropicContentBlock
			for _, block := range m.Content {
				switch block.Type {
				case "text":
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: block.Text})
				case "image":
					if block.Image != nil {
						blocks = append(blocks, anthropicContentBlock{
							Type: "image",
							Source: &anthropicImageSource{
								Type:      "base64",
								MediaType: block.Image.MediaType,
								Data:      base64.StdEncoding.EncodeToString(block.Image.Data),
							},
						})
					} else if block.Image != nil && block.Image.URL != "" {
						// URL-based image (Anthropic supports this directly)
						blocks = append(blocks, anthropicContentBlock{
							Type: "image",
							Source: &anthropicImageSource{
								Type: "url",
								Data: block.Image.URL,
							},
						})
					}
				}
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: "..."})
			}
			result = append(result, anthropicMessage{Role: "user", Content: blocks})

		case ai.AssistantMessage:
			var blocks []anthropicContentBlock
			if m.Text != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				var input json.RawMessage
				if err := json.Unmarshal([]byte(tc.Args), &input); err != nil {
					input = json.RawMessage(tc.Args)
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: "..."})
			}
			result = append(result, anthropicMessage{Role: "assistant", Content: blocks})

		case ai.ToolResultMessage:
			block := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   []anthropicContentBlock{{Type: "text", Text: m.Content}},
				IsError:   m.IsError,
			}
			// Merge consecutive tool results into a single user message
			if len(result) > 0 && result[len(result)-1].Role == "user" {
				result[len(result)-1].Content = append(result[len(result)-1].Content, block)
			} else {
				result = append(result, anthropicMessage{
					Role:    "user",
					Content: []anthropicContentBlock{block},
				})
			}
		}
	}

	return result
}
