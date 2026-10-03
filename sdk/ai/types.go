package ai

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message interface {
	Role() Role
	messageMarker()
}

type TextBlock struct {
	Text string `json:"text"`
}

type ImageBlock struct {
	MediaType string `json:"media_type,omitempty"`
	Data      []byte `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type ContentBlock struct {
	Type  string      `json:"type"` // "text" | "image"
	Text  string      `json:"text,omitempty"`
	Image *ImageBlock `json:"image,omitempty"`
}

type UserMessage struct {
	Content []ContentBlock `json:"content"`
	// DisplayText is an optional concise history representation. Providers receive Content.
	DisplayText string `json:"display_text,omitempty"`
}

func (UserMessage) Role() Role     { return RoleUser }
func (UserMessage) messageMarker() {}

// NewTextUserMessage 快捷创建纯文本 user message
func NewTextUserMessage(text string) UserMessage {
	return UserMessage{Content: []ContentBlock{{Type: "text", Text: text}}}
}

type AssistantMessage struct {
	Text       string     `json:"text,omitempty"`
	Thinking   string     `json:"thinking,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	ErrorMsg   string     `json:"error_msg,omitempty"`
	Usage      Usage      `json:"usage,omitempty"`
}

func (AssistantMessage) Role() Role     { return RoleAssistant }
func (AssistantMessage) messageMarker() {}

type ToolResultMessage struct {
	DurationMS int64  `json:"duration_ms,omitempty"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	Details    any    `json:"details,omitempty"` // structured附加数据（如 PlayDetails），可选
}

func (ToolResultMessage) Role() Role     { return RoleTool }
func (ToolResultMessage) messageMarker() {}

type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

type ToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type Cost struct {
	InputPerMega      float64 `json:"input_per_mega"`
	OutputPerMega     float64 `json:"output_per_mega"`
	CacheReadPerMega  float64 `json:"cache_read_per_mega"`
	CacheWritePerMega float64 `json:"cache_write_per_mega"`
}

type Model struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	API           string `json:"api"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	ContextWindow int    `json:"context_window"`
	MaxTokens     int    `json:"max_tokens"`
	Cost          Cost   `json:"cost"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type StopReason string

const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "tool_use"
	StopReasonError   StopReason = "error"
	StopReasonAborted StopReason = "aborted"
)

type StreamAssistantMessage struct {
	Text       string     `json:"text,omitempty"`
	Thinking   string     `json:"thinking,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	ErrorMsg   string     `json:"error_msg,omitempty"`
	Usage      Usage      `json:"usage,omitempty"`
}

type StreamRequest struct {
	Model       Model
	Messages    []Message
	System      string
	Tools       []ToolDefinition
	MaxTokens   *int
	Temperature *float64
	ToolChoice  *ToolChoice
}

type SimpleStreamRequest struct {
	Model       Model
	Messages    []Message
	System      string
	Tools       []ToolDefinition
	MaxTokens   *int
	Temperature *float64
}
