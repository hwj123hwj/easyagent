package runtime

import (
	"github.com/hwj123hwj/easyagent/sdk/skill"
	"github.com/hwj123hwj/easyagent/sdk/util"
	"path/filepath"
)

// DefaultSkillDirs includes the shared personal source used by the gateway.
// Project definitions take precedence over personal definitions with the same name.
func DefaultSkillDirs(workspace string) []string {
	return []string{filepath.Join(workspace, ".agents", "skills"), filepath.Join(workspace, ".claude", "skills"), filepath.Join(util.HomeDir(), ".agents", "skills"), filepath.Join(util.HomeDir(), ".claude", "skills")}
}

// LoadedSkills is the catalog used to build this session's current system prompt.
func (s *AgentSession) LoadedSkills() []skill.Skill {
	s.skillsMu.RLock()
	defer s.skillsMu.RUnlock()
	return append([]skill.Skill(nil), s.loadedSkills...)
}
