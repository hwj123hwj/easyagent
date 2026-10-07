package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	codingprofile "github.com/hwj123hwj/easyagent/internal/agents/coding/profile"
	"github.com/hwj123hwj/easyagent/internal/handoff"
	platformprompt "github.com/hwj123hwj/easyagent/sdk/prompt"
	"github.com/hwj123hwj/easyagent/sdk/skill"
)

// Options configures the coding-agent system prompt.
type Options struct {
	CustomPrompt       string
	CWD                string
	Tools              []agent.Tool
	ContextFiles       []platformprompt.ContextFile
	Skills             []skill.Skill
	AppendSystemPrompt string
	Profile            string
	Goal               string
}

// BuildSystemPrompt constructs the coding-agent system prompt on top of shared prompt context.
func BuildSystemPrompt(opts Options) string {
	var b strings.Builder

	// Determine base prompt: custom > profile-specific > default
	base := opts.CustomPrompt
	if base == "" {
		if profilePrompt := codingprofile.PromptFor(codingprofile.Profile(opts.Profile)); profilePrompt != "" {
			base = profilePrompt
		} else {
			base = defaultPrompt
		}
	}
	b.WriteString(base)
	b.WriteString("\n")

	toolNames := collectToolNames(opts.Tools)
	snippets := collectToolSnippets(opts.Tools)
	guidelines := collectToolGuidelines(opts.Tools, toolNames)

	if len(snippets) > 0 {
		b.WriteString("\n## Tool Summary\n\n")
		for _, name := range toolNames {
			if snippet, ok := snippets[name]; ok {
				b.WriteString(fmt.Sprintf("- **%s**: %s\n", name, snippet))
			}
		}
	}

	if len(opts.Tools) > 0 {
		b.WriteString("\n## Available Tools\n\n")
		for _, tool := range opts.Tools {
			b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", tool.Name(), tool.Description()))
			if params := tool.Parameters(); len(params) > 0 {
				b.WriteString(formatParameters(tool.Name(), params))
			}
		}
	}

	if len(guidelines) > 0 {
		b.WriteString("\n## Guidelines\n\n")
		for _, g := range guidelines {
			b.WriteString(fmt.Sprintf("- %s\n", g))
		}
	}

	if len(opts.ContextFiles) > 0 {
		b.WriteString("\n# Project Context\n\n")
		b.WriteString("Project-specific instructions and guidelines:\n\n")
		for _, cf := range opts.ContextFiles {
			b.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", cf.Path, cf.Content))
		}
	}

	if skillsPrompt := skill.FormatForSystemPrompt(opts.Skills); skillsPrompt != "" {
		b.WriteString("\n")
		b.WriteString(skillsPrompt)
		b.WriteString("\n")
	}

	if opts.AppendSystemPrompt != "" {
		b.WriteString("\n")
		b.WriteString(opts.AppendSystemPrompt)
		b.WriteString("\n")
	}

	// Wiki context injection — if .llm-wiki/index.md exists, inject wiki awareness
	if wikiCtx := buildWikiContext(opts.CWD); wikiCtx != "" {
		b.WriteString("\n")
		b.WriteString(wikiCtx)
		b.WriteString("\n")
	}

	// Goal injection — if a session goal is set, include it in the system prompt
	if opts.Goal != "" {
		b.WriteString("\n## Current Goal\n\n")
		b.WriteString(opts.Goal)
		b.WriteString("\n")
	}

	// Task handoff injection — if a TASK.md exists from a previous session,
	// include it so the agent resumes where it left off.
	if opts.CWD != "" {
		if handoffPrompt := handoff.LoadAsPrompt(opts.CWD); handoffPrompt != "" {
			b.WriteString(handoffPrompt)
		}
	}

	// Profile-specific prompt additions
	if profileAppend := codingprofile.PromptAppendFor(codingprofile.Profile(opts.Profile)); profileAppend != "" {
		b.WriteString(profileAppend)
		b.WriteString("\n")
	}

	b.WriteString("\n---\n")
	b.WriteString(fmt.Sprintf("Current date: %s\n", time.Now().Format("2006-01-02")))
	if opts.CWD != "" {
		cwd := filepath.ToSlash(opts.CWD)
		b.WriteString(fmt.Sprintf("Current working directory: %s\n", cwd))
	}
	if branch := getCurrentGitBranch(opts.CWD); branch != "" {
		b.WriteString(fmt.Sprintf("Current git branch: %s\n", branch))
	}

	return b.String()
}

const defaultPrompt = `You are EasyAgent, a server-side coding agent built in Go. You help users by reading files, executing commands, editing code, and writing new files.

You operate inside an agent loop:
1. Receive a user message
2. Think about what to do
3. Use tools to accomplish the task (you may call multiple tools in parallel when they are independent)
4. Return your response

Be concise, technical, and safe. Always prefer targeted operations over broad changes.`

func collectToolNames(tools []agent.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}

func collectToolSnippets(tools []agent.Tool) map[string]string {
	snippets := make(map[string]string)
	for _, t := range tools {
		if pi, ok := t.(agent.ToolWithPromptInfo); ok {
			if s := pi.PromptSnippet(); s != "" {
				snippets[t.Name()] = s
			}
		}
	}
	return snippets
}

func collectToolGuidelines(tools []agent.Tool, toolNames []string) []string {
	guidelineSet := make(map[string]bool)
	var guidelines []string

	addGuideline := func(g string) {
		g = strings.TrimSpace(g)
		if g != "" && !guidelineSet[g] {
			guidelineSet[g] = true
			guidelines = append(guidelines, g)
		}
	}

	has := make(map[string]bool)
	for _, name := range toolNames {
		has[name] = true
	}

	if has["bash"] && !has["grep"] && !has["find"] && !has["ls"] {
		addGuideline("Use bash for file operations like ls, rg, find")
	} else if has["bash"] && (has["grep"] || has["find"] || has["ls"]) {
		addGuideline("Prefer grep/find/ls tools over bash for file exploration (faster, more targeted)")
	}

	if has["read"] && has["edit"] {
		addGuideline("Use read to examine files before editing; use edit for precise string replacements")
	}

	if has["write"] {
		addGuideline("Use write only for creating new files or complete rewrites; prefer edit for modifications")
	}

	if has["bash"] {
		addGuideline("Avoid destructive commands (rm -rf, etc.) unless explicitly asked")
	}

	for _, t := range tools {
		if pi, ok := t.(agent.ToolWithPromptInfo); ok {
			for _, g := range pi.PromptGuidelines() {
				addGuideline(g)
			}
		}
	}

	addGuideline("Be concise in your responses")
	addGuideline("Show file paths clearly when working with files")
	addGuideline("Explain your reasoning before taking actions")

	return guidelines
}

func formatParameters(toolName string, params map[string]any) string {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		return ""
	}

	required := make(map[string]bool)
	if req, ok := params["required"].([]string); ok {
		for _, r := range req {
			required[r] = true
		}
	}

	var b strings.Builder
	b.WriteString("Parameters:\n")
	for name, schema := range props {
		schemaMap, ok := schema.(map[string]any)
		if !ok {
			continue
		}
		typeStr, _ := schemaMap["type"].(string)
		desc, _ := schemaMap["description"].(string)
		reqMark := ""
		if required[name] {
			reqMark = " (required)"
		}
		b.WriteString(fmt.Sprintf("  - `%s` (%s)%s", name, typeStr, reqMark))
		if desc != "" {
			b.WriteString(fmt.Sprintf(": %s", desc))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// buildWikiContext returns wiki context text if .llm-wiki/index.md exists.
// Returns empty string if wiki is not initialized.
func buildWikiContext(cwd string) string {
	if cwd == "" {
		return ""
	}
	indexPath := filepath.Join(cwd, ".llm-wiki", "index.md")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n# LLM Wiki\n\n")
	b.WriteString("This project has a curated LLM Wiki knowledge base at `.llm-wiki/`. It contains\n")
	b.WriteString("distilled, AI-maintained knowledge about this codebase — architecture,\n")
	b.WriteString("key modules, conventions, and gotchas.\n\n")

	// Inject index.md content so the model knows what pages exist without an extra tool call.
	if idx, err := os.ReadFile(indexPath); err == nil && len(idx) > 0 {
		b.WriteString("## Wiki Index\n\n")
		b.WriteString(string(idx))
		b.WriteString("\n\n")
	}

	b.WriteString("## Consult it proactively\n")
	b.WriteString("- Before exploring the codebase or answering questions about how something works,\n")
	b.WriteString("  consult `.llm-wiki/index.md` first to see if a relevant page already exists.\n")
	b.WriteString("- Prefer reading the matching `.llm-wiki/wiki/*.md` page over re-deriving the same\n")
	b.WriteString("  knowledge by broad code search. Use it to orient yourself, then dig into source.\n")
	b.WriteString("- Treat the wiki as a strong hint, not absolute truth: if a page looks stale or\n")
	b.WriteString("  conflicts with the current code, trust the code and consider updating the wiki.\n\n")
	b.WriteString("## Maintain it when asked\n")
	b.WriteString("When the user asks you to \"save to wiki\", \"learn into wiki\", \"update wiki\", or similar:\n")
	b.WriteString("1. Read `.llm-wiki/index.md` to understand the current structure.\n")
	b.WriteString("2. Create or update pages in `.llm-wiki/wiki/` with YAML frontmatter (`type`, `date`, `tags`).\n")
	b.WriteString("3. Use `[[wikilinks]]` for cross-references between pages.\n")
	b.WriteString("4. Update `.llm-wiki/index.md` to reflect new/changed pages.\n")
	b.WriteString("5. Append an entry to `.llm-wiki/log.md`.\n")
	b.WriteString("6. Never modify files in `.llm-wiki/raw/` — those are immutable sources.\n\n")
	b.WriteString("The user can also use `/wiki` slash commands for structured operations.\n")
	return b.String()
}

func getCurrentGitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(cwd, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if strings.HasPrefix(content, "ref: refs/heads/") {
		return strings.TrimPrefix(content, "ref: refs/heads/")
	}
	return ""
}
