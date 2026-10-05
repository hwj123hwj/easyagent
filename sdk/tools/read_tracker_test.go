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

func TestReadTracker_StaleProtection_EditTool(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "code.go")
	initialContent := "func main() {\n\tprintln(\"v1\")\n}\n"
	require.NoError(t, os.WriteFile(filePath, []byte(initialContent), 0o644))

	tracker := NewReadTracker()
	readTool := NewReadTool(WithReadTracker(tracker))
	editTool := NewEditTool(WithEditReadTracker(tracker))

	ctx := context.Background()

	// 1. Read file through ReadTool
	valRead, err := readTool.Validate([]byte(`{"path":"` + filePath + `"}`))
	require.NoError(t, err)
	resRead, err := readTool.Execute(ctx, valRead, nil)
	require.NoError(t, err)
	assert.Contains(t, resRead.Content, "v1")

	// 2. User/external editor modifies the file
	externalContent := "func main() {\n\tprintln(\"external change\")\n}\n"
	require.NoError(t, os.WriteFile(filePath, []byte(externalContent), 0o644))

	// 3. EditTool attempts to edit based on stale read
	valEdit, err := editTool.Validate([]byte(`{"path":"` + filePath + `","old_string":"v1","new_string":"v2"}`))
	require.NoError(t, err)
	resEdit, err := editTool.Execute(ctx, valEdit, nil)
	assert.Error(t, err)
	assert.True(t, resEdit.IsError)
	assert.Contains(t, resEdit.Content, "modified externally since last read")
	assert.Contains(t, resEdit.Content, "re-read the file before editing")

	// 4. File on disk was NOT overwritten by the stale edit
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, externalContent, string(data))

	// 5. Re-read the file
	resRead2, err := readTool.Execute(ctx, valRead, nil)
	require.NoError(t, err)
	assert.Contains(t, resRead2.Content, "external change")

	// 6. Now edit succeeds
	valEdit2, err := editTool.Validate([]byte(`{"path":"` + filePath + `","old_string":"external change","new_string":"v3"}`))
	require.NoError(t, err)
	resEdit2, err := editTool.Execute(ctx, valEdit2, nil)
	require.NoError(t, err)
	assert.False(t, resEdit2.IsError)
	assert.Contains(t, resEdit2.Content, "edited")

	data2, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(data2), "v3")
}

func TestReadTracker_StaleProtection_WriteTool(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "doc.md")
	initialContent := "# Header\nInitial content\n"
	require.NoError(t, os.WriteFile(filePath, []byte(initialContent), 0o644))

	tracker := NewReadTracker()
	readTool := NewReadTool(WithReadTracker(tracker))
	writeTool := NewWriteTool(WithWriteReadTracker(tracker))

	ctx := context.Background()

	// 1. Read file through ReadTool
	valRead, err := readTool.Validate([]byte(`{"path":"` + filePath + `"}`))
	require.NoError(t, err)
	_, err = readTool.Execute(ctx, valRead, nil)
	require.NoError(t, err)

	// 2. External modification
	require.NoError(t, os.WriteFile(filePath, []byte("# Header\nUser edited content\n"), 0o644))

	// 3. WriteTool attempts to overwrite
	valWrite, err := writeTool.Validate([]byte(`{"path":"` + filePath + `","content":"overwritten"}`))
	require.NoError(t, err)
	resWrite, err := writeTool.Execute(ctx, valWrite, nil)
	assert.Error(t, err)
	assert.True(t, resWrite.IsError)
	assert.Contains(t, resWrite.Content, "modified externally since last read")

	// 4. Content preserved
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "# Header\nUser edited content\n", string(data))
}

func TestEditTool_ToleranceAndDiagnostic(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("line 1\nline 2\nline 3\n"), 0o644))

	tool := NewEditTool()
	ctx := context.Background()

	// 1. Line numbers mistakenly copied from read tool: "     2\tline 2"
	val1, err := tool.Validate([]byte(`{"path":"` + filePath + `","old_string":"     2\tline 2","new_string":"line two"}`))
	require.NoError(t, err)
	res1, err := tool.Execute(ctx, val1, nil)
	require.NoError(t, err)
	assert.False(t, res1.IsError)

	data1, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "line 1\nline two\nline 3\n", string(data1))

	// 2. Trailing newline tolerance
	val2, err := tool.Validate([]byte(`{"path":"` + filePath + `","old_string":"line two\n\n","new_string":"line 2 updated"}`))
	require.NoError(t, err)
	res2, err := tool.Execute(ctx, val2, nil)
	require.NoError(t, err)
	assert.False(t, res2.IsError)

	// 3. Informative error diagnostic when old_string does not match
	val3, err := tool.Validate([]byte(`{"path":"` + filePath + `","old_string":"line 2 typo","new_string":"bar"}`))
	require.NoError(t, err)
	res3, err := tool.Execute(ctx, val3, nil)
	assert.Error(t, err)
	assert.True(t, res3.IsError)
	assert.Contains(t, res3.Content, "old_string not found in file")
	assert.Contains(t, res3.Content, "Nearest line match")
}

func TestReadTracker_Invalidate(t *testing.T) {
	tracker := NewReadTracker()
	tracker.Record("/test/file", []byte("data"), time.Now())

	stale, _, _ := tracker.CheckStale("/test/file", []byte("new-data"), time.Now())
	assert.True(t, stale)

	tracker.Invalidate("/test/file")
	staleAfter, _, _ := tracker.CheckStale("/test/file", []byte("new-data"), time.Now())
	assert.False(t, staleAfter)
}
