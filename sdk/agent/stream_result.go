package agent

import (
	"context"
	"errors"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

// 流事件读到关闭后再取 Result：保留 provider 的部分输出和原始错误，
// 也避免错误事件后的提前返回把 producer 卡在有界 channel 上。
func streamConsumptionResult(ctx context.Context, stream *ai.EventStream, fallback ai.StreamAssistantMessage, eventErr error) (ai.StreamAssistantMessage, error) {
	result, err := stream.Result()
	if result.StopReason == "" && fallback.StopReason != "" {
		result = fallback
	}
	if err == nil {
		err = eventErr
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && result.StopReason == ai.StopReasonError {
		message := result.ErrorMsg
		if message == "" {
			message = "provider stream failed"
		}
		err = errors.New(message)
	}
	if err == nil && result.StopReason == ai.StopReasonAborted {
		err = context.Canceled
	}
	return result, err
}
