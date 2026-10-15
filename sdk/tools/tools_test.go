package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashTool_Execute(t *testing.T) {
	tool := NewBashTool()
	assert.Equal(t, "bash", tool.Name())

	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"command":"echo hello"}`))
	require.NoError(t, err)
	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", result.Content)
	assert.False(t, result.IsError)
}

func TestBashTool_Validate(t *testing.T) {
	tool := NewBashTool()

	// 有效参数
	validated, err := tool.Validate([]byte(`{"command":"ls"}`))
	require.NoError(t, err)
	assert.NotNil(t, validated)

	// 缺少 command
	_, err = tool.Validate([]byte(`{}`))
	assert.Error(t, err)

	// 无效 JSON
	_, err = tool.Validate([]byte(`invalid`))
	assert.Error(t, err)
}

func TestBashTool_CommandFailure(t *testing.T) {
	tool := NewBashTool()
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"command":"false"}`))
	require.NoError(t, err)
	result, err := tool.Execute(ctx, validated, nil)
	// false command exits with 1, CombinedOutput returns err
	assert.Error(t, err)
	// 但 result.Content 仍然应该有输出
	_ = result
}

func TestReadTool_Execute(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("line1\nline2\nline3\n"), 0o644))

	tool := NewReadTool()
	assert.Equal(t, "read", tool.Name())

	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"path":"` + testFile + `"}`))
	require.NoError(t, err)
	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "line1")
	assert.Contains(t, result.Content, "line3")
}

func TestReadTool_WithOffset(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644))

	tool := NewReadTool()
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"path":"` + testFile + `","offset":2,"limit":2}`))
	require.NoError(t, err)
	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "line3")
	assert.Contains(t, result.Content, "line4")
	assert.NotContains(t, result.Content, "line1")
	assert.NotContains(t, result.Content, "line5")
}

func TestWriteTool_Execute(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "output.txt")

	tool := NewWriteTool()
	assert.Equal(t, "write", tool.Name())

	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"path":"` + testFile + `","content":"hello world"}`))
	require.NoError(t, err)
	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "written")

	// 验证文件内容
	data, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestWriteTool_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "sub", "dir", "output.txt")

	tool := NewWriteTool()
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"path":"` + testFile + `","content":"nested"}`))
	require.NoError(t, err)
	_, err = tool.Execute(ctx, validated, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, "nested", string(data))
}

func TestBashTool_TimeoutPreservesOutput(t *testing.T) {
	tool := NewBashTool()
	result, err := tool.Execute(context.Background(), []byte(`{"command":"echo partial; sleep 10", "timeout":1}`), nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "partial")
	assert.Contains(t, result.Content, "timed out after 1s")
}
func TestBashTool_ParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	result, err := NewBashTool().Execute(ctx, []byte(`{"command":"echo partial; sleep 10", "timeout":30}`), nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "canceled")
	assert.Contains(t, result.Content, "partial")
}
