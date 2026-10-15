package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GatewayModel 定义网关返回的模型项及其元数据。
type GatewayModel struct {
	ID              string `json:"id"`
	MaxInputTokens  *int   `json:"max_input_tokens,omitempty"`
	MaxOutputTokens *int   `json:"max_output_tokens,omitempty"`
}

// FetchGatewayModels 从 OpenAI 兼容端点拉取模型列表（GET {baseURL}/models）。
// 适配 LiteLLM / easyagent 网关等任何返回 {"data":[{"id":"..."}]} 的端点。
// 网关不可达或响应异常时返回 error，调用方降级为本地清单，不阻塞启动。
func FetchGatewayModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	models, err := FetchGatewayModelDefs(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// FetchGatewayModelDefs 从 OpenAI 兼容端点拉取模型完整元数据列表。
func FetchGatewayModelDefs(ctx context.Context, baseURL, apiKey string) ([]GatewayModel, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	url := baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}

	var out struct {
		Data []GatewayModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := make([]GatewayModel, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			result = append(result, m)
		}
	}
	return result, nil
}

// resolveDefaultContextWindow 根据模型名称特征推断合理的上下文窗口大小。
func resolveDefaultContextWindow(id string) int {
	if strings.HasPrefix(id, "gemini-") {
		return 1000000
	}
	if strings.HasPrefix(id, "deepseek-") {
		return 160000
	}
	if strings.HasPrefix(id, "gpt-5") || strings.HasPrefix(id, "gpt-6") {
		return 272000
	}
	if strings.HasPrefix(id, "glm-") {
		return 128000
	}
	return 128000
}

// MergeGateway 把网关返回的模型合并进注册表：
// 已存在的 ID 保留本地定义（含上下文窗口等元数据）不动；
// 网关新增的 ID 注册其 ID 与推断窗口。
func (r *Registry) MergeGateway(provider string, ids []string) int {
	models := make([]GatewayModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, GatewayModel{ID: id})
	}
	return r.MergeGatewayModels(provider, models)
}

// MergeGatewayModels 把网关返回的完整模型信息合并进注册表。
func (r *Registry) MergeGatewayModels(provider string, models []GatewayModel) int {
	added := 0
	for _, m := range models {
		if _, exists := r.models[m.ID]; exists {
			continue
		}
		contextWindow := resolveDefaultContextWindow(m.ID)
		if m.MaxInputTokens != nil && *m.MaxInputTokens > 0 {
			contextWindow = *m.MaxInputTokens
		}
		maxTokens := 4096
		if m.MaxOutputTokens != nil && *m.MaxOutputTokens > 0 {
			maxTokens = *m.MaxOutputTokens
		}
		r.Register(ModelDef{
			ID:            m.ID,
			Provider:      provider,
			Name:          m.ID,
			ContextWindow: contextWindow,
			MaxTokens:     maxTokens,
		})
		added++
	}
	return added
}
