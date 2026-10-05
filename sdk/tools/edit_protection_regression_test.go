package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/stretchr/testify/require"
)

func executeProtectionTool(t *testing.T, tool agent.Tool, params any) error {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	validated, err := tool.Validate(raw)
	require.NoError(t, err)
	_, err = tool.Execute(context.Background(), validated, nil)
	return err
}

func TestEditTolerancePreservesMatchedSpan(t *testing.T) {
	for _, tc := range []struct{ name, content, old, want string }{
		{"copied label", "hello\n", "1\thello", "new\n"},
		{"trimmed newline", "hello\ntail", "\nhello\n\n", "new\ntail"},
		{"LF in CRLF file", "hello\r\nworld\r\ntail", "hello\nworld", "new\r\ntail"},
		{"CRLF in LF file", "hello\nworld\ntail", "hello\r\nworld", "new\ntail"},
		{"preserve indentation", "  hello\n  world\n", "1\t  hello\n2\t  world", "new\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, bulk := range []bool{false, true} {
				path := filepath.Join(t.TempDir(), "target")
				require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
				params := EditParams{Path: path, OldString: tc.old, NewString: "new"}
				if bulk {
					params = EditParams{Path: path, Edits: []EditEntry{{OldString: tc.old, NewString: "new"}}}
				}
				require.NotPanics(t, func() { require.NoError(t, executeProtectionTool(t, NewEditTool(), params)) })
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, tc.want, string(data))
			}
		})
	}
}

func TestEditToleranceUsesSameCandidateForCountingAndReplacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target")
	require.NoError(t, os.WriteFile(path, []byte("hello\nend\nhello"), 0o600))
	require.NoError(t, executeProtectionTool(t, NewEditTool(), EditParams{Path: path, OldString: "hello\r\n", NewString: "new\n", ReplaceAll: true}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new\nend\nhello", string(data))
}

func TestEditToleranceRejectsAmbiguousAndPlainNumericPrefixes(t *testing.T) {
	for _, old := range []string{"1\thello", "123 hello", "hello\n\n"} {
		path := filepath.Join(t.TempDir(), "target")
		original := "hello\nhello\n"
		require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
		require.Error(t, executeProtectionTool(t, NewEditTool(), EditParams{Path: path, OldString: old, NewString: "new"}))
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, original, string(data))
	}
	path := filepath.Join(t.TempDir(), "numeric")
	require.NoError(t, os.WriteFile(path, []byte("hello\n"), 0o600))
	require.Error(t, executeProtectionTool(t, NewEditTool(), EditParams{Path: path, OldString: "123 hello", NewString: "new"}))
}

func TestEditTolerancePreservesExactPriorityAndRejectsOverlap(t *testing.T) {
	actual, err := applyEdits("1\thello\nhello\n", []EditEntry{{OldString: "1\thello", NewString: "new"}})
	require.NoError(t, err)
	require.Equal(t, "new\nhello\n", actual)
	_, err = applyEdits("hello world\ntail", []EditEntry{{OldString: "1\thello world", NewString: "new"}, {OldString: "world\n\n", NewString: "other"}})
	require.ErrorContains(t, err, "overlapping")
	path := filepath.Join(t.TempDir(), "mixed")
	require.NoError(t, os.WriteFile(path, []byte("hello\nworld|hello\r\nworld"), 0o600))
	require.NoError(t, executeProtectionTool(t, NewEditTool(), EditParams{Path: path, OldString: "1\thello\n2\tworld", NewString: "new", ReplaceAll: true}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Exact LF matching has priority over newline tolerance.
	require.Equal(t, "new|hello\r\nworld", string(data))
}

func TestReadManyFilesSharesProtectionWithMultiEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	tracker := NewReadTracker()
	require.NoError(t, executeProtectionTool(t, NewReadManyFilesTool(WithReadManyFilesWorkspace(dir), WithReadManyFilesTracker(tracker)), map[string]any{"paths": []string{"target.txt"}}))
	require.NoError(t, os.WriteFile(path, []byte("old external"), 0o600))
	require.ErrorContains(t, executeProtectionTool(t, NewMultiEditTool(WithMultiEditReadTracker(tracker)), MultiEditParams{FilePath: path, Edits: []MultiEditSingleEntry{{OldString: "old", NewString: "new"}}}), "modified externally")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "old external", string(data))
}

func TestReadProtectionContinuesAfterSuccessfulMutations(t *testing.T) {
	for _, kind := range []string{"edit", "bulk", "write", "multiedit"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "target")
			require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
			tracker := NewReadTracker()
			tracker.Record(path, []byte("old"), time.Time{})
			var tool agent.Tool
			var params any
			switch kind {
			case "edit":
				tool = NewEditTool(WithEditReadTracker(tracker))
				params = EditParams{Path: path, OldString: "old", NewString: "new"}
			case "bulk":
				tool = NewEditTool(WithEditReadTracker(tracker))
				params = EditParams{Path: path, Edits: []EditEntry{{OldString: "old", NewString: "new"}}}
			case "write":
				tool = NewWriteTool(WithWriteReadTracker(tracker))
				params = WriteParams{Path: path, Content: "new"}
			case "multiedit":
				tool = NewMultiEditTool(WithMultiEditReadTracker(tracker))
				params = MultiEditParams{FilePath: path, Edits: []MultiEditSingleEntry{{OldString: "old", NewString: "new"}}}
			}
			require.NoError(t, executeProtectionTool(t, tool, params))
			// An agent can continue editing its own new content without re-reading.
			require.NoError(t, executeProtectionTool(t, NewWriteTool(WithWriteReadTracker(tracker)), WriteParams{Path: path, Content: "newer"}))
			require.NoError(t, os.WriteFile(path, []byte("external"), 0o600))
			require.Error(t, executeProtectionTool(t, NewWriteTool(WithWriteReadTracker(tracker)), WriteParams{Path: path, Content: "lost"}))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "external", string(data))
		})
	}
}

func TestReadProtectionRejectsRecreatingDeletedFile(t *testing.T) {
	for _, edit := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "target")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
		tracker := NewReadTracker()
		tracker.Record(path, []byte("old"), time.Time{})
		require.NoError(t, os.Remove(path))
		var tool agent.Tool = NewWriteTool(WithWriteReadTracker(tracker))
		var params any = WriteParams{Path: path, Content: "recreated"}
		if edit {
			tool = NewEditTool(WithEditReadTracker(tracker))
			params = EditParams{Path: path, NewString: "recreated"}
		}
		require.Error(t, executeProtectionTool(t, tool, params))
		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err))
	}
}
