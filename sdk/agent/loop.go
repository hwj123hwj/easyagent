package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
)

// consumeStreamFunc 定义如何消费 LLM 事件流。
// 不同的调用者（RunLoop vs PromptStream）提供不同的实现。
type consumeStreamFunc func(stream *ai.EventStream) (ai.StreamAssistantMessage, error)

// toolBatch 表示一组可一起执行的工具调用。
type toolBatch struct {
	safe  bool          // true = 批次内可并行执行
	calls []ai.ToolCall // 批次内的工具调用
}

// turnAction 表示一轮处理后的动作。
type turnAction int

const (
	actionContinue turnAction = iota // 继续内层循环（有 tool call）
	actionFollowUp                   // 有 follow-up 消息，继续外层循环
	actionDone                       // 对话完成
)

// turnResult 表示一轮处理的结果。
type turnResult struct {
	assistant ai.AssistantMessage
	action    turnAction
	followUp  []ai.Message // action == actionFollowUp 时有值
	goalDone  bool         // true = Agent 明确表示目标已完成
}

// RunLoop 实现双层 Agent 循环。
// 外层循环处理 followUp 消息，内层循环处理 tool call + steering 消息。
func RunLoop(ctx context.Context, a *Agent) (ai.AssistantMessage, error) {
	provider, err := a.provider()
	if err != nil {
		return ai.AssistantMessage{}, err
	}

	// RunLoop 的 stream consumer：只关注 EventDone 和 EventError
	consume := func(stream *ai.EventStream) (ai.StreamAssistantMessage, error) {
		var streamMsg ai.StreamAssistantMessage
		for event := range stream.Events() {
			select {
			case <-ctx.Done():
				return streamMsg, ctx.Err()
			default:
			}
			switch e := event.(type) {
			case ai.EventDone:
				streamMsg = e.Message
			case ai.EventError:
				return streamMsg, fmt.Errorf("llm error: %s", e.Error)
			}
		}
		return streamMsg, nil
	}

	return runAgentLoop(ctx, a, provider, consume)
}

// runAgentLoop 是共享的 Agent 循环核心逻辑。
// RunLoop 和 PromptStream 都调用此函数，通过不同的 consume 回调区分行为。
func runAgentLoop(ctx context.Context, a *Agent, provider interface {
	Stream(ctx context.Context, req ai.StreamRequest) (*ai.EventStream, error)
}, consume consumeStreamFunc) (ai.AssistantMessage, error) {
	pending := a.steeringQueue.Drain()
	turns := 0
	var lastAssistant ai.AssistantMessage

	goalLog("[goal-debug] runAgentLoop started: goal=%q maxTurns=%d\n", a.goal, a.maxTurns)

	// 确定有效 maxTurns：如果有 active goal，取消轮次上限
	// 由 LLM 评估器决定何时停止，而不是硬性 maxTurns
	effectiveMaxTurns := a.maxTurns
	if a.goal != "" {
		effectiveMaxTurns = 0 // 0 = 无限制
		slog.Info("goal-driven loop active", "goal", a.goal, "originalMaxTurns", a.maxTurns)
		goalLog("[goal-debug] goal-driven mode: effectiveMaxTurns=0 (unlimited)\n")
	}

	history := make([]ai.Message, 0, 32)

	// 如果有 session storage，从 session 恢复历史
	if a.session != nil {
		sessionHistory, err := a.session.BuildContext(ctx)
		if err == nil && len(sessionHistory) > 0 {
			history = sessionHistory
		}
	}

	var recoveryErr error
	history, recoveryErr = recoverPendingToolResults(ctx, a, history)
	if recoveryErr != nil {
		return lastAssistant, recoveryErr
	}

	for {
		if err := ctx.Err(); err != nil {
			return lastAssistant, err
		}
		if effectiveMaxTurns > 0 && turns >= effectiveMaxTurns {
			return lastAssistant, nil
		}
		turns++
		a.emit(ctx, EventTurnStart{})

		result, updatedHistory, err := processTurn(ctx, a, provider, consume, history, pending)
		if err != nil {
			return lastAssistant, err
		}

		history = updatedHistory
		lastAssistant = result.assistant
		pending = nil

		switch result.action {
		case actionContinue:
			// 内层循环继续（tool call 后继续）
		case actionFollowUp:
			pending = result.followUp
		// 外层循环继续
		case actionDone:
			// Goal-driven loop：如果有活跃目标且未被标记完成，注入 follow-up 继续
			goalLog("[goal-debug] actionDone: goal=%q goalDone=%v turns=%d\n", a.goal, result.goalDone, turns)
			if a.goal != "" && !result.goalDone {
				slog.Info("goal-driven: injecting follow-up", "goal", a.goal, "turns", turns)
				goalLog("[goal-debug] Injecting follow-up, continuing loop\n")
				pending = []ai.Message{ai.NewTextUserMessage(
					fmt.Sprintf("Reminder: your current goal is \"%s\". Continue working on it.", a.goal),
				)}
				// 继续外层循环（不走 return）
			} else {
				if result.goalDone {
					slog.Info("goal-driven: goal completed, stopping", "turns", turns)
				}
				goalLog("[goal-debug] Loop ending: goalDone=%v goal=%q\n", result.goalDone, a.goal)
				return lastAssistant, nil
			}
		}
	}
}

// recoverPendingToolResults closes an interrupted last turn before accepting a new
// prompt. It records uncertainty, never retries commands or assumes they failed.
func recoverPendingToolResults(ctx context.Context, a *Agent, history []ai.Message) ([]ai.Message, error) {
	completed := make(map[string]bool)
	for i := len(history) - 1; i >= 0; i-- {
		switch msg := history[i].(type) {
		case ai.ToolResultMessage:
			completed[msg.ToolCallID] = true
		case ai.AssistantMessage:
			for _, call := range msg.ToolCalls {
				if completed[call.ID] {
					continue
				}
				result := ai.ToolResultMessage{ToolCallID: call.ID, IsError: true, Content: "Execution interrupted; no result was saved. The command may have produced side effects. Check its state before retrying."}
				if a.session != nil {
					if err := a.session.AppendMessage(ctx, result); err != nil {
						return history, fmt.Errorf("recover interrupted tool: %w", err)
					}
				}
				history = append(history, result)
			}
			return history, nil
		default:
			return history, nil
		}
	}
	return history, nil
}

// processTurn 处理一个 Agent 轮次。
// 返回轮次结果、更新后的历史和可能的错误。
func processTurn(ctx context.Context, a *Agent, provider interface {
	Stream(ctx context.Context, req ai.StreamRequest) (*ai.EventStream, error)
}, consume consumeStreamFunc, history []ai.Message, pending []ai.Message) (turnResult, []ai.Message, error) {
	if len(pending) == 0 && len(history) == 0 {
		pending = []ai.Message{ai.NewTextUserMessage("hello")}
	}

	// 将 pending 消息追加到历史
	history = append(history, pending...)

	// 只保存本轮新增的 pending 消息到 session（避免 O(n²)）
	if a.session != nil {
		for _, msg := range pending {
			_ = a.session.AppendMessage(ctx, msg)
		}
	}

	// 检查是否需要 compaction
	history = a.maybeCompact(ctx, history)

	// 用完整历史调用 LLM
	request := a.llmRequest(history)
	a.emit(ctx, EventContextUsage{Usage: estimateContext(request, latestRequestUsage(history))})
	stream, err := provider.Stream(ctx, request)
	if err != nil {
		return turnResult{}, history, err
	}

	// 消费事件流
	streamMsg, err := consume(stream)
	if err != nil {
		return turnResult{}, history, err
	}

	message, err := a.handleAssistantMessage(streamMsg)
	if err != nil {
		return turnResult{}, history, err
	}

	a.emit(ctx, EventContextUsage{Usage: estimateContext(a.llmRequest(append(append([]ai.Message{}, history...), message)), message.Usage)})

	// 将 assistant message 追加到历史
	history = append(history, message)

	// 保存到 session
	if a.session != nil {
		_ = a.session.AppendMessage(ctx, message)
	}

	// 如果 LLM 返回 error/aborted，终止循环
	if message.StopReason == ai.StopReasonError || message.StopReason == ai.StopReasonAborted {
		return turnResult{assistant: message, action: actionDone}, history, nil
	}

	// 执行 tool calls（如果有的话）
	toolResults, err := executeToolCalls(ctx, a, message.ToolCalls)
	if err != nil {
		return turnResult{assistant: message}, history, err
	}
	a.emit(ctx, EventTurnEnd{Message: message, ToolResults: toolResults})

	// 循环检测：遍历本轮 tool calls 更新检测器，命中则发事件 + 注入提醒（柔性，不中断）。
	if a.loopDetectSettings.Enabled && len(message.ToolCalls) > 0 {
		for _, tc := range message.ToolCalls {
			if a.loopDetect.observe(a.loopDetectSettings.Threshold, tc.Name, tc.Args) {
				a.emit(ctx, EventLoopDetected{ToolName: tc.Name, RepeatCount: a.loopDetect.repeatCount})
				reminder := renderReminder(a.loopDetectSettings.ReminderTemplate, tc.Name, tc.Args, a.loopDetect.repeatCount)
				a.followUpQueue.Enqueue(ai.NewTextUserMessage(reminder))
				slog.Info("loop detected", "tool", tc.Name, "repeat", a.loopDetect.repeatCount)
				break // 一轮内只注入一次提醒
			}
		}
	}

	// 如果有 tool calls，将 tool results 追加到历史并继续内层循环
	if message.StopReason == ai.StopReasonToolUse && len(toolResults) > 0 {
		history = append(history, toolResults...)

		// 保存 tool results 到 session
		if a.session != nil {
			// A canceled request must still close its persisted tool-call records.
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			for _, tr := range toolResults {
				if err := a.session.AppendMessage(saveCtx, tr); err != nil {
					return turnResult{assistant: message}, history, fmt.Errorf("persist tool result: %w", err)
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return turnResult{assistant: message}, history, err
		}
		return turnResult{assistant: message, action: actionContinue}, history, nil
	}

	// 内层循环结束（无 tool call），检查 follow-up
	if next := a.followUpQueue.Drain(); len(next) > 0 {
		return turnResult{assistant: message, action: actionFollowUp, followUp: next}, history, nil
	}

	// Goal-driven：用 LLM 评估目标是否已完成
	isGoalDone := false
	if a.goal != "" && message.StopReason == ai.StopReasonStop {
		goalLog("[goal-debug] Evaluating goal completion (responseLen=%d, stopReason=%s)\n", len(message.Text), message.StopReason)
		done, reason := evaluateGoalCompletion(ctx, a.registry, a.model, message.Text, a.goal)
		goalLog("[goal-debug] Evaluator result: done=%v reason=%q\n", done, reason)
		if done {
			isGoalDone = true
			clearedGoal := a.goal
			a.goal = "" // 清除 goal，下一轮正常停止
			a.emit(ctx, EventGoalCompleted{Goal: clearedGoal})
			slog.Info("goal evaluated as completed", "goal", clearedGoal, "reason", reason)
		}
	}

	if a.goal != "" && message.StopReason != ai.StopReasonStop {
		goalLog("[goal-debug] Goal active but StopReason=%s, NOT evaluating (textLen=%d)\n", message.StopReason, len(message.Text))
	}
	return turnResult{assistant: message, action: actionDone, goalDone: isGoalDone}, history, nil
}

// maybeCompact 检查是否需要压缩上下文，如果需要则执行压缩并返回新的历史。
// 两级压缩：先 MicroCompact（低阈值，清旧 tool result，不调 LLM），
// 再全量 AutoCompact（高阈值，调 LLM 摘要）。
func (a *Agent) maybeCompact(ctx context.Context, history []ai.Message) []ai.Message {
	if !a.compactionSettings.Enabled {
		return history
	}

	// 估算当前 token 数
	contextTokens := compaction.EstimateTokens(history)
	contextWindow := a.model.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 128000
	}

	// 第一级：MicroCompact（清旧 tool result，不依赖 summarizeFunc）
	if compaction.ShouldMicroCompact(contextTokens, contextWindow, a.compactionSettings) {
		newHistory, cleared := compaction.MicroCompact(history, a.compactionSettings.MicroKeepRecent)
		if cleared > 0 {
			a.emit(ctx, EventMicroCompacted{ClearedResults: cleared, TokensBefore: contextTokens, TokensAfter: compaction.EstimateTokens(newHistory)})
			history = newHistory
			contextTokens = compaction.EstimateTokens(history) // 重算：Micro 后可能降到全量阈值以下
		}
	}

	// 第二级：全量 AutoCompact（需 summarizeFunc 才能做 LLM 摘要）
	if a.summarizeFunc == nil {
		return history
	}
	if !compaction.ShouldCompact(contextTokens, contextWindow, a.compactionSettings) {
		return history
	}

	// 执行压缩
	historyPart, recentPart := compaction.SplitMessages(history, a.compactionSettings.KeepRecentTokens)
	if len(historyPart) == 0 {
		return history
	}

	// PreCompress hook：观察型，确认要压缩时触发。不能阻止或修改压缩。
	runPreCompressHooks(ctx, a.lifecycleHooks.PreCompress, PreCompressEvent{
		ContextTokens: contextTokens,
		ContextWindow: contextWindow,
		MessageCount:  len(historyPart),
	})

	summary, err := compaction.Compact(ctx, historyPart, recentPart, "", a.summarizeFunc)
	if err != nil {
		// 压缩失败，继续使用完整历史
		a.emit(ctx, EventCompactionFailed{Error: err.Error()})
		return history
	}

	info := compactionInfo("automatic", "", history, summary, recentPart)
	// Persist compaction entry to session storage so it survives across prompts.
	if a.session != nil {
		if pErr := a.session.AppendCompactionKeeping(ctx, summary, recentPart, info); pErr != nil {
			a.emit(ctx, EventCompactionFailed{Error: "persist compaction: " + pErr.Error()})
			return history
		}
	}

	a.emit(ctx, EventCompacted{
		Info:        info,
		Summary:     summary,
		TrimmedFrom: len(history),
		TrimmedTo:   len(recentPart) + 1,
	})

	// 替换历史：summary + recent
	newHistory := make([]ai.Message, 0, len(recentPart)+1)
	newHistory = append(newHistory, ai.NewTextUserMessage("Context summary from previous conversation:\n\n"+summary))
	newHistory = append(newHistory, recentPart...)

	return newHistory
}

func executeToolCalls(ctx context.Context, a *Agent, calls []ai.ToolCall) ([]ai.Message, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	batches := partitionToolCalls(ctx, a, calls)
	var allResults []ai.Message

	for i, batch := range batches {
		// 批次开始事件
		names := make([]string, 0, len(batch.calls))
		for _, c := range batch.calls {
			names = append(names, c.Name)
		}
		a.emit(ctx, EventToolBatchStart{
			BatchIndex: i,
			Safe:       batch.safe,
			ToolNames:  names,
		})

		var results []ai.Message
		var err error

		if batch.safe {
			results, err = executeToolCallsParallel(ctx, a, batch.calls)
		} else {
			results, err = executeToolCallsSequential(ctx, a, batch.calls)
		}

		if err != nil {
			// 返回已执行的结果（支持未来 continueOnError 扩展）
			return allResults, err
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// partitionToolCalls 将工具调用按并发安全性分区为有序的批次。
//
// 分区规则：
//  1. 查找每个 call 对应的 tool
//  2. 若 tool 实现了 ConcurrencySafeChecker 且 IsConcurrencySafe(params) == true → safe
//     若 tool 实现了 ToolWithMode 且 ExecutionMode() == Sequential → unsafe（保守策略）
//     两者都不满足 → unsafe（保守策略）
//  3. 连续的 safe call 合入同一并行批次
//  4. 每个 unsafe call 独占一个串行批次
//
// 注：此处直接将 call.Args（string）转为 json.RawMessage 传给 IsConcurrencySafe，
// 无需先 validate。对只读工具而言 IsConcurrencySafe 始终返回 true，不依赖参数内容。
// 参数预留用于未来可能的动态判断。
func partitionToolCalls(ctx context.Context, a *Agent, calls []ai.ToolCall) []toolBatch {
	var batches []toolBatch
	var currentBatch *toolBatch

	for _, call := range calls {
		safe := isToolCallSafe(a, call)

		if safe {
			if currentBatch == nil || !currentBatch.safe {
				// 开始新的并行批次
				batches = append(batches, toolBatch{safe: true})
				currentBatch = &batches[len(batches)-1]
			}
			currentBatch.calls = append(currentBatch.calls, call)
		} else {
			// 不安全：独占串行批次
			batches = append(batches, toolBatch{safe: false, calls: []ai.ToolCall{call}})
			currentBatch = nil
		}
	}

	return batches
}

// isToolCallSafe 判断单个 tool call 是否可安全并发执行。
func isToolCallSafe(a *Agent, call ai.ToolCall) bool {
	tool, ok := a.tools[call.Name]
	if !ok {
		return false // 未知工具，保守策略
	}

	// 优先级 1: ToolWithMode.Sequential → 保守不安全
	if tm, ok := tool.(ToolWithMode); ok && tm.ExecutionMode() == ExecutionModeSequential {
		return false
	}

	// 优先级 2: ConcurrencySafeChecker
	if csc, ok := tool.(ConcurrencySafeChecker); ok {
		return csc.IsConcurrencySafe(json.RawMessage(call.Args))
	}

	// 默认不安全
	return false
}

func executeToolCallsParallel(ctx context.Context, a *Agent, calls []ai.ToolCall) ([]ai.Message, error) {
	results := make([]ai.Message, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, call ai.ToolCall) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[idx] = ai.ToolResultMessage{
						ToolCallID: call.ID,
						Content:    fmt.Sprintf("tool panic: %v", r),
						IsError:    true,
					}
				}
			}()
			results[idx] = executeOneTool(ctx, a, call)
		}(i, call)
	}
	wg.Wait()
	return results, nil
}

func executeToolCallsSequential(ctx context.Context, a *Agent, calls []ai.ToolCall) ([]ai.Message, error) {
	results := make([]ai.Message, len(calls))
	for i, call := range calls {
		func(idx int, call ai.ToolCall) {
			defer func() {
				if r := recover(); r != nil {
					results[idx] = ai.ToolResultMessage{
						ToolCallID: call.ID,
						Content:    fmt.Sprintf("tool panic: %v", r),
						IsError:    true,
					}
				}
			}()
			results[idx] = executeOneTool(ctx, a, call)
		}(i, call)
	}
	return results, nil
}

func executeOneTool(ctx context.Context, a *Agent, call ai.ToolCall) (result ai.Message) {
	started := time.Now()
	defer func() {
		if message, ok := result.(ai.ToolResultMessage); ok {
			message.DurationMS = time.Since(started).Milliseconds()
			result = message
		}
	}()
	if err := ctx.Err(); err != nil {
		return ai.ToolResultMessage{ToolCallID: call.ID, Content: "tool not started: " + err.Error(), IsError: true}
	}

	// 1. Find tool
	tool, ok := a.tools[call.Name]
	if !ok {
		return ai.ToolResultMessage{ToolCallID: call.ID, Content: fmt.Sprintf("tool %q not found", call.Name), IsError: true}
	}

	// 2. Emit start (with raw args for UI display)
	a.emit(ctx, EventToolExecutionStart{ToolCallID: call.ID, ToolName: call.Name, Args: string(call.Args)})

	// 3. Validate args
	validated, err := a.decodeToolArgs(call.Args, tool)
	if err != nil {
		a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: err.Error(), IsError: true})
		return ai.ToolResultMessage{ToolCallID: call.ID, Content: err.Error(), IsError: true}
	}

	// 4. Optional PrepareArguments
	args := validated
	if preparer, ok := tool.(ToolWithPrepareArguments); ok {
		prepared, err := preparer.PrepareArguments(ctx, validated)
		if err != nil {
			a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: err.Error(), IsError: true})
			return ai.ToolResultMessage{ToolCallID: call.ID, Content: err.Error(), IsError: true}
		}
		args = prepared
	}

	// Build call context for hooks
	callCtx := ToolCallContext{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		RawArgs:    json.RawMessage(call.Args),
		Args:       args,
	}

	// 5. Run before hooks
	if a.hookSystem != nil {
		callCtx, err = a.hookSystem.RunBefore(ctx, a.lifecycleHooks.Before, callCtx)
	} else {
		for _, hook := range a.lifecycleHooks.Before {
			callCtx, err = hook(ctx, callCtx)
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		// Before hook blocked execution — emit end with error
		a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: err.Error(), IsError: true})
		return ai.ToolResultMessage{ToolCallID: call.ID, Content: err.Error(), IsError: true}
	}
	args = callCtx.Args

	// 5.5 Confirmation gate: 危险工具执行前向用户确认（若工具声明需要且注入了 ConfirmFunc）
	if confirmer, ok := tool.(ToolWithConfirmation); ok {
		if desc, needConfirm := confirmer.RequiresConfirmation(args); needConfirm {
			a.emit(ctx, EventConfirmationRequest{
				ToolCallID:  call.ID,
				ToolName:    call.Name,
				Description: desc,
			})

			// 未注入 ConfirmFunc（serve/feishu 单向流）→ 默认放行。
			approved := true
			reason := ""
			gate, gated := tool.(ToolRequiringConfirmation)
			requiresApproval := gated && gate.RequiresConfirmationAvailable()
			if requiresApproval && a.confirmFunc == nil {
				approved = false
				reason = "this tool requires an interactive approval or explicit server trust"
			}
			if a.confirmFunc != nil {
				decision := a.confirmFunc(ctx, ConfirmationRequest{
					RequiresApproval: requiresApproval,
					ToolCallID:       call.ID,
					ToolName:         call.Name,
					Args:             args,
					Description:      desc,
				})
				approved = decision.Approved
				reason = decision.Reason
			}
			a.emit(ctx, EventConfirmationResult{
				ToolCallID: call.ID,
				Approved:   approved,
				Reason:     reason,
			})

			if !approved {
				// 用户拒绝：阻断执行，把"用户拒绝"回告 LLM。
				// IsError=false——避免 Agent 误判为系统错误而重试；内容明确告知被拒。
				msg := "user declined this action"
				if reason != "" {
					msg = fmt.Sprintf("user declined this action: %s", reason)
				}
				details := map[string]any{"approval": "declined"}
				a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: msg, Details: details, IsError: false})
				return ai.ToolResultMessage{ToolCallID: call.ID, Content: msg, Details: details, IsError: false}
			}
		}
	}

	// 6. Build onUpdate callback
	onUpdate := func(pr PartialResult) {
		a.emit(ctx, EventToolExecutionUpdate{
			ToolCallID:    call.ID,
			ToolName:      call.Name,
			Args:          string(args),
			PartialResult: pr,
		})
	}

	// 7. Execute tool
	rawResult, err := tool.Execute(ctx, args, onUpdate)
	if err != nil {
		// Some tools (e.g. bash) return both a result with output and an error.
		// Prefer the result content (actual command output/error) over err.Error()
		// which may be a generic message like "command exited with code 127".
		content := err.Error()
		if rawResult.Content != "" {
			content = rawResult.Content
		}
		a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: content, IsError: true})
		return ai.ToolResultMessage{ToolCallID: call.ID, Content: content, IsError: true}
	}

	// 8. Run after hooks
	preHookResult := rawResult
	if a.hookSystem != nil {
		rawResult, err = a.hookSystem.RunAfter(ctx, a.lifecycleHooks.After, callCtx, rawResult)
	} else {
		for _, hook := range a.lifecycleHooks.After {
			rawResult, err = hook(ctx, callCtx, rawResult)
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		// After hook failed — treat as execution failure.
		// Wrap with AfterHookError to preserve pre-hook result for debugging.
		hookErr := NewAfterHookError(err, preHookResult)
		errMsg := fmt.Sprintf("%s (original result: %s)", hookErr.Error(), preHookResult.Content)
		a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: errMsg, IsError: true})
		return ai.ToolResultMessage{ToolCallID: call.ID, Content: errMsg, IsError: true}
	}

	// 9. Emit end
	// Result 给 UI 展示，优先 UserFacing（给用户看的更详细内容）；回填 LLM 用 Content（见 appendToolResult）。
	a.emit(ctx, EventToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: rawResult.DisplayText(), Details: rawResult.Details, IsError: rawResult.IsError})
	return a.appendToolResult(call, rawResult)
}

func marshalArgs(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
