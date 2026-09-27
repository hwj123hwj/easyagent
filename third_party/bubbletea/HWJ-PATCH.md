# HWJ 本地补丁说明

本目录是 bubbletea v1.3.10 的本地副本（MIT，见 LICENSE），通过 go.mod
的 replace 指令使用。上游 v1 已冻结不再更新，故直接落地源码打补丁。

## 补丁内容（standard_renderer.go）

1. 包级 `SetCursorColumn(cols int)`：应用声明每帧渲染完后终端光标的停靠列。
2. flush() 尾部：光标停靠位置由固定「最后一行第 0 列」改为
   `CursorPosition(cursorColumn, len(newLines))`（altscreen）/
   `\r` + `CursorForward(cursorColumn)`（普通模式）。

## 为什么需要

输入法内联组词（预编辑串）渲染在终端光标当前位置。上游固定停第 0 列，
导致中文输入组词串叠在行首/状态栏上。easyagent 在 View() 里调用
`tea.SetCursorColumn(input.CursorColumn())` 让组词串落在光标处。

参考: easyagent PR #51（输入框挪帧尾）与本次补丁配套。

升级上游版本时需重新套用此补丁。

## 补丁 2（commands.go / tea.go）

`tea.Bell()`：响铃命令（BEL \a）。长任务完成且用户滚离底部时提醒。
不在上游 v1 中。
