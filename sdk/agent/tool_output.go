package agent

// ────────────────────────────────────────────────────────────────────────────
//  Full Output Retrieval
//
//  ToolOutputStore holds the full text of tool outputs that were replaced by
//  a synopsis. The full_output tool lets the model fetch a stored output by
//  ref, giving the synopsis's "you can read the full text" guidance a real
//  retrieval path. Only the most recent maxEntries outputs are kept so long
//  sessions do not grow without bound.
// ────────────────────────────────────────────────────────────────────────────

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ToolOutputStore 保存被概览化的大输出全文，供 full_output 工具按 ref 取回。
// 只保留最近 maxEntries 条，超出后淘汰最旧，防止长会话内存无界增长。
type ToolOutputStore struct {
	mu      sync.Mutex
	next    int
	max     int
	order   []string          // FIFO，记录淘汰顺序
	entries map[string]string // ref -> 全文
}

// NewToolOutputStore 创建存储；maxEntries <= 0 时用 defaultStoreEntries。
func NewToolOutputStore(maxEntries int) *ToolOutputStore {
	if maxEntries <= 0 {
		maxEntries = defaultStoreEntries
	}
	return &ToolOutputStore{max: maxEntries, entries: make(map[string]string, maxEntries)}
}

// Put 保存一份完整输出并返回它的引用。
func (s *ToolOutputStore) Put(content string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	ref := fmt.Sprintf("out_%d", s.next)
	s.entries[ref] = content
	s.order = append(s.order, ref)
	if len(s.order) > s.max {
		delete(s.entries, s.order[0])
		s.order = s.order[1:]
	}
	return ref
}

// Get 按 ref 取回完整输出；引用不存在或已被淘汰时返回 false。
func (s *ToolOutputStore) Get(ref string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.entries[ref]
	return content, ok
}

// FullOutputTool 按 ref 取回此前被概览化的工具输出全文。
type FullOutputTool struct {
	store *ToolOutputStore
}

// NewFullOutputTool 创建绑定 store 的 full_output 工具。
func NewFullOutputTool(store *ToolOutputStore) *FullOutputTool {
	return &FullOutputTool{store: store}
}

func (t *FullOutputTool) Name() string { return "full_output" }

func (t *FullOutputTool) Description() string {
	return "Fetch the full text of a large tool output that was replaced by an overview (输出概览). " +
		"Use when the overview is not enough to continue; the ref is given at the end of the overview."
}

func (t *FullOutputTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref": map[string]any{"type": "string", "description": "Output ref from the overview, e.g. out_3."},
		},
		"required": []string{"ref"},
	}
}

func (t *FullOutputTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Ref) == "" {
		return nil, fmt.Errorf("ref is required")
	}
	return raw, nil
}

// IsConcurrencySafe 报告 full_output 只读、可并行执行。
func (t *FullOutputTool) IsConcurrencySafe(json.RawMessage) bool { return true }

func (t *FullOutputTool) Execute(_ context.Context, raw json.RawMessage, _ func(PartialResult)) (ToolResult, error) {
	var params struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return ToolResult{IsError: true}, err
	}
	content, ok := t.store.Get(params.Ref)
	if !ok {
		return ToolResult{
			IsError: true,
			Content: fmt.Sprintf("ref %q 不存在或已被淘汰（只保留最近 %d 条输出）。请使用概览结尾给出的当前引用。", params.Ref, t.maxEntries()),
		}, nil
	}
	return ToolResult{Content: content}, nil
}

func (t *FullOutputTool) maxEntries() int {
	if t.store == nil {
		return defaultStoreEntries
	}
	return t.store.max
}
