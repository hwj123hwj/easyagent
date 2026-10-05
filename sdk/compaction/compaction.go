package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// Settings 控制上下文压缩行为。
type Settings struct {
	Enabled          bool
	ReserveTokens    int // 默认 16384，给总结 prompt 和输出留的 token
	KeepRecentTokens int // 默认 20000，保留的最近上下文 token 数

	// MicroCompact（清旧 tool result，不调 LLM）
	MicroCompactRatio float64 // 默认 0.6，token 占比超此值触发 MicroCompact
	MicroKeepRecent   int     // 默认 5，保留最近 N 个 tool result 完整
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:           true,
		ReserveTokens:     16384,
		KeepRecentTokens:  20000,
		MicroCompactRatio: 0.6,
		MicroKeepRecent:   5,
	}
}

// ShouldCompact 判断是否需要全量压缩（调 LLM 摘要）。
func ShouldCompact(contextTokens int, contextWindow int, settings Settings) bool {
	if !settings.Enabled {
		return false
	}
	return contextTokens > contextWindow-settings.ReserveTokens
}

// ShouldMicroCompact 判断是否需要 MicroCompact（清旧 tool result，不调 LLM）。
// 阈值 = contextWindow * MicroCompactRatio（默认 60%），比全量压缩（约 90%）低，先触发。
func ShouldMicroCompact(contextTokens int, contextWindow int, settings Settings) bool {
	if !settings.Enabled || contextWindow <= 0 {
		return false
	}
	ratio := settings.MicroCompactRatio
	if ratio <= 0 {
		ratio = 0.6
	}
	return float64(contextTokens) > float64(contextWindow)*ratio
}

// SummarizeFunc 是调用 LLM 生成摘要的函数，由调用方注入（依赖倒置）。
// customInstructions: 可选的用户自定义摘要指令（/compact <instructions>）。
type SummarizeFunc func(ctx context.Context, history []ai.Message, recent []ai.Message, customInstructions string) (string, error)

// Compact 执行上下文压缩。
// history: 被压缩的历史消息
// recent: 保留的最近消息
// customInstructions: 可选的用户自定义摘要指令
// 返回摘要文本。
func Compact(ctx context.Context, history []ai.Message, recent []ai.Message, customInstructions string, summarize SummarizeFunc) (string, error) {
	if len(history) == 0 {
		return "", nil
	}
	summary, err := summarize(ctx, history, recent, customInstructions)
	if err != nil {
		return "", fmt.Errorf("compaction summarize failed: %w", err)
	}
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("compaction summarizer returned an empty summary")
	}
	return summary, nil
}
