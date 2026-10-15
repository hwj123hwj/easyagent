package skillmarket

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/skill"
)

func mkDirAllForTest(t *testing.T, dir string) error {
	t.Helper()
	return os.MkdirAll(dir, 0o755)
}

func writeFileForTest(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestInstalledSkillDiscoverableByLoader is the end-to-end guarantee: a skill
// installed by this package is found by sdk/skill.LoadFromDirs on the
// personal skills root, exactly how runtime.DefaultSkillDirs mounts it.
func TestInstalledSkillDiscoverableByLoader(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	if _, err := client.Install(t.Context(), 1); err != nil {
		t.Fatalf("Install: %v", err)
	}

	result := skill.LoadFromDirs(client.SkillsDir())
	if len(result.Skills) == 0 {
		t.Fatalf("loader found no skills; diagnostics: %+v", result.Diagnostics)
	}
	found := false
	for _, s := range result.Skills {
		if s.Name == "hello-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("installed skill not discovered; got %+v", result.Skills)
	}

	// And after uninstall it disappears again.
	if err := client.Uninstall("hello-skill"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	result = skill.LoadFromDirs(client.SkillsDir())
	for _, s := range result.Skills {
		if s.Name == "hello-skill" {
			t.Fatal("skill still discoverable after uninstall")
		}
	}
}

// TestInstallDoesNotShadowHandWrittenSkill verifies that a hand-written skill
// with a different name remains discoverable after a marketplace install.
func TestInstallDoesNotShadowHandWrittenSkill(t *testing.T) {
	store := newFakeStore(t)
	client := newTestClient(t, store)

	handDir := filepath.Join(client.SkillsDir(), "my-own-skill")
	if err := mkDirAllForTest(t, handDir); err != nil {
		t.Fatal(err)
	}
	if err := writeFileForTest(t, filepath.Join(handDir, "SKILL.md"),
		"---\nname: my-own-skill\ndescription: hand written\n---\n\n# Own\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Install(t.Context(), 1); err != nil {
		t.Fatalf("Install: %v", err)
	}

	result := skill.LoadFromDirs(client.SkillsDir())
	names := map[string]bool{}
	for _, s := range result.Skills {
		names[s.Name] = true
	}
	if !names["my-own-skill"] || !names["hello-skill"] {
		t.Fatalf("expected both skills discoverable, got %v", names)
	}
}
