package main

// Relaunch uses the normal quit path, including this guard. An update must not
// terminate a task or lose an editor buffer just because it finished downloading.
func (a *preview) quitBlockReason() string {
	for _, s := range a.Sessions {
		if s.Running || s.Sending || s.Pending {
			return "请先停止或等待运行中的任务，再退出或重启更新。"
		}
	}
	if a.Live != nil {
		if a.Live.configBusy() {
			return "正在保存配置，请稍后再退出或重启。"
		}
		for _, f := range a.Live.Workbench.Files {
			if f.Code != nil && f.Code.Buffer.Modified() {
				return "请先保存或取消文件编辑，再退出或重启更新。"
			}
		}
	}
	return ""
}
