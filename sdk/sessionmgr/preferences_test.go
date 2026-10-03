package sessionmgr

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreferencesPersistWithoutLosingWorkspace(t *testing.T) {
	m := NewManager(t.TempDir())
	id, _, err := m.Create(context.Background())
	require.NoError(t, err)
	require.NoError(t, m.SaveMeta(id, "/project", "coding"))
	title, yes := "我的任务", true
	require.NoError(t, m.UpdatePreferences(id, PreferencePatch{Title: &title, Pinned: &yes, Archived: &yes}))
	require.NoError(t, m.SaveMeta(id, "/project", "coding"))
	reloaded := NewManager(m.dataDir)
	list, err := reloaded.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, title, list[0].Title)
	require.True(t, list[0].Pinned)
	require.True(t, list[0].Archived)
	require.Equal(t, "/project", list[0].Workspace)
	no := false
	require.NoError(t, reloaded.UpdatePreferences(id, PreferencePatch{Archived: &no}))
	list, err = reloaded.List(context.Background())
	require.NoError(t, err)
	require.False(t, list[0].Archived)
	require.True(t, list[0].Pinned)
}

func TestPreferencesRejectUnsafeOrMissingSession(t *testing.T) {
	m := NewManager(t.TempDir())
	yes := true
	for _, id := range []string{"", "..", "../outside", "missing"} {
		require.Error(t, m.UpdatePreferences(id, PreferencePatch{Pinned: &yes}))
	}
	id, _, err := m.Create(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(m.SessionsDir(), id, "session.jsonl")))
	require.NoError(t, os.Remove(filepath.Join(m.SessionsDir(), id)))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(m.SessionsDir(), id)))
	require.Error(t, m.UpdatePreferences(id, PreferencePatch{Pinned: &yes}))
}
