package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Folder selection follows the selected host: the system picker belongs to
// this Mac, while a remote path is passed unchanged to its own server.
func (a *preview) openWorkspacePicker() {
	l := a.Live
	if !l.Connected || l.configBusy() || l.Loading["folder-picker"] || l.Loading["create-session"] {
		return
	}
	a.NewMenu = false
	if !a.Settings.host().Local {
		l.WorkspacePath, l.WorkspaceHost = "", l.Config.ActiveHost
		l.NewWorkspace = true
		return
	}
	choose := l.PickDirectory
	if choose == nil {
		choose = mygo.Dialog.Open
	}
	options := mygo.OpenDialogOptions{Parent: a.Window, Directory: true, ButtonLabel: "打开", Title: "选择项目文件夹"}
	a.run("folder-picker", func(context.Context, *backendClient) (any, error) { return choose(options) }, func(v any) {
		paths, _ := v.([]string)
		if len(paths) == 0 || paths[0] == "" {
			l.FolderButton.Focus()
			return
		}
		a.createLiveSessionIn("", paths[0], "")
	})
}

func absoluteWorkspace(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/") || len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
}

func (a *preview) selectRemoteWorkspace() {
	l := a.Live
	path := strings.TrimSpace(l.WorkspacePath)
	if !l.NewWorkspace || l.WorkspaceHost != l.Config.ActiveHost || !absoluteWorkspace(path) || l.configBusy() || l.Loading["create-session"] {
		return
	}
	l.NewWorkspace = false
	a.createLiveSessionIn("", path, "")
}

func (a *preview) workspacePathDialog(c *ui.Context) {
	l := a.Live
	if l.NewWorkspace && l.WorkspaceHost != l.Config.ActiveHost {
		l.NewWorkspace = false
	}
	panel := ui.DialogBase(c, &l.NewWorkspace, func(back, panel ui.Element) {
		w, h := c.Size()
		back.Background(ui.RGBA(0, 0, 0, .55))
		panel.Label("选择远程项目").Width(min(480, w-40)).MaxHeight(h-40).Padding(28).Radius(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Gap(0)
		ui.Text(c, "选择远程项目").Role(ui.RoleHeading).FontSize(20).FontWeight(500).Margin(0, 0, 12, 0)
		ui.Text(c, "填写 "+a.Settings.host().Name+" 上的工作区绝对路径。文件和工具会在该主机上运行。").FontSize(13).LineHeight(1.8).TextColor(c.Theme().TextMuted)
		ui.Column(c).Margin(24, 0).Gap(10).Children(func() {
			ui.Text(c, "工作区路径").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.TextInput(c, &l.WorkspacePath).Label("工作区路径").Placeholder("/home/q/projects/my-project").AutoFocus().Padding(12).Radius(8).FontSize(13).Background(c.Theme().Background).Border(1, c.Theme().Border).OnSubmit(a.selectRemoteWorkspace)
		})
		valid := absoluteWorkspace(l.WorkspacePath)
		if strings.TrimSpace(l.WorkspacePath) != "" && !valid {
			ui.Text(c, "请填写绝对路径").Role(ui.RoleStatus).FontSize(12).TextColor(c.Theme().Danger).Margin(0, 0, 12, 0)
		}
		ui.Row(c).Justify(ui.End).Gap(12).Children(func() {
			a.action(c, "取消").Padding(8, 14).OnClick(func() { l.NewWorkspace = false; l.FolderButton.Focus() })
			a.primary(c, "选择项目").Padding(8, 14).Disabled(!valid || l.configBusy() || l.Loading["create-session"]).OnClick(a.selectRemoteWorkspace)
		})
	})
	if panel.Valid() && panel.Dismissed() {
		l.FolderButton.Focus()
	}
}
