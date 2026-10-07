package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/ui"
)

// promptConfirm 向用户展示操作描述并同步等待 y/n 裁决。
// 输入以 y/yes（任意大小写）视为同意，其余（含空行）视为拒绝。
//
// 拒绝时若用户输入了非空理由（y/n 之外的文字），作为 reason 回告 LLM。
func promptConfirm(out io.Writer, in io.Reader, description string) agent.ConfirmDecision {
	fmt.Fprintf(out, "\n%s⚠ 需要确认%s\n%s\n", ui.ColorYellow, ui.ColorReset, description)
	fmt.Fprintf(out, "%s是否执行？[y/N] (或输入拒绝理由): %s", ui.ColorYellow, ui.ColorReset)

	reader := bufio.NewReader(in)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return agent.ConfirmDecision{Approved: false, Reason: ""}
	}

	lower := strings.ToLower(line)
	if lower == "y" || lower == "yes" {
		return agent.ConfirmDecision{Approved: true}
	}
	// 非 y/yes 的一律视为拒绝；若输入了其他文字则作为理由回告 LLM
	return agent.ConfirmDecision{Approved: false, Reason: line}
}
