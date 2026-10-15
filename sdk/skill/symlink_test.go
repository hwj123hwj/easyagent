package skill

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedSkillLinksAndProjectPrecedence(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	project := filepath.Join(root, "project")
	personal := filepath.Join(root, "personal")
	require.NoError(t, os.MkdirAll(shared, 0755))
	require.NoError(t, os.MkdirAll(project, 0755))
	require.NoError(t, os.MkdirAll(personal, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(shared, "SKILL.md"), []byte("---\nname: shared-skill\ndescription: personal shared skill\n---\nInstructions."), 0644))
	require.NoError(t, os.Symlink(shared, filepath.Join(personal, "linked")))
	require.NoError(t, os.Symlink(personal, filepath.Join(personal, "cycle")))
	require.NoError(t, os.WriteFile(filepath.Join(project, "SKILL.md"), []byte("---\nname: shared-skill\ndescription: project override\n---\nProject instructions."), 0644))
	result := LoadFromDirs(personal)
	require.Len(t, result.Skills, 1)
	require.Equal(t, "shared-skill", result.Skills[0].Name)
	result = LoadFromDirs(project, personal)
	require.Len(t, result.Skills, 1)
	require.Equal(t, "project override", result.Skills[0].Description)
}
