package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBundledHelperWinsOverLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "easyagent-cua-helper")
	bundled := filepath.Join(dir, "cua-helper", "EasyAgent Computer Use.app", "Contents", "MacOS", "easyagent-cua-helper")
	require.NoError(t, os.MkdirAll(filepath.Dir(bundled), 0700))
	require.NoError(t, os.WriteFile(legacy, []byte("legacy"), 0755))
	require.NoError(t, os.WriteFile(bundled, []byte("native"), 0755))
	require.Equal(t, bundled, helperPathForExecutable(filepath.Join(dir, "easyagent")))
	require.NoError(t, os.Chmod(bundled, 0644))
	require.Equal(t, legacy, helperPathForExecutable(filepath.Join(dir, "easyagent")))
}
func TestScreenshotIncludesModelImageAndRejectsEscapes(t *testing.T) {
	tool := NewTool(t.TempDir())
	tool.ConfigureSession("screenshot-test")
	require.NoError(t, os.MkdirAll(tool.shotsDir, 0700))
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 3))))
	path := filepath.Join(tool.shotsDir, "shot.png")
	require.NoError(t, os.WriteFile(path, buffer.Bytes(), 0600))
	tool.helper = func(context.Context, string, any) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"path": path, "width": 2, "height": 3})
	}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"screenshot"}`), nil)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Images, 1)
	require.Equal(t, buffer.Bytes(), result.Images[0].Data)
	require.Empty(t, Registry.Owner())
	outside := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(outside, buffer.Bytes(), 0600))
	link := filepath.Join(tool.shotsDir, "link.png")
	require.NoError(t, os.Symlink(outside, link))
	for _, bad := range []string{outside, link} {
		path = bad
		result, err = tool.Execute(context.Background(), json.RawMessage(`{"action":"screenshot"}`), nil)
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Empty(t, result.Images)
		require.Empty(t, Registry.Owner())
	}
}
