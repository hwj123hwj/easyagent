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
	"sort"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// OpenAIProvider 通过 HTTP 调用 OpenAI Chat Completions API。
// 兼容所有 OpenAI 格式的中转服务（如 https://api.longcat.chat/openai）。
type OpenAIProvider struct {
	apiKey  string
	baseURL string // 例如 https://api.openai.com/v1 或 https://api.longcat.chat/openai/v1
	client  *http.Client
}

func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	// CRITICAL: Strip any control characters (ANSI escape codes like \x1b[A)
	// that may have leaked into the baseURL from terminal input corruption.
	// Without this, the URL becomes invalid and causes:
	//   "parse \x1b[A\x1b[/v1/chat/completions": net/url: invalid control character in URL"
	baseURL = sanitizeURL(baseURL)
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  newStreamHTTPClient(),
	}
}

// sanitizeURL removes all non-printable control characters from a URL string.
func sanitizeURL(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7F {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) StreamSimple(ctx context.Context, req ai.SimpleStreamRequest) (*ai.EventStream, error) {
	return p.Stream(ctx, ai.StreamRequest{
		Model:     req.Model,
		Messages:  req.Messages,
		System:    req.System,
		Tools:     req.Tools,
		MaxTokens: req.MaxTokens,
	})
}

// ─── OpenAI 请求/响应结构 ──────────────────────────────────────────────────────────

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolParam struct {
	Type     string          `json:"type"`
	Function json.RawMessage `json:"function"`
}

type openAIRequest struct {
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Model       string            `json:"model"`
	Messages    []openAIMessage   `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
	Tools       []openAIToolParam `json:"tools,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// 推理内容：deepseek 系用 reasoning_content，OpenRouter 用 reasoning
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
	Reasoning        string                 `json:"reasoning,omitempty"`
	ToolCalls        []openAIStreamToolCall `json:"tool_calls,omitempty"`
}

type openAIStreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

type openAIStreamChunk struct {
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens *int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage,omitempty"`
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

// ─── Stream ───────────────────────────────────────────────────────────────────────

func (p *OpenAIProvider) Stream(ctx context.Context, req ai.StreamRequest) (*ai.EventStream, error) {
	stream := ai.NewEventStream(8)

	oaiReq := p.buildOpenAIRequest(req, true)
	body, err := json.Marshal(oaiReq)
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

		// 断流重试：上游（如 glm coding 端点）长思考静默期会被前置 LB 掐断，
		// 客户端读到 unexpected EOF，或收到无内容的 finish_reason。只要一个
		// delta 都没收到，原样重发是安全的（内容尚未开始）。
		for attempt := 1; ; attempt++ {
			httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
				p.baseURL+"v1/chat/completions", bytes.NewReader(body))
			if err != nil {
				fail(fmt.Errorf("create request: %w", err))
				return
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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
			// 空流（EOF 断流或伪造 finish_reason）且未被取消 → 退避后重试
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

func (p *OpenAIProvider) handleSSE(ctx context.Context, stream *ai.EventStream, body io.Reader, partial *ai.StreamAssistantMessage, wd *streamWatchdog) (received bool, err error) {
	scanner := bufio.NewScanner(body)
	// 超长单行真实存在（巨型工具参数、长推理文本），初始小、按需增长到大上限
	scanner.Buffer(make([]byte, 64*1024), 16<<20)

	// 用于增量拼装 tool_calls（key = index）
	type toolCallAccum struct {
		id        string
		name      string
		arguments strings.Builder
	}
	toolCalls := map[int]*toolCallAccum{}

	textAccum := strings.Builder{}
	textBlockStarted := false

	for scanner.Scan() {
		line := scanner.Text()
		wd.Kick()
		// 兼容 data: 不带空格 / 多空格的网关（对齐 new-api stream_scanner 的容错）
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "" {
			continue
		}
		if strings.HasPrefix(payload, "[DONE]") {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			partial.Usage = ai.Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens, CachedInputTokens: chunk.Usage.PromptTokensDetails.CachedTokens}
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// 同时持久化和实时转发上游明确返回的推理片段。
		if delta.ReasoningContent != "" {
			partial.Thinking += delta.ReasoningContent
			_ = stream.Push(ctx, ai.EventThinkingDelta{Delta: delta.ReasoningContent})
		}
		if delta.Reasoning != "" {
			partial.Thinking += delta.Reasoning
			_ = stream.Push(ctx, ai.EventThinkingDelta{Delta: delta.Reasoning})
		}

		// --- 文本内容 ---
		if delta.Content != "" {
			textAccum.WriteString(delta.Content)
			if !textBlockStarted {
				textBlockStarted = true
				_ = stream.Push(ctx, ai.EventTextStart{ContentIndex: 0, Partial: *partial})
			}
			_ = stream.Push(ctx, ai.EventTextDelta{ContentIndex: 0, Delta: delta.Content, Partial: *partial})
		}

		// --- 工具调用（增量拼装）---
		for _, tc := range delta.ToolCalls {
			idx := tc.Index
			if _, ok := toolCalls[idx]; !ok {
				toolCalls[idx] = &toolCallAccum{}
			}
			acc := toolCalls[idx]
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.arguments.WriteString(tc.Function.Arguments)
		}

		// --- finish_reason ---
		if choice.FinishReason != nil {
			switch *choice.FinishReason {
			case "stop":
				partial.StopReason = ai.StopReasonStop
			case "tool_calls":
				partial.StopReason = ai.StopReasonToolUse
			case "length":
				partial.StopReason = ai.StopReasonLength
			}
		}
	}

	// 关闭文本 block
	if textBlockStarted {
		partial.Text = textAccum.String()
		_ = stream.Push(ctx, ai.EventTextEnd{ContentIndex: 0, Text: partial.Text, Partial: *partial})
	}

	// 输出拼装完整的 tool_calls（按 index 顺序）
	// 收集并排序 index，兼容 0-based 和 1-based indexing
	indices := make([]int, 0, len(toolCalls))
	for idx := range toolCalls {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	// 上游 index 只用作累积 key，不透传：1-based 网关叠加文本 block 会留下
	// ContentIndex 空洞，输出序号在这里重新分配
	nextIdx := 0
	if textBlockStarted {
		nextIdx = 1
	}
	for _, i := range indices {
		acc := toolCalls[i]
		tc := ai.ToolCall{
			ID:   acc.id,
			Name: acc.name,
			Args: acc.arguments.String(),
		}
		partial.ToolCalls = append(partial.ToolCalls, tc)
		_ = stream.Push(ctx, ai.EventToolCallStart{ContentIndex: nextIdx, Partial: *partial})
		_ = stream.Push(ctx, ai.EventToolCallDelta{ContentIndex: nextIdx, Delta: tc.Args, Partial: *partial})
		_ = stream.Push(ctx, ai.EventToolCallEnd{ContentIndex: nextIdx, ToolCall: tc, Partial: *partial})
		nextIdx++
	}

	// 判断是否收到过任何内容（文本 / 工具调用 / 思考）
	received = textBlockStarted || len(toolCalls) > 0 || partial.Thinking != ""

	// 流异常结束（含空闲超时）：报错而非伪装成正常完成。
	// 终止事件与结果由调用方统一处理（可能先重试）。
	resultErr := scanner.Err()
	if resultErr != nil {
		if wd.Tripped() {
			resultErr = idleTimeoutError(streamIdleTimeout)
		}
		return received, resultErr
	}

	// 空流：上游可能被 LB 掐断后网关补了个假 finish_reason，
	// 不装作正常完成（空消息会污染会话），交给调用方重试/报错
	if !received {
		return false, nil
	}

	if partial.StopReason == "" {
		partial.StopReason = ai.StopReasonStop
	}
	_ = stream.Push(ctx, ai.EventDone{Reason: partial.StopReason, Message: *partial})
	stream.SetResult(*partial, nil)
	return true, nil
}

// ─── 消息转换 ────────────────────────────────────────────────────────────────────

func (p *OpenAIProvider) buildOpenAIRequest(req ai.StreamRequest, stream bool) openAIRequest {
	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	oaiReq := openAIRequest{
		Model:       req.Model.ID,
		MaxTokens:   maxTokens,
		Stream:      stream,
		Temperature: 0.7,
	}

	if stream {
		oaiReq.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{true}
	}

	// system prompt 作为第一条 system 消息
	if req.System != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// 转换 messages
	for _, msg := range req.Messages {
		switch m := msg.(type) {
		case ai.UserMessage:
			hasOnlyText := true
			for _, block := range m.Content {
				if block.Type != "text" {
					hasOnlyText = false
					break
				}
			}
			if hasOnlyText {
				var text string
				for _, block := range m.Content {
					text += block.Text
				}
				if text == "" {
					text = "..."
				}
				oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
					Role:    "user",
					Content: text,
				})
			} else {
				// Multi-modal: convert to OpenAI content parts
				parts := make([]map[string]any, 0, len(m.Content))
				for _, block := range m.Content {
					switch block.Type {
					case "text":
						parts = append(parts, map[string]any{
							"type": "text",
							"text": block.Text,
						})
					case "image":
						imgPart := map[string]any{"type": "image_url"}
						if block.Image != nil {
							if block.Image.URL != "" {
								imgPart["image_url"] = map[string]any{"url": block.Image.URL}
							} else if len(block.Image.Data) > 0 {
								mediaType := block.Image.MediaType
								if mediaType == "" {
									mediaType = "image/png"
								}
								b64 := base64.StdEncoding.EncodeToString(block.Image.Data)
								imgPart["image_url"] = map[string]any{"url": "data:" + mediaType + ";base64," + b64}
							}
						}
						parts = append(parts, imgPart)
					}
				}
				oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
					Role:    "user",
					Content: parts,
				})
			}

		case ai.AssistantMessage:
			msg := openAIMessage{Role: "assistant", Content: m.Text}
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: tc.Args,
					},
				})
			}
			oaiReq.Messages = append(oaiReq.Messages, msg)

		case ai.ToolResultMessage:
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			})
		}
	}

	// 转换 tools
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			fn := map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			}
			fnBytes, _ := json.Marshal(fn)
			oaiReq.Tools = append(oaiReq.Tools, openAIToolParam{
				Type:     "function",
				Function: fnBytes,
			})
		}
	}

	return oaiReq
}
