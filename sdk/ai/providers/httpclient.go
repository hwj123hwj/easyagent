package providers

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// 流式 LLM 响应的总时长可以远超常规 HTTP 请求，而 http.Client.Timeout 会把
// 整个 body 读取计入总时长，导致长生成被整体砍断。这里的替代方案：
//   - ResponseHeaderTimeout 限制首字节时间（TTFB，含上游排队）
//   - streamIdleTimeout 限制流式 body 的空闲时间：每收到一行数据重置，
//     连续无数据才中止（对齐 new-api 的 streamingTimeout 模型）
const responseHeaderTimeout = 120 * time.Second

// 变量化以便测试缩短等待。
var streamIdleTimeout = 120 * time.Second

// 空流重试（断流重发）的次数上限与基础退避间隔，实际退避 = attempt * 基础间隔。
// 变量化以便测试缩短等待。
const streamRetryAttempts = 3

var streamRetryDelay = time.Second

func newStreamHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{Transport: transport}
}

// streamWatchdog 监视流式 body 的读取进度。
type streamWatchdog struct {
	Kick    func()      // 每读到一行数据调用
	Tripped func() bool // 看门狗是否已因空闲超时触发
}

// startStreamWatchdog 启动看门狗：超过 idle 未 Kick 则 cancel（中止停滞的流）。
// ctx 需是请求所绑定的 context；cancel 是它的取消函数。
func startStreamWatchdog(ctx context.Context, idle time.Duration, cancel context.CancelFunc) *streamWatchdog {
	kickCh := make(chan struct{}, 1)
	var tripped atomic.Bool

	go func() {
		timer := time.NewTimer(idle)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-kickCh:
				// 排掉可能已到期的 timer，避免刚 Kick 就误触发
				select {
				case <-timer.C:
				default:
				}
				timer.Reset(idle)
			case <-timer.C:
				tripped.Store(true)
				cancel()
				return
			}
		}
	}()

	return &streamWatchdog{
		Kick: func() {
			select {
			case kickCh <- struct{}{}:
			default:
			}
		},
		Tripped: tripped.Load,
	}
}

// idleTimeoutError 返回看门狗触发时用于 Result 的错误。
func idleTimeoutError(idle time.Duration) error {
	return fmt.Errorf("stream idle over %v: no data from upstream", idle)
}
