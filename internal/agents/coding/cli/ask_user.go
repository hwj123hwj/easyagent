package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/hwj123hwj/easyagent/internal/ui"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/tools"
)

// promptAskUser renders the ask_user_question panel on the terminal and reads
// the answers. It returns decision.Response as the JSON encoded answers so
// AskUserTool.ExecuteConfirmed can validate them, mirroring the desktop
// QuestionPrompt flow.
func promptAskUser(out io.Writer, in io.Reader, req agent.ConfirmationRequest) agent.ConfirmDecision {
	questions, err := tools.ParseAskUserQuestions(req.Args)
	if err != nil {
		return agent.ConfirmDecision{Approved: false, Reason: fmt.Sprintf("invalid questions: %v", err)}
	}

	reader := bufio.NewReader(in)
	answers := make([]tools.AskUserAnswer, len(questions))
	for i, q := range questions {
		fmt.Fprintf(out, "\n%s[%d/%d] %s%s%s\n", ui.ColorYellow, i+1, len(questions), ui.ColorBold, q.Question, ui.ColorReset)
		if q.MultiSelect {
			fmt.Fprintf(out, "%s  （可多选，逗号分隔编号）%s\n", ui.ColorDim, ui.ColorReset)
		}
		for j, opt := range q.Options {
			desc := ""
			if opt.Description != "" {
				desc = " — " + opt.Description
			}
			fmt.Fprintf(out, "  %s%d.%s %s%s\n", ui.ColorCyan, j+1, ui.ColorReset, opt.Label, desc)
		}
		fmt.Fprintf(out, "  %s0.%s 其他（输入自定义回答）\n", ui.ColorCyan, ui.ColorReset)

		answer := &answers[i]
		for {
			fmt.Fprintf(out, "%s选择%s: ", ui.ColorYellow, ui.ColorReset)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if idxs, text, ok := parseAnswerChoice(line, len(q.Options)); ok {
				if text != "" {
					answer.Selected = nil
					answer.Text = text
				} else {
					answer.Selected = make([]string, 0, len(idxs))
					for _, n := range idxs {
						answer.Selected = append(answer.Selected, q.Options[n-1].Label)
					}
					answer.Text = ""
				}
				break
			}
			fmt.Fprintf(out, "%s无效输入：请输入编号（多选用逗号），或 0 加自定义文本%s\n", ui.ColorYellow, ui.ColorReset)
		}
	}

	encoded, err := json.Marshal(answers)
	if err != nil {
		return agent.ConfirmDecision{Approved: false, Reason: fmt.Sprintf("encode answers: %v", err)}
	}
	return agent.ConfirmDecision{Approved: true, Response: encoded}
}

// parseAnswerChoice parses "1", "1,3", "0 自定义文本" / "0: 文本".
// It returns 1-based option indices, or custom text when the user chose 0.
func parseAnswerChoice(line string, optionCount int) ([]int, string, bool) {
	if line == "" {
		return nil, "", false
	}
	// Custom text: "0 ..." or "0: ..."/"0：..."
	if line == "0" {
		// empty custom text is not an answer; force the user to type something
		return nil, "", false
	}
	if line[0] == '0' {
		rest := strings.TrimSpace(strings.TrimLeft(line[1:], ":： \t"))
		if rest == "" {
			return nil, "", false
		}
		return nil, rest, true
	}

	parts := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\t' })
	seen := map[int]bool{}
	var idxs []int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > optionCount || seen[n] {
			return nil, "", false
		}
		seen[n] = true
		idxs = append(idxs, n)
	}
	if len(idxs) == 0 {
		return nil, "", false
	}
	return idxs, "", true
}
