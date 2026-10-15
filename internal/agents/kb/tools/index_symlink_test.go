package kbtools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// 回归：~/agent-lessons 是指向真实仓库的软链，而 filepath.Walk 不跟随
// 软链根目录——第二大脑面板因此永远扫到 0 条。GetIndex 必须先解析软链。
func TestGetIndexFollowsSymlinkedRepoRoot(t *testing.T) {
	real := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(real, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("knowledge/tech/card.md", "---\ntitle: 测试卡片\nsummary: 摘要\ncategory: tech\n---\n正文")
	write("README.md", "# repo")

	link := filepath.Join(t.TempDir(), "agent-lessons")
	require.NoError(t, os.Symlink(real, link))

	// 软链路径与真实路径必须扫出同样的条目
	viaLink, err := GetIndex(link)
	require.NoError(t, err)
	require.NotEmpty(t, viaLink.Entries, "软链根目录应能扫到条目")

	viaReal, err := GetIndex(real)
	require.NoError(t, err)
	require.Len(t, viaLink.Entries, len(viaReal.Entries))

	// 条目的 RelPath 应相对真实根计算，不含软链路径残留
	require.Equal(t, "knowledge/tech/card.md", viaLink.Entries[0].RelPath)

	// 缓存键按解析后的根：同一真实仓库的两种访问应命中同一份缓存
	if cachedIndex != nil && cachedIndex.Root == viaReal.Root {
		require.Equal(t, viaReal.Root, viaLink.Root)
	}
}

func TestResolveRepoRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(real, link))
	// macOS 上 /var 本身也是软链：两边都解析到底后比较
	resolvedReal, rerr := filepath.EvalSymlinks(real)
	require.NoError(t, rerr)
	require.Equal(t, resolvedReal, resolveRepoRoot(link))
	require.Equal(t, resolvedReal, resolveRepoRoot(real))
	// 不存在的路径原样返回（交给上层报错）
	require.Equal(t, "/nonexistent/path", resolveRepoRoot("/nonexistent/path"))
}
