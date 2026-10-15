package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (a *preview) bindWorkbench() {
	l, w, s := a.Live, &a.Live.Workbench, a.current()
	scope := l.Config.ActiveHost + "\x00" + s.RemoteID + "\x00" + s.Workspace
	if w.Scope == scope {
		return
	}
	if w.Scope != "" {
		for _, key := range []string{"review", "run-files", "context", "profile", "kb", "kb-read", "kb-stats", "kb-health", "kb-tags", "kb-categories", "workflows", "static-workflows", "workflow-detail"} {
			l.Versions[key]++
			delete(l.Data, key)
			delete(l.ResourceErrors, key)
			l.Loading[key] = false
		}
	}
	w.Scope = scope
	w.SelectedRun = ""
	w.SelectedChange = ""
	w.PendingReviewPath = ""
	w.Diff = nil
	w.CompactInstructions = ""
	w.KBPath = ""
}
func (a *preview) selectReviewRun(id string) {
	w := &a.Live.Workbench
	w.SelectedRun = id
	w.SelectedChange = ""
	w.PendingReviewPath = ""
	w.Diff = nil
	a.resource("run-files", sessionPath(a.current().RemoteID)+"/runs/"+pathSegment(id)+"/files")
}
func (a *preview) selectReviewFile(file map[string]any) {
	w := &a.Live.Workbench
	w.SelectedChange = str(file["path"])
	w.Diff = &editor.DiffState{Old: editor.NewBuffer(str(file["before"])), New: editor.NewBuffer(str(file["after"])), Wrap: true}
}
func (a *preview) reviewWorkbench(c *ui.Context) {
	l, w, s := a.Live, &a.Live.Workbench, a.current()
	if a.resourceFeedback(c, "review") {
		return
	}
	runs := list(l.Data["review"])
	choices := []string{}
	byID := map[string]map[string]any{}
	for _, raw := range runs {
		run := object(raw)
		if len(list(run["files"])) == 0 {
			continue
		}
		id := str(run["run_id"])
		choices = append(choices, id)
		byID[id] = run
	}
	if len(choices) == 0 {
		ui.Column(c).Grow(1).Padding(20).Children(func() { a.note(c, "暂无文件变更。任务修改文件后，可以在这里审查差异。") })
		return
	}
	if byID[w.SelectedRun] == nil {
		a.selectReviewRun(choices[len(choices)-1])
	}
	ui.Column(c).Padding(10, 12).Gap(8).Children(func() {
		ui.Text(c, "任务快照").FontSize(11).TextColor(c.Theme().TextMuted)
		control := a.selectControl(c, "选择任务快照", &w.SelectedRun, choices)
		if control.Changed() {
			a.selectReviewRun(w.SelectedRun)
		}
	})
	if a.resourceFeedback(c, "run-files") {
		return
	}
	set := object(l.Data["run-files"])
	if str(set["run_id"]) != "" && str(set["run_id"]) != w.SelectedRun {
		return
	}
	files := list(set["files"])
	ui.Scroll(c).MaxHeight(230).MinHeight(80).Padding(0, 12).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			if boolean(set["incomplete"]) {
				ui.Text(c, "工作区快照不完整，统计可能遗漏实际改动。").FontSize(11).TextColor(c.Theme().Warning)
			}
			for _, raw := range files {
				file := object(raw)
				path := str(file["path"])
				ui.Row(c.Key(path)).Gap(6).Children(func() {
					selectedButton(c, ui.ButtonBase(c).Label("审查文件："+path).Grow(1).MinWidth(0).Justify(ui.Start).Padding(7, 8).Radius(6), w.SelectedChange == path).OnClick(func() { a.selectReviewFile(file) }).Children(func() {
						icon(c, "file")
						ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
							ui.Text(c, portableBase(path)).FontSize(12).SingleLine()
							label := str(file["kind"])
							if translated := map[string]string{"modified": "修改", "added": "新增", "deleted": "删除"}[label]; translated != "" {
								label = translated
							}
							if boolean(file["undone"]) {
								label = "已撤回"
							} else if str(file["attribution"]) == "file_tool" {
								label += " · 文件工具修改"
							} else {
								label += " · 来源未确认"
							}
							ui.Text(c, label).FontSize(10).TextColor(c.Theme().TextMuted)
						})
					})
					if boolean(file["can_undo"]) {
						iconButton(c, "undo", "撤销文件修改："+path).Size(26, 28).Disabled(l.Loading["undo"]).OnClick(func() {
							runID, sid := w.SelectedRun, s.RemoteID
							l.Confirm = &nativeConfirmation{"撤销这项文件修改？", path + "；文件在之后有变化时会拒绝覆盖。", func() {
								a.operation("undo", "POST", sessionPath(sid)+"/runs/"+pathSegment(runID)+"/undo", map[string]any{"path": path, "after_hash": file["version"]}, func(any) { a.selectReviewRun(runID); a.resource("review", sessionPath(sid)+"/run-files") })
							}}
						})
					}
				})
			}
		})
	})
	if w.PendingReviewPath != "" {
		for _, raw := range files {
			if file := object(raw); str(file["path"]) == w.PendingReviewPath {
				a.selectReviewFile(file)
				w.PendingReviewPath = ""
				break
			}
		}
	}
	if w.SelectedChange == "" && len(files) > 0 {
		a.selectReviewFile(object(files[0]))
	}
	var selected map[string]any
	for _, raw := range files {
		if file := object(raw); str(file["path"]) == w.SelectedChange {
			selected = file
			break
		}
	}
	if selected != nil {
		ui.Row(c).Padding(8, 12).Children(func() {
			ui.Text(c, w.SelectedChange).FontSize(11).SingleLine().MinWidth(0)
			ui.Spacer(c)
			if conflict := str(selected["conflict"]); conflict != "" {
				ui.Text(c, conflict).FontSize(11).TextColor(c.Theme().Warning)
			}
		})
		if boolean(selected["binary"]) {
			ui.Column(c).Grow(1).Padding(14).Children(func() {
				a.note(c, "二进制文件 · "+marketBytes(selected["size"]))
				a.action(c, "查看文件").AlignSelf(ui.Start).OnClick(func() { a.openResource("files"); a.openNativeFile(w.SelectedChange) })
			})
			return
		}
	}
	if w.Diff != nil {
		editor.DiffView(c, w.Diff).Grow(1).FillWidth()
	} else {
		ui.Text(c, fmt.Sprintf("%d 个文件", len(files))).Padding(14)
	}
}
