package computer

import (
	"encoding/json"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/require"
)

func TestPolicyAskConfirmsEverything(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigurePolicy("ask", nil)

	// ask：普通批与高危批都需要确认
	normal := normalize(t, `{"action":"perform_action","actions":[{"type":"click","x":1,"y":2}]}`)
	desc, ok := tool.RequiresConfirmation(normal)
	require.True(t, ok)
	require.Contains(t, desc, "电脑控制")

	high := normalize(t, `{"action":"perform_action","actions":[{"type":"key","key":"cmd+q"}]}`)
	desc, ok = tool.RequiresConfirmation(high)
	require.True(t, ok)
	require.Contains(t, desc, "⚠️ 高风险")
}

func TestPolicyAutoSkipsNormalButNeverHighRisk(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigurePolicy("auto", []string{"com.apple.finder"})

	// auto：普通批免确认
	normal := normalize(t, `{"action":"perform_action","actions":[{"type":"click","x":1,"y":2},{"type":"key","key":"cmd+s"}]}`)
	_, ok := tool.RequiresConfirmation(normal)
	require.False(t, ok, "auto 策略下普通动作批应免确认")

	// auto：高危批仍然必确认——用户退出类操作永远人工把关
	high := normalize(t, `{"action":"perform_action","actions":[{"type":"key","key":"cmd+q"}]}`)
	desc, ok := tool.RequiresConfirmation(high)
	require.True(t, ok, "auto 策略下高危动作批仍必须确认")
	require.Contains(t, desc, "⚠️ 高风险")

	// 非法策略回退 ask
	tool2 := NewTool(t.TempDir())
	tool2.ConfigurePolicy("yolo", nil)
	_, ok = tool2.RequiresConfirmation(normal)
	require.True(t, ok, "非法策略必须回退 ask（全部确认）")
}

func TestApprovedAppsTrackedAndPersisted(t *testing.T) {
	dataDir := t.TempDir()
	tool := NewTool(dataDir)
	tool.ConfigurePolicy("auto", nil)

	// 模拟 helper 返回 focused_app 的记录路径
	tool.recordFocusedApp(json.RawMessage(`{"focused_app":"com.apple.finder"}`))
	tool.recordFocusedApp(json.RawMessage(`{"focused_app":"com.apple.finder"}`)) // 去重
	require.Equal(t, []string{"com.apple.finder"}, tool.ApprovedApps())

	// 重启恢复：LoadRuntimeOverrides 读回记录
	cfg := config.Config{}
	cfg.LoadRuntimeOverrides(dataDir)
	require.Equal(t, []string{"com.apple.finder"}, cfg.ApprovedApps)

	// recordFocusedApp 对空输出无害
	tool.recordFocusedApp(json.RawMessage(`{}`))
	require.Len(t, tool.ApprovedApps(), 1)
}

func TestClearAndCapApprovedApps(t *testing.T) {
	dataDir := t.TempDir()
	for i := 0; i < 55; i++ {
		require.NoError(t, config.RecordApprovedApp(dataDir, "com.app"+string(rune('a'+i%26))+string(rune('0'+i/26))))
	}
	cfg := config.Config{}
	cfg.LoadRuntimeOverrides(dataDir)
	require.LessOrEqual(t, len(cfg.ApprovedApps), 50, "approved apps 必须有上限防膨胀")

	require.NoError(t, config.ClearApprovedApps(dataDir))
	cfg2 := config.Config{}
	cfg2.LoadRuntimeOverrides(dataDir)
	require.Empty(t, cfg2.ApprovedApps)
}
