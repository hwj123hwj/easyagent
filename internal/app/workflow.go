package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/internal/dynamicflow"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

func (a *App) DynamicWorkflows() *dynamicflow.Manager { return a.flows }

type workflowApplication struct {
	runtime.Application
	app *App
}

func (a workflowApplication) BuildTools(opts runtime.ToolBuildOptions) []agent.Tool {
	for _, name := range []string{"create_workflow", "get_workflow", "resume_workflow"} {
		opts.ExtensionTools = append(opts.ExtensionTools, &workflowTool{app: a.app, name: name, parent: opts.SessionID})
	}
	return a.Application.BuildTools(opts) // original allow/block filters apply to all three tools
}

type workflowHost struct{ app *App }

func (h workflowHost) Prepare(_ context.Context, parent string) (json.RawMessage, error) {
	sess, ok := h.app.sessionStore.Get(parent)
	if !ok {
		return nil, errors.New("工作流需要已加载的父会话")
	}
	provider, model := sess.ModelInfo()
	policy := actorConfig{Workspace: sess.Workspace(), Provider: provider, Model: model, RequireConfirmation: sess.ConfirmEnabled(), AllowedTools: sess.ToolNames(), EnableBash: h.app.Config().EnableBash, AllowOutsideWorkspace: h.app.Config().AllowOutsideWorkspace}
	return json.Marshal(policy)
}

func (h workflowHost) Actor(ctx context.Context, parent string, policy json.RawMessage, persona dynamicflow.Persona, existing string) (string, error) {
	a := h.app
	var record actorConfig
	if len(policy) == 0 || json.Unmarshal(policy, &record) != nil {
		return "", errors.New("缺少工作流创建时的执行配置，无法安全恢复")
	}
	var parentSession *runtime.AgentSession
	if parent != "" {
		parentSession, _ = a.sessionStore.Get(parent)
	}
	if record.RequireConfirmation && (parentSession == nil || parentSession.ConfirmationCallback() == nil) {
		return "", errors.New("该 Actor 需要原会话的工具确认，请从带确认交互的入口恢复")
	}
	cfg := a.Config()
	record.apply(&cfg)
	deps := a.deps()
	deps.Application = a.ResolveApplication("") // actors never receive workflow-launch tools
	cfg.PromptTemplate += "\n\nWorkflow actor: " + persona.Name + "\n" + persona.System
	options := runtime.AgentSessionOptions{Config: cfg, SkillDirs: a.skillDirs}
	var sess *runtime.AgentSession
	var err error
	if existing != "" {
		if !a.sessionMgr.Exists(existing) {
			return "", errors.New("Actor 会话已删除，无法安全恢复")
		}
		sess, err = a.loadWorkflowActor(ctx, existing)
	} else {
		sess, err = a.sessionStore.Create(ctx, options, deps)
	}
	if err != nil {
		return "", err
	}
	if existing == "" {
		record.Persona = persona
		if err = a.saveActorConfig(sess.SessionID(), record); err != nil {
			return "", err
		}
	}
	if parentSession != nil && parentSession.ConfirmationCallback() != nil {
		callback := parentSession.ConfirmationCallback()
		sess.SetConfirmFunc(func(ctx context.Context, request agent.ConfirmationRequest) agent.ConfirmDecision {
			a.workflowConfirmMu.Lock()
			defer a.workflowConfirmMu.Unlock()
			if ctx.Err() != nil {
				return agent.ConfirmDecision{Approved: false, Reason: "工作流已取消"}
			}
			return callback(ctx, request)
		})
	}
	return sess.SessionID(), nil
}
func (h workflowHost) Ask(ctx context.Context, id, prompt string, typed bool, schema json.RawMessage) (string, error) {
	sess, ok := h.app.sessionStore.Get(id)
	if !ok {
		return "", errors.New("workflow actor not loaded")
	}
	if typed {
		prompt += "\n\nReturn your final result as JSON only, matching this JSON Schema. No Markdown fences or commentary outside JSON:\n" + string(schema)
	}
	message, err := sess.Prompt(ctx, prompt)
	if err != nil {
		return "", err
	}
	if message.ErrorMsg != "" {
		return "", errors.New(message.ErrorMsg)
	}
	return message.Text, nil
}

type workflowTool struct {
	app          *App
	name, parent string
}

func (t *workflowTool) Name() string { return t.name }
func (t *workflowTool) Description() string {
	switch t.name {
	case "create_workflow":
		return `Execute a dynamic TypeScript workflow using the ZCode engine. Script body supports top-level await/return, agent(name,{system}), actor.ask<T>(instructions), Promise.all, branches and loops. Actors retain context across asks; distinct actors run concurrently. Define interface T for structured results. Only actor tools can access the workspace; do not use world/files/git/artifact APIs. Return a final artifact from the script. This tool waits for completion; cancelling the conversation cancels this run. Successful asks are journaled for resume.`
	case "resume_workflow":
		return "Resume an interrupted dynamic workflow using its original script and completed results. Incomplete tasks may repeat external actions; inspect the run before resuming."
	default:
		return "Read a dynamic workflow's status, actor sessions, and final result."
	}
}
func (t *workflowTool) Parameters() map[string]any {
	properties := map[string]any{"run_id": map[string]any{"type": "string"}}
	required := []string{"run_id"}
	if t.name == "create_workflow" {
		properties = map[string]any{"name": map[string]any{"type": "string"}, "script": map[string]any{"type": "string", "description": "TypeScript async function body. Example: const a = agent('researcher'); const x = await a.ask('Research the task'); return await a.ask('Review and improve: '+x);"}}
		required = []string{"name", "script"}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func (t *workflowTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Name, Script string
		RunID        string `json:"run_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if t.name == "create_workflow" {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Script) == "" || len(p.Script) > 64<<10 {
			return nil, errors.New("name and script (<=64 KiB) required")
		}
	} else if p.RunID == "" {
		return nil, errors.New("run_id required")
	}
	return params, nil
}
func (t *workflowTool) Execute(ctx context.Context, params json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		Name, Script string
		RunID        string `json:"run_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return agent.ToolResult{}, err
	}
	var run *dynamicflow.Run
	var err error
	switch t.name {
	case "create_workflow":
		run, err = t.app.flows.Start(ctx, p.Name, p.Script, t.parent)
	case "resume_workflow":
		run, err = t.app.flows.Resume(ctx, p.RunID)
	default:
		run, err = t.app.flows.Get(p.RunID)
	}
	if err != nil {
		return agent.ToolResult{}, err
	}
	if t.name != "get_workflow" {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for run.Status == "running" {
			if onUpdate != nil {
				onUpdate(agent.PartialResult{Content: fmt.Sprintf("工作流 %s · %s · %d 个 Actor", run.ID, run.Status, len(run.Actors))})
			}
			select {
			case <-ctx.Done():
				_ = t.app.flows.Cancel(run.ID)
				return agent.ToolResult{}, ctx.Err()
			case <-ticker.C:
			}
			run, err = t.app.flows.Get(run.ID)
			if err != nil {
				return agent.ToolResult{}, err
			}
		}
	}
	content, _ := json.Marshal(map[string]any{"run_id": run.ID, "status": run.Status, "sessions": run.Actors, "result": run.Result, "error": run.Error})
	return agent.ToolResult{Content: string(content), IsError: run.Status != "completed" && run.Status != "running", Details: map[string]string{"workflow_run_id": run.ID}}, nil
}

// Keep routing instructions in the system prompt so user messages and titles
// retain the original task instead of exposing internal orchestration text.
func (a workflowApplication) BuildPrompt(opts runtime.PromptBuildOptions, profile, goal string) string {
	base := a.Application.BuildPrompt(opts, profile, goal)
	return base + `

When the user starts a message with /workflow followed by whitespace, design and execute a dynamic workflow for that task. First decide named actors, which asks share context, parallel branches and final output. Then call create_workflow with a TypeScript script; do not substitute YAML or ordinary sequential tool calls. Use agent(name,{system}) and ask<T>(), define interface T for typed returns. Actor tasks can use the regular EasyAgent tools. Only supported workflow APIs are agent, ask, Promise.all, report and phase; use actors for all external operations. Include necessary context from this conversation in actor instructions. Inspect compilation errors and fix the script if needed. After completion summarize the actual result and run ID. Avoid duplicate execution after a runtime failure; inspect the run and use resume_workflow where appropriate.`
}
