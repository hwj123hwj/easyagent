package computer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func normalize(t *testing.T, raw string) json.RawMessage {
	t.Helper()
	normalized, err := validateActions(json.RawMessage(raw))
	require.NoError(t, err)
	return normalized
}

func TestValidateActionsHappy(t *testing.T) {
	raw := normalize(t, `{"actions":[
		{"type":"click","x":120,"y":80},
		{"type":"type","text":"hello 世界"},
		{"type":"key","key":"cmd+s"},
		{"type":"scroll","dy":-3},
		{"type":"wait","ms":500}
	]}`)
	var batch actionBatch
	require.NoError(t, json.Unmarshal(raw, &batch))
	require.Len(t, batch.Actions, 5)
}

func TestValidateActionsBounds(t *testing.T) {
	// click 缺坐标
	_, err := validateActions(json.RawMessage(`{"actions":[{"type":"click"}]}`))
	require.ErrorContains(t, err, "x/y")

	// 坐标越界
	_, err = validateActions(json.RawMessage(`{"actions":[{"type":"click","x":99999,"y":0}]}`))
	require.ErrorContains(t, err, "超出")

	// type 空文本
	_, err = validateActions(json.RawMessage(`{"actions":[{"type":"type"}]}`))
	require.ErrorContains(t, err, "text")

	// type 超长
	long := make([]byte, maxTypeChars+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err = validateActions(json.RawMessage(`{"actions":[{"type":"type","text":"` + string(long) + `"}]}`))
	require.ErrorContains(t, err, "超长")

	// 非法组合键
	_, err = validateActions(json.RawMessage(`{"actions":[{"type":"key","key":"hyper+space"}]}`))
	require.ErrorContains(t, err, "无法识别")

	// wait 超时
	_, err = validateActions(json.RawMessage(`{"actions":[{"type":"wait","ms":9000}]}`))
	require.ErrorContains(t, err, "0-5000")

	// scroll 缺方向量
	_, err = validateActions(json.RawMessage(`{"actions":[{"type":"scroll"}]}`))
	require.ErrorContains(t, err, "dx 或 dy")

	// 空批
	_, err = validateActions(json.RawMessage(`{"actions":[]}`))
	require.ErrorContains(t, err, "actions 数组")
}

func TestValidateActionsStepLimit(t *testing.T) {
	var sb = `{"actions":[`
	for i := 0; i <= maxActionSteps; i++ {
		if i > 0 {
			sb += ","
		}
		sb += `{"type":"wait","ms":10}`
	}
	sb += `]}`
	_, err := validateActions(json.RawMessage(sb))
	require.ErrorContains(t, err, "拆分")
}

func TestRequiresConfirmationOnlyForControl(t *testing.T) {
	tool := NewTool(t.TempDir())

	// 只读动作不需要确认
	_, ok := tool.RequiresConfirmation(json.RawMessage(`{"action":"screenshot"}`))
	require.False(t, ok)
	_, ok = tool.RequiresConfirmation(json.RawMessage(`{"action":"get_app_state"}`))
	require.False(t, ok)

	// 控制批需要确认，描述包含每一步
	desc, ok := tool.RequiresConfirmation(json.RawMessage(
		`{"action":"perform_action","actions":[{"type":"click","x":10,"y":20},{"type":"key","key":"cmd+s"}]}`))
	require.True(t, ok)
	require.Contains(t, desc, "电脑控制")
	require.Contains(t, desc, "左键点击 (10, 20)")
	require.Contains(t, desc, "按键 cmd+s")
	require.Contains(t, desc, "依次执行 2 个动作")
}

func TestHighRiskFlagging(t *testing.T) {
	tool := NewTool(t.TempDir())

	// cmd+q 高危 → 描述带 ⚠️
	desc, ok := tool.RequiresConfirmation(json.RawMessage(
		`{"action":"perform_action","actions":[{"type":"key","key":"cmd+q"}]}`))
	require.True(t, ok)
	require.Contains(t, desc, "⚠️ 高风险")

	// 普通按键不高危
	desc, _ = tool.RequiresConfirmation(json.RawMessage(
		`{"action":"perform_action","actions":[{"type":"key","key":"cmd+s"}]}`))
	require.NotContains(t, desc, "⚠️")
}
