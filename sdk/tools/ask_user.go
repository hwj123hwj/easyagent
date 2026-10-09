package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
)

// AskUserTool pauses the agent loop and presents questions to the user.
// It uses the agent's ConfirmFunc/callback mechanism for user interaction.
//
// The LLM calls ask_user with questions; the tool returns a confirmation
// request that the UI layer renders. When the user answers, the answers
// are captured and returned as the tool result.
type AskUserTool struct{}

// AskUserQuestion represents a single question with options.
type AskUserQuestion struct {
	Question    string          `json:"question"`
	Header      string          `json:"header,omitempty"`
	Options     []AskUserOption `json:"options"`
	MultiSelect bool            `json:"multiSelect,omitempty"`
}

// AskUserOption represents a single answer option.
type AskUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

type AskUserParams struct {
	Questions []AskUserQuestion `json:"questions"`
	Metadata  *AskUserMetadata  `json:"metadata,omitempty"`
}

type AskUserMetadata struct {
	Source string `json:"source,omitempty"`
}

func NewAskUserTool() *AskUserTool {
	return &AskUserTool{}
}

func (t *AskUserTool) Name() string { return "ask_user_question" }

func (t *AskUserTool) Description() string {
	return `Asks the user multiple choice questions to gather information, clarify ambiguity, understand preferences, make decisions or offer them choices.

Use this tool when you need to ask the user questions during execution. This allows you to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices as you work
4. Offer choices to the user about what direction to take.

Usage notes:
- Users will always be able to select "Other" to provide custom text input
- Use multiSelect: true to allow multiple answers to be selected for a question
- If you recommend a specific option, make that the first option in the list and add "(Recommended)" at the end of the label`
}

func (t *AskUserTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"description": "Questions to ask the user (1-4 questions in a single dialog).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The complete question to ask the user. Should be clear, specific, and end with a question mark.",
						},
						"header": map[string]any{
							"type":        "string",
							"description": "Very short label displayed as a chip/tag (max 12 chars). Examples: \"Auth method\", \"Library\".",
						},
						"options": map[string]any{
							"type":        "array",
							"description": "Available choices (2-4 options). Must be distinct. Do not include \"Other\" — it is auto-appended by the UI.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string", "description": "The display text for this option. Should be concise (1-5 words)."},
									"description": map[string]any{"type": "string", "description": "Explanation of what this option means or implies."},
									"preview":     map[string]any{"type": "string", "description": "Optional preview content (markdown) rendered side-by-side when focused."},
								},
								"required": []string{"label"},
							},
						},
						"multiSelect": map[string]any{
							"type":        "boolean",
							"description": "Set to true to allow selecting multiple options. Defaults to false.",
						},
					},
					"required": []string{"question", "options"},
				},
			},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{"type": "string", "description": "Optional identifier for the source of this question."},
				},
			},
		},
		"required": []string{"questions"},
	}
}

func (t *AskUserTool) Validate(raw json.RawMessage) (json.RawMessage, error) {
	var params AskUserParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	if len(params.Questions) == 0 {
		return nil, fmt.Errorf("at least one question is required")
	}
	if len(params.Questions) > 4 {
		return nil, fmt.Errorf("at most 4 questions may be asked in a single call")
	}

	questionTexts := make(map[string]bool)
	for i, q := range params.Questions {
		if strings.TrimSpace(q.Question) == "" {
			return nil, fmt.Errorf("questions[%d]: question text must be non-empty", i)
		}
		if questionTexts[q.Question] {
			return nil, fmt.Errorf("questions[%d]: duplicate question text %q", i, q.Question)
		}
		questionTexts[q.Question] = true

		if len(q.Options) < 2 || len(q.Options) > 4 {
			return nil, fmt.Errorf("questions[%d]: each question must have 2-4 options (got %d)", i, len(q.Options))
		}

		labelSet := make(map[string]bool)
		for j, opt := range q.Options {
			if strings.TrimSpace(opt.Label) == "" {
				return nil, fmt.Errorf("questions[%d].options[%d]: label must be non-empty", i, j)
			}
			if labelSet[opt.Label] {
				return nil, fmt.Errorf("questions[%d].options[%d]: duplicate label %q", i, j, opt.Label)
			}
			labelSet[opt.Label] = true
			lower := strings.ToLower(strings.TrimSpace(opt.Label))
			if lower == "other" || lower == "others" {
				return nil, fmt.Errorf("questions[%d].options[%d]: do not include an \"Other\" option — it is auto-appended by the UI", i, j)
			}
			if opt.Preview != "" && q.MultiSelect {
				return nil, fmt.Errorf("questions[%d].options[%d]: preview is not supported on multiSelect questions", i, j)
			}
		}

		// Auto-fix header
		if q.Header == "" {
			params.Questions[i].Header = "Question"
		} else if len([]rune(q.Header)) > 12 {
			params.Questions[i].Header = string([]rune(q.Header)[:12])
		}
	}

	return json.Marshal(params)
}

// RequiresConfirmation implements agent.ToolWithConfirmation.
// This tool presents questions to the user, so it always requires confirmation.
func (t *AskUserTool) RequiresConfirmation(raw json.RawMessage) (string, bool) {
	var params AskUserParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return "即将向用户提问（参数解析失败）", true
	}

	count := len(params.Questions)
	if count == 1 {
		return fmt.Sprintf("即将向用户提问:\n  %s", params.Questions[0].Question), true
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("即将向用户提问（%d 个问题）:\n", count))
	for _, q := range params.Questions {
		b.WriteString(fmt.Sprintf("  • %s\n", q.Question))
	}
	return b.String(), true
}

// A question needs an answer even in full-access mode.
func (t *AskUserTool) RequiresInteractiveConfirmation() bool { return true }

// AskUserAnswer uses option labels and a separate custom-text field. Questions
// are addressed by their position in the validated request.
type AskUserAnswer struct {
	Selected []string `json:"selected"`
	Text     string   `json:"text,omitempty"`
}

func ValidateAskUserAnswers(raw, response json.RawMessage) ([]AskUserAnswer, error) {
	validated, err := NewAskUserTool().Validate(raw)
	if err != nil {
		return nil, err
	}
	var params AskUserParams
	_ = json.Unmarshal(validated, &params)
	var answers []AskUserAnswer
	if err := json.Unmarshal(response, &answers); err != nil {
		return nil, fmt.Errorf("invalid question answers: %w", err)
	}
	if len(answers) != len(params.Questions) {
		return nil, fmt.Errorf("answer every question (%d required)", len(params.Questions))
	}
	for i, q := range params.Questions {
		answer := &answers[i]
		answer.Text = strings.TrimSpace(answer.Text)
		count := len(answer.Selected)
		if answer.Text != "" {
			count++
		}
		if count == 0 || (!q.MultiSelect && count != 1) {
			return nil, fmt.Errorf("question %d: select %s", i+1, "an answer (single-select permits only one)")
		}
		seen := map[string]bool{}
		for _, label := range answer.Selected {
			valid := false
			for _, option := range q.Options {
				if label == option.Label {
					valid = true
					break
				}
			}
			if !valid || seen[label] {
				return nil, fmt.Errorf("question %d: invalid or duplicate option", i+1)
			}
			seen[label] = true
		}
	}
	return answers, nil
}

func (t *AskUserTool) Execute(_ context.Context, _ json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	return agent.ToolResult{IsError: true}, fmt.Errorf("ask_user_question requires an interactive answer channel; no answers were collected")
}

func (t *AskUserTool) ExecuteConfirmed(ctx context.Context, raw json.RawMessage, decision agent.ConfirmDecision, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agent.ToolResult{IsError: true}, err
	}
	if !decision.Approved {
		return agent.ToolResult{IsError: true}, fmt.Errorf("no user answer was submitted")
	}
	answers, err := ValidateAskUserAnswers(raw, decision.Response)
	if err != nil {
		return agent.ToolResult{IsError: true}, err
	}
	var params AskUserParams
	_ = json.Unmarshal(raw, &params)
	var b strings.Builder
	b.WriteString("User answers:\n")
	for i, answer := range answers {
		values := append([]string(nil), answer.Selected...)
		if answer.Text != "" {
			values = append(values, answer.Text)
		}
		fmt.Fprintf(&b, "%s: %s\n", params.Questions[i].Question, strings.Join(values, "; "))
	}
	return agent.ToolResult{Content: b.String(), Details: map[string]any{"answers": answers}}, nil
}
