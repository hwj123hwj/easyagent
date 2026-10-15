package main

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *preview) refreshRunResults(s *session) {
	if a.Live.Client == nil || s.RemoteID == "" {
		return
	}
	a.resource("run-results/"+s.RemoteID, sessionPath(s.RemoteID)+"/run-files")
}

type aggregateFile struct {
	Path, RunID    string
	File           map[string]any
	Before, After  string
	Known, Partial bool
}

func workspaceRuns(runs []any, workspace string) []map[string]any {
	var out []map[string]any
	norm := func(path string) string { return strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/") }
	for _, raw := range runs {
		run := object(raw)
		if !boolean(run["complete"]) || norm(str(run["workspace"])) != norm(workspace) {
			continue
		}
		copy := map[string]any{}
		for k, v := range run {
			copy[k] = v
		}
		var files []any
		incomplete := boolean(run["incomplete"])
		for _, reason := range list(run["skipped"]) {
			if str(reason) == "扫描达到上限" || str(reason) == "快照达到文件或总大小上限" {
				incomplete = true
			}
		}
		for _, file := range list(run["files"]) {
			if !incomplete || str(object(file)["kind"]) == "modified" {
				files = append(files, file)
			}
		}
		copy["files"], copy["incomplete"] = files, incomplete
		if len(files) > 0 {
			out = append(out, copy)
		}
	}
	return out[max(0, len(out)-20):]
}
func aggregateRunFiles(runs []map[string]any, details map[string]any) []aggregateFile {
	var files []aggregateFile
	positions := map[string]int{}
	for _, run := range runs {
		id := str(run["run_id"])
		full, known := details[id]
		items := list(run["files"])
		if known {
			items = list(object(full)["files"])
		}
		for _, raw := range items {
			item := object(raw)
			path := str(item["path"])
			if boolean(run["incomplete"]) && str(item["kind"]) != "modified" {
				continue
			}
			at, exists := positions[path]
			current := aggregateFile{Path: path, RunID: id, File: item, Before: str(item["before"]), After: str(item["after"]), Known: known, Partial: boolean(item["binary"])}
			if exists {
				old := files[at]
				continuous := old.Known && current.Known && old.After == current.Before && str(old.File["attribution"]) == str(current.File["attribution"])
				current.Partial = current.Partial || old.Partial || !continuous
				if continuous {
					current.Before = old.Before
				}
				files[at] = current
			} else {
				positions[path] = len(files)
				files = append(files, current)
			}
		}
	}
	return files
}
func diffLineCounts(before, after string) (added, removed int, known bool) {
	split := func(text string) []string {
		if text == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	old, next := split(before), split(after)
	for len(old) > 0 && len(next) > 0 && old[0] == next[0] {
		old, next = old[1:], next[1:]
	}
	for len(old) > 0 && len(next) > 0 && old[len(old)-1] == next[len(next)-1] {
		old, next = old[:len(old)-1], next[:len(next)-1]
	}
	if len(old)*len(next) > 2000000 {
		return 0, 0, false
	}
	row := make([]int, len(next)+1)
	for _, line := range old {
		previous := 0
		for j, value := range next {
			above := row[j+1]
			if line == value {
				row[j+1] = previous + 1
			} else {
				row[j+1] = max(row[j], row[j+1])
			}
			previous = above
		}
	}
	lcs := row[len(next)]
	return len(next) - lcs, len(old) - lcs, true
}
func (a *preview) openRunReview(s *session, run, path string) {
	if a.current() != s {
		a.selectSession(s.ID)
	}
	a.openResource("review")
	a.selectReviewRun(run)
	a.Live.Workbench.PendingReviewPath = path
}
func (a *preview) runResults(c *ui.Context, s *session) {
	if s.RemoteID == "" {
		return
	}
	l := a.Live
	key := "run-results/" + s.RemoteID
	if err := l.ResourceErrors[key]; err != "" {
		ui.Row(c).Gap(8).Children(func() {
			a.note(c, "文件变更加载失败")
			a.action(c, "重试变更").OnClick(func() { a.refreshRunResults(s) })
		})
		return
	}
	runs := workspaceRuns(list(l.Data[key]), s.Workspace)
	if len(runs) == 0 {
		return
	}
	for _, source := range []string{"file_tool", "observed"} {
		details := map[string]any{}
		for _, run := range runs {
			id := str(run["run_id"])
			detailKey := "run-result-detail/" + s.RemoteID + "/" + id
			if value, ok := l.Data[detailKey]; ok {
				details[id] = value
			}
		}
		var files []aggregateFile
		for _, file := range aggregateRunFiles(runs, details) {
			kind := "observed"
			if str(file.File["attribution"]) == "file_tool" {
				kind = "file_tool"
			}
			if kind == source {
				files = append(files, file)
			}
		}
		if len(files) == 0 && source == "observed" {
			continue
		}
		root := ui.Column(c.Key("file-summary/"+s.RemoteID+source)).Background(c.Theme().Surface).Gap(8).Padding(12).Border(1, c.Theme().Border).Radius(8).MinWidth(0)
		open := ui.Local(root, "open", func() bool { return source == "file_tool" })
		shown := ui.Local(root, "shown", func() int { return 6 })
		if *open {
			for _, run := range runs {
				id := str(run["run_id"])
				detailKey := "run-result-detail/" + s.RemoteID + "/" + id
				if l.Data[detailKey] == nil && !l.Loading[detailKey] && l.ResourceErrors[detailKey] == "" {
					a.resource(detailKey, sessionPath(s.RemoteID)+"/runs/"+pathSegment(id)+"/files")
				}
			}
		}
		title := fmt.Sprintf("文件工具累计修改 %d 个文件", len(files))
		if source == "observed" {
			title = fmt.Sprintf("其他工作区变化：%d 个文件", len(files))
		}
		addedTotal, removedTotal, partialTotal := 0, 0, false
		statsByPath := map[string]*nativeLineStats{}
		for _, file := range files {
			if !file.Known || file.Partial {
				partialTotal = true
				continue
			}
			stats := ui.Local(root, file.Path+"/stats", func() nativeLineStats { return nativeLineStats{} })
			hash := sha256.Sum256([]byte(file.Before + "\x00" + file.After))
			if hash != stats.Hash {
				stats.Hash = hash
				stats.Added, stats.Removed, stats.Known = diffLineCounts(file.Before, file.After)
			}
			statsByPath[file.Path] = stats
			if stats.Known {
				addedTotal += stats.Added
				removedTotal += stats.Removed
			} else {
				partialTotal = true
			}
		}
		root.Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				glyph := "chevron-right"
				if *open {
					glyph = "chevron-down"
				}
				flatButton(c, glyph, title).Justify(ui.Start).MinWidth(0).Grow(1).Label(title).Expanded(*open).OnClick(func() { *open = !*open })
				nativeChangeStats(c, addedTotal, removedTotal, partialTotal)
				a.action(c, "审查").Disabled(len(files) == 0).OnClick(func() { a.openRunReview(s, files[len(files)-1].RunID, "") })
			})
			if !*open {
				return
			}
			a.note(c, fmt.Sprintf("当前工作区最近 %d 个有变更的任务，按路径去重，属于历史记录。", len(runs)))
			if source == "observed" {
				a.note(c, "Bash、外部进程等来源未确认的变化单独展示，不计入文件工具修改数量。")
			}
			for _, file := range files[:min(len(files), *shown)] {
				ui.Row(c.Key(file.Path)).Gap(8).Children(func() {
					flatButton(c, "file", relativeWorkspace(s.Workspace, file.Path)).Justify(ui.Start).FontSize(12).Grow(1).MinWidth(0).Label("变更文件：" + file.Path).OnClick(func() { a.openRunReview(s, file.RunID, file.Path) })
					if stats := statsByPath[file.Path]; stats != nil && stats.Known {
						nativeChangeStats(c, stats.Added, stats.Removed, false)
					}

					if boolean(file.File["undone"]) {
						a.note(c, "已撤回")
					}
				})
			}
			if *shown < len(files) {
				a.action(c, fmt.Sprintf("显示全部 %d 个文件", len(files))).AlignSelf(ui.Start).OnClick(func() { *shown = len(files) })
			}
			partial := false
			for _, run := range runs {
				if boolean(run["incomplete"]) {
					partial = true
				}
			}
			if partial {
				ui.Text(c, "工作区快照不完整，统计可能遗漏实际改动。").FontSize(11).TextColor(c.Theme().Warning)
			}
			for _, run := range runs {
				detailKey := "run-result-detail/" + s.RemoteID + "/" + str(run["run_id"])
				if l.ResourceErrors[detailKey] != "" {
					a.action(c, "重试变更详情").AlignSelf(ui.Start).OnClick(func() { delete(l.ResourceErrors, detailKey); delete(l.Data, detailKey) })
					break
				}
			}
		})
	}
}

type nativeLineStats struct {
	Hash           [32]byte
	Added, Removed int
	Known          bool
}

func nativeChangeStats(c *ui.Context, added, removed int, partial bool) {
	if added > 0 {
		ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(11).TextColor(c.Theme().Accent)
	}
	if removed > 0 {
		ui.Text(c, fmt.Sprintf("−%d", removed)).FontSize(11).TextColor(c.Theme().Danger)
	}
	if partial {
		ui.Text(c, "部分").FontSize(10).TextColor(c.Theme().TextMuted).Tooltip("仅已统计的文本文件；部分文件无法计算行数")
	}
}
