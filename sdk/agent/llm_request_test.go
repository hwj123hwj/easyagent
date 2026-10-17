package agent

import (
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// MaxTokens 为 nil 时 provider 兜底 4096，会截断长回复（见 issue #168）：
// 模型定义了输出上限就必须随请求下发，未定义时保持 nil 走 provider 兜底。
func TestLLMRequestCarriesModelMaxTokens(t *testing.T) {
	a := &Agent{model: ai.Model{ID: "glm-5.3-flash", Provider: "openai", MaxTokens: 32768}}
	req := a.llmRequest(nil)
	if req.MaxTokens == nil {
		t.Fatal("llmRequest().MaxTokens = nil, want 32768")
	}
	if *req.MaxTokens != 32768 {
		t.Errorf("llmRequest().MaxTokens = %d, want 32768", *req.MaxTokens)
	}
}

func TestLLMRequestNilMaxTokensWhenModelUnset(t *testing.T) {
	a := &Agent{model: ai.Model{ID: "custom-model", Provider: "openai"}}
	req := a.llmRequest(nil)
	if req.MaxTokens != nil {
		t.Errorf("llmRequest().MaxTokens = %d, want nil (provider fallback)", *req.MaxTokens)
	}
}
