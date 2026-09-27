package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteFileTool_Basic(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "to_delete.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello\nworld\n"), 0o644))

	tool := NewDeleteFileTool()
	assert.Equal(t, "delete_file", tool.Name())

	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"file_path":"` + testFile + `"}`))
	require.NoError(t, err)

	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Successfully deleted")

	// Verify file is gone
	_, err = os.Stat(testFile)
	assert.True(t, os.IsNotExist(err))
}

func TestDeleteFileTool_WithReason(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "obsolete.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n"), 0o644))

	tool := NewDeleteFileTool()
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"file_path":"` + testFile + `","reason":"No longer needed"}`))
	require.NoError(t, err)

	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No longer needed")
}

func TestDeleteFileTool_WithBackup(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "backup_test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("important data"), 0o644))

	bm := NewBackupManager()
	tool := NewDeleteFileTool(WithDeleteFileBackupManager(bm))
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"file_path":"` + testFile + `"}`))
	require.NoError(t, err)

	result, err := tool.Execute(ctx, validated, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "preserved in backup")

	// Verify backup exists
	assert.True(t, bm.HasBackup(testFile))

	// Restore from backup
	err = bm.Restore(testFile)
	require.NoError(t, err)

	// Verify file is restored
	data, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, "important data", string(data))
}

func TestDeleteFileTool_NonExistent(t *testing.T) {
	tool := NewDeleteFileTool()
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"file_path":"/tmp/nonexistent_file_for_test.txt"}`))
	require.NoError(t, err)

	_, err = tool.Execute(ctx, validated, nil)
	assert.Error(t, err)
}

func TestDeleteFileTool_Directory(t *testing.T) {
	dir := t.TempDir()
	tool := NewDeleteFileTool()
	ctx := context.Background()
	validated, err := tool.Validate([]byte(`{"file_path":"` + dir + `"}`))
	require.NoError(t, err)

	result, err := tool.Execute(ctx, validated, nil)
	assert.Error(t, err)
	assert.True(t, result.IsError)
}

func TestDeleteFileTool_RelativePath(t *testing.T) {
	tool := NewDeleteFileTool()
	_, err := tool.Validate([]byte(`{"file_path":"relative/path.txt"}`))
	assert.NoError(t, err) // Relative paths are resolved against the configured workspace.
}
