package commands

import (
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"strings"
)

func registerWorkflow(registry *slashcmd.Registry) {
	registry.Register(slashcmd.Command{Name: "workflow", Description: "设计并执行动态工作流（/workflow <任务>）", Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
		args = strings.TrimSpace(args)
		if args == "" {
			return slashcmd.CommandResult{Output: "用法：/workflow <任务描述>\nAgent 会设计 TypeScript 动态流程并调用 create_workflow，支持 Actor 并行、连续上下文、分支和循环。网页工作流页可查看执行过程、取消和恢复。"}, nil
		}
		return slashcmd.CommandResult{ShouldQuery: true, QueryPrompt: "/workflow " + args}, nil
	}})
}
