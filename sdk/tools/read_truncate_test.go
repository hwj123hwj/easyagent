package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeRead(t *testing.T, tool *ReadTool, params string) string {
	t.Helper()
	validated, err := tool.Validate([]byte(params))
	require.NoError(t, err)
	result, err := tool.Execute(context.Background(), validated, nil)
	require.NoError(t, err)
	return result.Content
}

// TestReadTool_LineBoundaryTruncation：字符上限在行边界生效，模型不会看到半行，
// 且续读提示给出精确的下一 offset。
func TestReadTool_LineBoundaryTruncation(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "lines.txt")

	const lineCount = 200
	lines := make([]string, lineCount)
	for i := 1; i <= lineCount; i++ {
		lines[i-1] = fmt.Sprintf("line-%03d %s", i, strings.Repeat("x", 40))
	}
	// 不写尾随换行，避免 split 出的尾随空行计入 totalLines（既有语义）
	require.NoError(t, os.WriteFile(testFile, []byte(strings.Join(lines, "\n")), 0o644))

	tool := NewReadTool(WithReadMaxOutputLen(1200))
	content := executeRead(t, tool, `{"path":"`+testFile+`"}`)

	// 每一行都保持完整格式（行号 + tab 开头），没有半行
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if strings.HasPrefix(line, "...") || line == "" {
			continue
		}
		assert.Regexp(t, `^\s*\d+\t`, line, "输出被切在半行中间: %q", line)
	}

	// 续读提示包含精确 offset：showing lines 1-N → offset=N
	m := regexp.MustCompile(`showing lines 1-(\d+) of (\d+); continue with offset=(\d+)`).FindStringSubmatch(content)
	require.NotNil(t, m, "应包含行边界截断的续读提示:\n%s", content)
	shown, _ := strconv.Atoi(m[1])
	total, _ := strconv.Atoi(m[2])
	nextOffset, _ := strconv.Atoi(m[3])
	assert.Equal(t, nextOffset, shown, "offset 应指向第一个未展示的行")
	assert.Equal(t, lineCount, total)

	// 续读 offset 能拿到被截掉的第一行
	next := executeRead(t, tool, fmt.Sprintf(`{"path":"%s","offset":%d}`, testFile, nextOffset))
	assert.Contains(t, next, fmt.Sprintf("line-%03d", nextOffset+1))
}

// TestReadTool_LongSingleLineHardCut：单行超限时 rune 安全硬切，不产生非法 UTF-8。
func TestReadTool_LongSingleLineHardCut(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "minified.txt")

	longLine := strings.Repeat("中文x", 1000) // 4000 rune / 8000 byte，单行超限
	require.NoError(t, os.WriteFile(testFile, []byte(longLine+"\nsecond line\n"), 0o644))

	tool := NewReadTool(WithReadMaxOutputLen(1000))
	content := executeRead(t, tool, `{"path":"`+testFile+`"}`)

	assert.True(t, utf8.ValidString(content), "截断不应产生非法 UTF-8")
	assert.Less(t, len(content), 1000+len("\n... output capped at 1000 characters: showing lines 1-1 of 2; continue with offset=1 (and a smaller limit when lines are very long)\n")+1)
	assert.Contains(t, content, "continue with offset=1")
	assert.NotContains(t, content, "second line")
}

// TestReadTool_WithinCapKeepsRemainingHint：未触及字符上限时保留原有的行数剩余提示。
func TestReadTool_WithinCapKeepsRemainingHint(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "small.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("a\nb\nc\nd\ne\n"), 0o644))

	tool := NewReadTool(WithReadMaxOutputLen(10000))
	// 文件以换行结尾：split 出的尾随空行计入 totalLines（既有语义），limit=2 后剩 4 行
	content := executeRead(t, tool, `{"path":"`+testFile+`","limit":2}`)

	assert.Contains(t, content, "... 4 more lines (use offset=2 to read further)")
	assert.NotContains(t, content, "output capped")
}

func TestTruncateByteCap(t *testing.T) {
	assert.Equal(t, "hello", truncateByteCap("hello", 10))
	assert.Equal(t, "hel", truncateByteCap("hello", 3))
	assert.Equal(t, "", truncateByteCap("hello", 0))
	assert.Equal(t, "", truncateByteCap("hello", -1))

	// 中文三字节一枚：切在多字节中间时回退到 rune 边界
	got := truncateByteCap("中文中文", 7) // 7 字节会切开第三枚，回退到 6 字节
	assert.Equal(t, "中文", got)
	assert.True(t, utf8.ValidString(got))
}
