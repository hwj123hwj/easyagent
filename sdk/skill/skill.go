package skill

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ─── 类型定义 ─────────────────────────────────────────────────────────────────

// Skill 表示一个从 SKILL.md 或 .md 文件加载的技能。
// 遵循 agentskills.io 格式规范。
type Skill struct {
	// Name 技能名称，必须唯一，用于查找和调用。
	Name string `json:"name"`
	// Description 简短描述，用于系统提示中展示给 LLM。
	Description string `json:"description"`
	// Content 技能的完整指令内容。
	Content string `json:"content"`
	// FilePath 技能文件的绝对路径。
	FilePath string `json:"file_path"`
	// BaseDir 技能文件所在目录，用于解析相对路径引用。
	BaseDir string `json:"base_dir"`
	// DisableModelInvocation 为 true 时，不在系统提示中列出，但仍可被应用显式调用。
	DisableModelInvocation bool `json:"disable_model_invocation,omitempty"`
}

// DiagnosticCode 诊断错误码。
type DiagnosticCode string

const (
	DiagFileInfoFailed DiagnosticCode = "file_info_failed"
	DiagListFailed     DiagnosticCode = "list_failed"
	DiagReadFailed     DiagnosticCode = "read_failed"
	DiagParseFailed    DiagnosticCode = "parse_failed"
	DiagInvalidName    DiagnosticCode = "invalid_name"
	DiagInvalidDesc    DiagnosticCode = "invalid_description"
)

// Diagnostic 加载过程中产生的非致命诊断信息。
type Diagnostic struct {
	Type    DiagnosticCode `json:"type"`
	Code    DiagnosticCode `json:"code"`
	Message string         `json:"message"`
	Path    string         `json:"path"`
}

// ─── 常量 ─────────────────────────────────────────────────────────────────────

const (
	maxNameLength        = 64
	maxDescriptionLength = 1024
	skillFileName        = "SKILL.md"
)

// 验证 name 的正则：小写字母、数字、连字符
var validNameRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// ─── 加载 ─────────────────────────────────────────────────────────────────────

// LoadResult 加载结果。
type LoadResult struct {
	Skills      []Skill
	Diagnostics []Diagnostic
}

// LoadFromDirs 从一个或多个目录加载技能。
// 遍历目录树，查找 SKILL.md 文件，解析 frontmatter，返回技能列表和诊断信息。
// 不存在的目录会被跳过。
func LoadFromDirs(dirs ...string) LoadResult {
	result := LoadResult{
		Skills:      make([]Skill, 0),
		Diagnostics: make([]Diagnostic, 0),
	}
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Code:    DiagFileInfoFailed,
				Message: fmt.Sprintf("cannot resolve path: %v", err),
				Path:    dir,
			})
			continue
		}

		info, err := os.Stat(absDir)
		if err != nil {
			if !os.IsNotExist(err) {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					Code:    DiagFileInfoFailed,
					Message: err.Error(),
					Path:    absDir,
				})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}

		r := loadFromDir(absDir, true, absDir)
		result.Skills = append(result.Skills, r.Skills...)
		result.Diagnostics = append(result.Diagnostics, r.Diagnostics...)
	}
	seen := map[string]bool{}
	unique := []Skill{}
	for _, s := range result.Skills {
		if !seen[s.Name] {
			seen[s.Name] = true
			unique = append(unique, s)
		}
	}
	result.Skills = unique
	return result
}

// loadFromDir 递归加载目录中的技能文件。
// includeRootFiles 为 true 时，根目录中的 .md 文件也会被加载为技能。
func loadFromDir(dir string, includeRootFiles bool, rootDir string) LoadResult {
	return loadDirectory(dir, includeRootFiles, rootDir, map[string]bool{})
}
func loadDirectory(dir string, includeRootFiles bool, rootDir string, visited map[string]bool) LoadResult {
	result := LoadResult{
		Skills:      make([]Skill, 0),
		Diagnostics: make([]Diagnostic, 0),
	}

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || visited[resolved] {
		return result
	}
	visited[resolved] = true
	entries, err := os.ReadDir(dir)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    DiagListFailed,
			Message: err.Error(),
			Path:    dir,
		})
		return result
	}

	// 优先查找 SKILL.md
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() != skillFileName {
			continue
		}
		skill, diags := parseSkillFile(filepath.Join(dir, entry.Name()))
		result.Diagnostics = append(result.Diagnostics, diags...)
		if skill != nil {
			result.Skills = append(result.Skills, *skill)
		}
		return result // 找到 SKILL.md 后不再处理其他文件
	}

	// 递归处理子目录
	ignorePatterns := loadIgnorePatterns(dir)
	ignore := newIgnoreMatcherWithPatterns(ignorePatterns)
	for _, entry := range entries {
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if !isDir {
			continue
		}
		name := entry.Name()
		if shouldIgnoreDir(name, ignore) {
			continue
		}
		sub := loadDirectory(filepath.Join(dir, name), true, rootDir, visited)
		result.Skills = append(result.Skills, sub.Skills...)
		result.Diagnostics = append(result.Diagnostics, sub.Diagnostics...)
	}

	// 根目录中直接加载 .md 文件（仅在第一层）
	if includeRootFiles {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			if entry.Name() == skillFileName {
				continue // 已在上面处理
			}
			skill, diags := parseSkillFile(filepath.Join(dir, entry.Name()))
			result.Diagnostics = append(result.Diagnostics, diags...)
			if skill != nil {
				result.Skills = append(result.Skills, *skill)
			}
		}
	}

	return result
}

// ─── 解析 ─────────────────────────────────────────────────────────────────────

// frontmatter 解析 YAML frontmatter（--- 分隔符之间的内容）。
// 返回 frontmatter map 和 body 内容。
func parseFrontmatter(content string) (map[string]string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, content
	}

	fm := make(map[string]string)
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
		// 解析 key: value
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			fm[key] = value
		}
	}

	if endIdx == -1 {
		return nil, content
	}

	body := strings.Join(lines[endIdx+1:], "\n")
	return fm, strings.TrimSpace(body)
}

// parseSkillFile 解析一个技能文件。
func parseSkillFile(filePath string) (*Skill, []Diagnostic) {
	var diags []Diagnostic

	data, err := os.ReadFile(filePath)
	if err != nil {
		diags = append(diags, Diagnostic{
			Code:    DiagReadFailed,
			Message: err.Error(),
			Path:    filePath,
		})
		return nil, diags
	}

	content := string(data)
	fm, body := parseFrontmatter(content)

	// 确定 name：优先用 frontmatter，否则用目录名或文件名
	name := ""
	desc := ""
	disableModelInvocation := false

	if fm != nil {
		name = fm["name"]
		desc = fm["description"]
		if v, ok := fm["disable-model-invocation"]; ok {
			disableModelInvocation = v == "true"
		}
	}

	if name == "" {
		// 用文件所在目录名（对于 SKILL.md）或文件名（对于 .md）
		base := filepath.Base(filePath)
		if base == skillFileName {
			name = filepath.Base(filepath.Dir(filePath))
		} else {
			name = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}

	// 如果 body 为空，整个内容作为 body
	if body == "" {
		body = strings.TrimSpace(content)
	}

	// 验证
	parentDirName := filepath.Base(filepath.Dir(filePath))
	if errs := validateName(name, parentDirName, filePath); len(errs) > 0 {
		for _, e := range errs {
			diags = append(diags, Diagnostic{
				Code:    DiagInvalidName,
				Message: e,
				Path:    filePath,
			})
		}
	}
	if errs := validateDescription(desc, filePath); len(errs) > 0 {
		for _, e := range errs {
			diags = append(diags, Diagnostic{
				Code:    DiagInvalidDesc,
				Message: e,
				Path:    filePath,
			})
		}
	}

	return &Skill{
		Name:                   name,
		Description:            desc,
		Content:                body,
		FilePath:               filePath,
		BaseDir:                filepath.Dir(filePath),
		DisableModelInvocation: disableModelInvocation,
	}, diags
}

// ─── 验证 ─────────────────────────────────────────────────────────────────────

func validateName(name, parentDirName, filePath string) []string {
	var errs []string
	if name != parentDirName && filepath.Base(filePath) != skillFileName {
		// 对于非 SKILL.md 文件，不强求 name 与目录名匹配
		// 但仍然检查格式
	}
	if len(name) > maxNameLength {
		errs = append(errs, fmt.Sprintf("name exceeds %d characters (%d)", maxNameLength, len(name)))
	}
	if !validNameRegex.MatchString(name) {
		errs = append(errs, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errs = append(errs, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errs = append(errs, "name must not contain consecutive hyphens")
	}
	return errs
}

func validateDescription(desc string, filePath string) []string {
	var errs []string
	if strings.TrimSpace(desc) == "" {
		// description 缺失是警告，不阻止加载
		errs = append(errs, "description is empty (recommended but not required)")
	} else if len(desc) > maxDescriptionLength {
		errs = append(errs, fmt.Sprintf("description exceeds %d characters (%d)", maxDescriptionLength, len(desc)))
	}
	return errs
}

// ─── 格式化 ───────────────────────────────────────────────────────────────────

// FormatForSystemPrompt 将技能列表格式化为系统提示中的 XML 块。
// 只包含 DisableModelInvocation 为 false 的技能。
// 遵循 agentskills.io 格式。
func FormatForSystemPrompt(skills []Skill) string {
	visible := make([]Skill, 0, len(skills))
	for _, s := range skills {
		if !s.DisableModelInvocation {
			visible = append(visible, s)
		}
	}
	if len(visible) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("The following skills provide specialized instructions for specific tasks.\n")
	b.WriteString("Read the full skill file when the task matches its description.\n")
	b.WriteString("When a skill file references a relative path, resolve it against the skill directory and use that absolute path in tool commands.\n")
	b.WriteString("\n<available_skills>\n")

	for _, skill := range visible {
		b.WriteString("  <skill>\n")
		b.WriteString(fmt.Sprintf("    <name>%s</name>\n", escapeXML(skill.Name)))
		b.WriteString(fmt.Sprintf("    <description>%s</description>\n", escapeXML(skill.Description)))
		b.WriteString(fmt.Sprintf("    <location>%s</location>\n", escapeXML(skill.FilePath)))
		b.WriteString("  </skill>\n")
	}

	b.WriteString("</available_skills>")
	return b.String()
}

// FormatInvocation 格式化技能调用消息。
// 用于将技能内容注入到用户消息中。
func FormatInvocation(skill Skill, additionalInstructions string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<skill name=\"%s\" location=\"%s\">\n", escapeXML(skill.Name), escapeXML(skill.FilePath)))
	b.WriteString(fmt.Sprintf("References are relative to %s.\n\n", escapeXML(skill.BaseDir)))
	b.WriteString(skill.Content)
	b.WriteString("\n</skill>")
	if additionalInstructions != "" {
		b.WriteString("\n\n")
		b.WriteString(additionalInstructions)
	}
	return b.String()
}

// FindByName 从技能列表中按名称查找技能。
func FindByName(skills []Skill, name string) *Skill {
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i]
		}
	}
	return nil
}

// ─── 辅助 ─────────────────────────────────────────────────────────────────────

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// ignoreMatcher 简单的忽略文件匹配器。
// 仅在递归加载时跳过特定目录。
type ignoreMatcher struct {
	patterns []string
}

func newIgnoreMatcher() *ignoreMatcher {
	return &ignoreMatcher{}
}

func newIgnoreMatcherWithPatterns(patterns []string) *ignoreMatcher {
	return &ignoreMatcher{patterns: patterns}
}

// shouldIgnoreDir 判断目录是否应该被跳过。
// 结合 ignore patterns 和基础规则（隐藏目录等）。
func shouldIgnoreDir(name string, ignore *ignoreMatcher) bool {
	// 基础规则：始终跳过隐藏目录
	if strings.HasPrefix(name, ".") {
		return true
	}
	// 基础规则：跳过常见无关目录
	basicIgnores := []string{"node_modules", "vendor", "__pycache__", ".git", ".svn", ".hg"}
	for _, pattern := range basicIgnores {
		if name == pattern {
			return true
		}
	}
	// 自定义 ignore patterns
	return ignore.match(name)
}

func (im *ignoreMatcher) match(name string) bool {
	for _, p := range im.patterns {
		if p == name {
			return true
		}
	}
	return false
}

// LoadIgnoreFile 尝试加载目录中的 .gitignore 样式忽略规则。
// 当前实现仅支持简单的文件名匹配。
func loadIgnorePatterns(dir string) []string {
	var patterns []string
	for _, name := range []string{".gitignore", ".ignore", ".fdignore"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			patterns = append(patterns, line)
		}
		f.Close()
	}
	return patterns
}
