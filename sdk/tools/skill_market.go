package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/skillmarket"
)

// SkillMarketTool lets the agent search the official skill store, inspect
// skill details, install skills into the personal skills directory, and
// uninstall previously installed ones.
//
// Installation requires interactive confirmation (network download +
// filesystem write outside the workspace). After a successful install the
// caller should bump the tool revision so the next turn reloads skills.
type SkillMarketTool struct {
	client     *skillmarket.Client
	onMutation func() // optional: called after install/uninstall (e.g. bump tool revision)
}

// SkillMarketParams is the union of the tool's sub-operations.
// Exactly one action is executed per call; unused fields are ignored.
type SkillMarketParams struct {
	Action   string `json:"action"` // search | detail | install | uninstall
	ID       int    `json:"id,omitempty"`
	Query    string `json:"query,omitempty"`
	Category string `json:"category,omitempty"`
	Sort     string `json:"sort,omitempty"`
	Page     int    `json:"page,omitempty"`
	Name     string `json:"name,omitempty"` // uninstall target (directory name)
}

// NewSkillMarketTool creates the tool bound to the given marketplace client.
// WithSkillMarketOnMutation registers a callback invoked after successful
// installs/uninstalls so live sessions can reload skills.
func NewSkillMarketTool(client *skillmarket.Client, opts ...SkillMarketOption) *SkillMarketTool {
	t := &SkillMarketTool{client: client}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// SkillMarketOption configures the skill_market tool.
type SkillMarketOption func(*SkillMarketTool)

// WithSkillMarketOnMutation sets a post-mutation callback (install/uninstall).
func WithSkillMarketOnMutation(fn func()) SkillMarketOption {
	return func(t *SkillMarketTool) { t.onMutation = fn }
}

func (t *SkillMarketTool) Name() string { return "skill_market" }

func (t *SkillMarketTool) Description() string {
	return `Search, inspect, install, and uninstall skills from the official skill marketplace.

Actions:
- search: browse/list skills. Optional query (keyword), category, sort (featured|installs|name), page (1-based).
- detail: full metadata for one skill. Requires id.
- install: download and install a skill into the personal skills directory (~/.agents/skills). Requires id. Follows the session permission mode for confirmation. Newly installed skills take effect on the NEXT turn or session.
- uninstall: remove a previously marketplace-installed skill by name (directory name). Requires name. Hand-written skills are never removable this way.

Use search first to discover what is available, then install by numeric id.`
}

func (t *SkillMarketTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"search", "detail", "install", "uninstall"},
				"description": "Which marketplace operation to perform.",
			},
			"id": map[string]any{
				"type":        "integer",
				"description": "Numeric skill id (required for detail and install).",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Keyword search (search action).",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Category filter (search action).",
			},
			"sort": map[string]any{
				"type":        "string",
				"enum":        []string{"featured", "installs", "name"},
				"description": "Result ordering (search action).",
			},
			"page": map[string]any{
				"type":        "integer",
				"description": "1-based page number (search action).",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Installed skill directory name (uninstall action).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *SkillMarketTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params SkillMarketParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	switch params.Action {
	case "search":
	case "detail", "install":
		if params.ID <= 0 {
			return nil, fmt.Errorf("id must be a positive integer for %q action", params.Action)
		}
	case "uninstall":
		if strings.TrimSpace(params.Name) == "" {
			return nil, fmt.Errorf("name is required for uninstall action")
		}
	default:
		return nil, fmt.Errorf("unknown action %q (want search, detail, install, uninstall)", params.Action)
	}
	return json.Marshal(params)
}

// RequiresConfirmation implements agent.ToolWithConfirmation: installs pull
// remote packages and write outside the workspace, so they follow the session confirmation policy.
func (t *SkillMarketTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params SkillMarketParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "", false
	}
	switch params.Action {
	case "install":
		return fmt.Sprintf("即将从技能市场下载并安装技能 (id=%d) 到个人技能目录，是否继续？", params.ID), true
	case "uninstall":
		return fmt.Sprintf("即将卸载市场安装的技能 %q，该目录将被删除，是否继续？", params.Name), true
	}
	return "", false
}

// IsConcurrencySafe reports search/detail as parallel-safe while install and
// uninstall mutate the skills directory.
func (t *SkillMarketTool) IsConcurrencySafe(raw json.RawMessage) bool {
	var params SkillMarketParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return false
	}
	return params.Action == "search" || params.Action == "detail"
}

func (t *SkillMarketTool) Execute(ctx context.Context, raw json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var params SkillMarketParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return agent.ToolResult{IsError: true}, err
	}

	switch params.Action {
	case "search":
		return t.search(ctx, params)
	case "detail":
		return t.detail(ctx, params)
	case "install":
		return t.install(ctx, params)
	case "uninstall":
		return t.uninstall(params)
	}
	return agent.ToolResult{IsError: true, Content: fmt.Sprintf("unknown action %q", params.Action)}, fmt.Errorf("unknown action %q", params.Action)
}

func (t *SkillMarketTool) search(ctx context.Context, params SkillMarketParams) (agent.ToolResult, error) {
	result, err := t.client.Search(ctx, skillmarket.BrowseQuery{
		Category: params.Category,
		Sort:     params.Sort,
		Query:    params.Query,
		Page:     params.Page,
	})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("Marketplace search failed: %v", err)}, err
	}
	if len(result.Skills) == 0 {
		return agent.ToolResult{Content: "No skills matched the search. Try different keywords or clear the category filter."}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d skills (page %d):\n\n", result.Total, result.Page)
	for _, s := range result.Skills {
		fmt.Fprintf(&b, "- id=%d  %s  v%s\n", s.ID, s.DisplayName, s.Version)
		fmt.Fprintf(&b, "  %s\n", s.Description)
		fmt.Fprintf(&b, "  category=%s installs=%d\n", s.Category, s.InstallCount)
	}
	b.WriteString("\nUse action=detail with a numeric id for full metadata, or action=install to install.")
	return agent.ToolResult{Content: b.String()}, nil
}

func (t *SkillMarketTool) detail(ctx context.Context, params SkillMarketParams) (agent.ToolResult, error) {
	item, err := t.client.Detail(ctx, params.ID, "")
	if err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("Marketplace detail failed: %v", err)}, err
	}
	data, err := json.MarshalIndent(map[string]any{
		"id":            item.ID,
		"name":          item.Name,
		"display_name":  item.DisplayName,
		"description":   item.Description,
		"category":      item.Category,
		"tags":          item.Tags,
		"version":       item.Version,
		"install_count": item.InstallCount,
		"size_bytes":    item.TarballSize,
	}, "", "  ")
	if err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("Failed to format detail: %v", err)}, err
	}
	content := string(data) + "\n\nUse action=install with id=" + fmt.Sprint(item.ID) + " to install this skill."
	return agent.ToolResult{Content: content}, nil
}

func (t *SkillMarketTool) install(ctx context.Context, params SkillMarketParams) (agent.ToolResult, error) {
	item, err := t.client.Install(ctx, params.ID)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("Skill install failed: %v", err)}, err
	}
	if t.onMutation != nil {
		t.onMutation()
	}
	content := fmt.Sprintf("Skill %q (id=%d, version=%s) installed into %s. "+
		"It becomes available to new sessions and to this session on the next turn; "+
		"read its SKILL.md before using it.",
		item.DisplayName, item.ID, item.Version, t.client.SkillsDir())
	return agent.ToolResult{Content: content}, nil
}

func (t *SkillMarketTool) uninstall(params SkillMarketParams) (agent.ToolResult, error) {
	if err := t.client.Uninstall(params.Name); err != nil {
		return agent.ToolResult{IsError: true, Content: fmt.Sprintf("Skill uninstall failed: %v", err)}, err
	}
	if t.onMutation != nil {
		t.onMutation()
	}
	return agent.ToolResult{Content: fmt.Sprintf("Skill %q uninstalled. It disappears from new sessions; already-loaded sessions keep it until reloaded.", params.Name)}, nil
}
