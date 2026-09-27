package operations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalBashOperations_Run(t *testing.T) {
	ops := LocalBashOperations{}
	ctx := context.Background()

	t.Run("successful command", func(t *testing.T) {
		result, err := ops.Run(ctx, RunRequest{Command: "echo hello"})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Equal(t, "hello\n", string(result.Output))
	})

	t.Run("failed command", func(t *testing.T) {
		result, err := ops.Run(ctx, RunRequest{Command: "false"})
		require.NoError(t, err) // Run itself doesn't return error for non-zero exit
		assert.NotEqual(t, 0, result.ExitCode)
	})

	t.Run("with workdir", func(t *testing.T) {
		dir := t.TempDir()
		result, err := ops.Run(ctx, RunRequest{Command: "pwd", WorkDir: dir})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Equal(t, dir+"\n", string(result.Output))
	})

	t.Run("with timeout", func(t *testing.T) {
		result, err := ops.Run(ctx, RunRequest{Command: "sleep 10", Timeout: 1})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.NotEqual(t, 0, result.ExitCode) // should timeout
	})
}

func TestLocalFileOperations_ReadWrite(t *testing.T) {
	ops := LocalFileOperations{}
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("write and read", func(t *testing.T) {
		path := filepath.Join(dir, "test.txt")
		data := []byte("hello world")

		err := ops.WriteFile(ctx, path, data, 0o644)
		require.NoError(t, err)

		got, err := ops.ReadFile(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("read nonexistent", func(t *testing.T) {
		_, err := ops.ReadFile(ctx, filepath.Join(dir, "nope.txt"))
		assert.Error(t, err)
	})
}

func TestLocalFileOperations_MkdirAll(t *testing.T) {
	ops := LocalFileOperations{}
	ctx := context.Background()
	dir := t.TempDir()

	nested := filepath.Join(dir, "a", "b", "c")
	err := ops.MkdirAll(ctx, nested, 0o755)
	require.NoError(t, err)

	info, err := os.Stat(nested)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLocalFileOperations_Stat(t *testing.T) {
	ops := LocalFileOperations{}
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(dir, "file.txt")
		require.NoError(t, os.WriteFile(path, []byte("content"), 0o644))

		info, err := ops.Stat(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "file.txt", info.Name)
		assert.False(t, info.IsDir)
		assert.Equal(t, int64(7), info.Size)
	})

	t.Run("directory", func(t *testing.T) {
		info, err := ops.Stat(ctx, dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir)
	})

	t.Run("nonexistent", func(t *testing.T) {
		_, err := ops.Stat(ctx, filepath.Join(dir, "nope"))
		assert.Error(t, err)
	})
}

func TestLocalFileOperations_ReadDir(t *testing.T) {
	ops := LocalFileOperations{}
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("y"), 0o644))

	entries, err := ops.ReadDir(ctx, dir)
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name] = true
	}
	assert.True(t, names["a.txt"])
	assert.True(t, names["b.go"])
	assert.True(t, names["subdir"])
	assert.Equal(t, 3, len(entries))
}

func TestLocalFileOperations_Walk(t *testing.T) {
	ops := LocalFileOperations{}
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("y"), 0o644))

	var paths []string
	err := ops.Walk(ctx, dir, func(path string, entry DirEntry, err error) error {
		require.NoError(t, err)
		paths = append(paths, path)
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 4, len(paths)) // root, root.txt, sub, sub/nested.txt
}

func TestNewLocalOperations(t *testing.T) {
	ops := NewLocalOperations()
	assert.NotNil(t, ops.Bash)
	assert.NotNil(t, ops.Files)

	_, ok := ops.Bash.(LocalBashOperations)
	assert.True(t, ok)

	_, ok = ops.Files.(LocalFileOperations)
	assert.True(t, ok)
}
