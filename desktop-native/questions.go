package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

type userQuestion struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Multi    bool             `json:"multiSelect"`
	Options  []questionOption `json:"options"`
}
type questionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Preview     string `json:"preview"`
}
type questionAnswer struct {
	Selected []string `json:"selected"`
	Text     string   `json:"text,omitempty"`
}
type questionForm struct {
	Answers []questionAnswer
	Other   []bool
}

func parseNativeQuestions(raw any) []userQuestion {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var args struct {
		Questions []userQuestion `json:"questions"`
	}
	if json.Unmarshal(data, &args) != nil || len(args.Questions) == 0 || len(args.Questions) > 4 {
		return nil
	}
	for _, q := range args.Questions {
		if strings.TrimSpace(q.Question) == "" || len(q.Options) < 2 || len(q.Options) > 4 {
			return nil
		}
		for _, option := range q.Options {
			if strings.TrimSpace(option.Label) == "" {
				return nil
			}
		}
	}
	return args.Questions
}
func nativeAnswersComplete(questions []userQuestion, answers []questionAnswer) bool {
	if len(questions) == 0 || len(questions) != len(answers) {
		return false
	}
	for i, q := range questions {
		answer := answers[i]
		if utf8.RuneCountInString(answer.Text) > 4000 {
			return false
		}
		count := len(answer.Selected)
		if strings.TrimSpace(answer.Text) != "" {
			count++
		}
		if count == 0 || !q.Multi && count != 1 {
			return false
		}
		seen := map[string]bool{}
		for _, label := range answer.Selected {
			valid := false
			for _, o := range q.Options {
				if o.Label == label {
					valid = true
				}
			}
			if !valid || seen[label] {
				return false
			}
			seen[label] = true
		}
	}
	return true
}
func (a *preview) questionFields(c *ui.Context, s *session, value confirmation) {
	questions := parseNativeQuestions(value.Args)
	root := ui.Column(c.Key(s.RemoteID + "/" + value.ID)).Gap(10).MinWidth(0)
	form := ui.Local(root, "answers", func() questionForm {
		return questionForm{Answers: make([]questionAnswer, len(questions)), Other: make([]bool, len(questions))}
	})
	busy := a.Live.Loading["confirm/"+s.RemoteID]
	root.Children(func() {
		if len(questions) == 0 {
			ui.Text(c, "问题格式无效，请跳过后让 Agent 重新提问。").TextColor(c.Theme().Danger).FontSize(12)
		}
		ui.Scroll(c).MaxHeight(350).MinWidth(0).Children(func() {
			ui.Column(c).Gap(14).Children(func() {
				for index, q := range questions {
					ui.Column(c.Key(index)).Gap(6).Disabled(busy).Children(func() {
						ui.Row(c).Gap(6).Wrap().Children(func() {
							if q.Header != "" {
								ui.Text(c, q.Header).FontSize(10).Padding(3, 6).Background(c.Theme().Accent.Alpha(.1)).Radius(4)
							}
							ui.Text(c, q.Question).FontSize(13).FontWeight(600)
							if q.Multi {
								a.note(c, "可多选")
							}
						})
						picked := ""
						if len(form.Answers[index].Selected) > 0 {
							picked = form.Answers[index].Selected[0]
						}
						if form.Other[index] && !q.Multi {
							picked = "__other__"
						}
						changed := false
						ui.RadioGroup(c, func() {
							for _, option := range q.Options {
								optionRoot := ui.Column(c.Key(option.Label)).Gap(3).Padding(6, 8).Border(1, c.Theme().Border).Radius(6)
								optionRoot.Children(func() {
									if q.Multi {
										selected := false
										for _, label := range form.Answers[index].Selected {
											if label == option.Label {
												selected = true
											}
										}
										if ui.Checkbox(c, &selected, option.Label).Changed() {
											next := []string{}
											for _, label := range form.Answers[index].Selected {
												if label != option.Label {
													next = append(next, label)
												}
											}
											if selected {
												next = append(next, option.Label)
											}
											form.Answers[index].Selected = next
										}
									} else {
										if ui.Radio(c, &picked, option.Label, option.Label).Changed() {
											changed = true
										}
									}
									if option.Description != "" {
										ui.Text(c, option.Description).FontSize(11).LineHeight(1.5).TextColor(c.Theme().TextMuted)
									}
								})
							}
							if q.Multi {
								if ui.Checkbox(c, &form.Other[index], "其他回答").Changed() && !form.Other[index] {
									form.Answers[index].Text = ""
								}
							} else {
								if ui.Radio(c, &picked, "__other__", "其他回答").Changed() {
									changed = true
								}
							}
						})
						if changed {
							form.Other[index] = picked == "__other__"
							form.Answers[index].Text = ""
							form.Answers[index].Selected = []string{}
							if picked != "__other__" {
								form.Answers[index].Selected = []string{picked}
							}
						}
						if form.Other[index] {
							ui.TextArea(c, &form.Answers[index].Text).Lines(2, 5).Label(fmt.Sprintf("其他回答 %d", index+1)).Placeholder("填写你的回答…")
							if utf8.RuneCountInString(form.Answers[index].Text) > 4000 {
								ui.Text(c, "回答最多 4000 字").FontSize(11).TextColor(c.Theme().Danger)
							}
						}
						for _, option := range q.Options {
							if option.Preview == "" {
								continue
							}
							for _, label := range form.Answers[index].Selected {
								if label == option.Label {
									block := ui.Column(c.Key("preview/" + option.Label)).Padding(10).Background(c.Theme().SurfaceHover).Radius(6)
									doc := ui.Local(block, "markdown", func() markdownDocument { return markdownDocument{} })
									block.Children(func() { a.renderSessionMarkdown(c, doc, option.Preview, s, "", false) })
								}
							}
						}
					})
				}
			})
		})
		ui.Row(c).Gap(8).Children(func() {
			a.action(c, "跳过问题").Disabled(busy).OnClick(func() { a.confirmSession(s, value.ID, false, nil) })
			a.primary(c, "提交回答").Disabled(busy || !nativeAnswersComplete(questions, form.Answers)).OnClick(func() {
				snapshot := make([]questionAnswer, len(form.Answers))
				for i, answer := range form.Answers {
					snapshot[i] = questionAnswer{Selected: append([]string{}, answer.Selected...), Text: strings.TrimSpace(answer.Text)}
				}
				a.confirmSession(s, value.ID, true, snapshot)
			})
		})
	})
}
func (a *preview) confirmSession(s *session, id string, approved bool, answers []questionAnswer) {
	body := map[string]any{"run_id": s.runID(), "confirmation_id": id, "approved": approved}
	if answers != nil {
		body["answers"] = answers
	}
	a.operation("confirm/"+s.RemoteID, "POST", sessionPath(s.RemoteID)+"/run/confirm", body, func(any) { a.loadSession(s) })
}
