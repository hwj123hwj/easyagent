package kbtools

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Data Types ────────────────────────────────────────────────────────────

// Entry represents a single knowledge document in the repository.
type Entry struct {
	Path     string   // absolute path
	RelPath  string   // path relative to repo root
	Title    string   // first heading or frontmatter title
	Category string   // from frontmatter, > 分类 line, or directory name
	Tags     []string // from frontmatter, ## 标签 section, or inline tags
	Summary  string   // from frontmatter, ## 摘要 section, or first paragraph
	Source   string   // > 来源：... metadata line
	Modified time.Time
}

// Index is the in-memory representation of the entire repository.
type Index struct {
	Entries []Entry
	Root    string
}

// indexCache caches the index for a short duration to avoid repeated scans.
var (
	cachedIndex   *Index
	cachedAt      time.Time
	cacheMu       sync.Mutex
	cacheDuration = 30 * time.Second
)

// skipFiles are filenames that should not be indexed (auto-generated or non-knowledge).
var skipFiles = map[string]bool{
	"INDEX.md":           true,
	"KNOWLEDGE_BASE.md":  true,
	"tags-index.json":    true,
	"by-project.md":      true,
	"USER-CHANGELOG.md":  true,
}

// skipDirs are directory prefixes that should not be indexed.
var skipDirs = []string{
	"scripts/",
	"hooks/",
	".claude/",
	".git/",
	"devops/",
}

// GetIndex builds (or returns cached) an index of all .md files in the repo.
func GetIndex(repoPath string) (*Index, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cachedIndex != nil && time.Since(cachedAt) < cacheDuration {
		if cachedIndex.Root == repoPath {
			return cachedIndex, nil
		}
	}

	idx := &Index{Root: repoPath}
	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Only index .md files
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		relPath, _ := filepath.Rel(repoPath, path)
		relPath = filepath.ToSlash(relPath)

		// Skip root README
		if relPath == "README.md" {
			return nil
		}
		// Skip auto-generated index files
		baseName := filepath.Base(path)
		if skipFiles[baseName] {
			return nil
		}
		// Skip certain directories
		for _, skipDir := range skipDirs {
			if strings.HasPrefix(relPath, skipDir) {
				return nil
			}
		}

		entry := parseEntry(path, repoPath)
		entry.RelPath = relPath
		idx.Entries = append(idx.Entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	cachedIndex = idx
	cachedAt = time.Now()
	return idx, nil
}

// ClearCache invalidates the index cache (e.g. after a kb_save).
func ClearCache() {
	cacheMu.Lock()
	cachedIndex = nil
	cachedAt = time.Time{}
	cacheMu.Unlock()
}

// ── Parsing ───────────────────────────────────────────────────────────────

// parseEntry reads a markdown file and extracts metadata.
// It supports multiple formats found in the agent-lessons repo:
//   - YAML frontmatter (issues/: ---\ntitle: ...\ntags: [...]\n---)
//   - doubao-knowledge cards (# Title\n> 来源...\n## 关键要点\n## 摘要\n## 标签)
//   - chatgpt-export/doubao-export conversations (# Title\n> URL...\n## 👤 User)
//   - project-journals (# Title\n> 自动生成于...)
//   - Legacy markdown (# Title\n\n**Tags**: a, b\n\nbody)
func parseEntry(absPath, repoPath string) Entry {
	info, _ := os.Stat(absPath)
	entry := Entry{
		Path:     absPath,
		Modified: modTime(info),
	}

	// Category from directory structure
	entry.Category = deriveCategory(absPath, repoPath)

	content, err := os.ReadFile(absPath)
	if err != nil {
		return entry
	}
	text := string(content)

	// Try YAML frontmatter first
	if fm, body, ok := parseFrontmatter(text); ok {
		if v := unquote(fm["title"]); v != "" {
			entry.Title = v
		}
		if v := unquote(fm["summary"]); v != "" {
			entry.Summary = v
		}
		if v := unquote(fm["category"]); v != "" {
			entry.Category = v
		}
		if tags, ok := fm["tags"]; ok {
			entry.Tags = parseTagsValue(tags)
		}
		// Fallbacks from body
		if entry.Title == "" {
			entry.Title = extractTitle(body)
		}
		if entry.Summary == "" {
			entry.Summary = extractSectionBody(body, "摘要")
		}
		if len(entry.Tags) == 0 {
			entry.Tags = extractSectionTags(body)
		}
		return entry
	}

	// No frontmatter — parse markdown body
	entry.Title = extractTitle(text)
	entry.Source = extractSourceLine(text)

	// doubao-knowledge format: ## 摘要 and ## 标签 sections
	entry.Summary = extractSectionBody(text, "摘要")
	entry.Tags = extractSectionTags(text)

	// Fallback: legacy **Tags**: line
	if len(entry.Tags) == 0 {
		entry.Tags = extractInlineTags(text)
	}
	// Fallback: first paragraph as summary
	if entry.Summary == "" {
		entry.Summary = extractSummary(text)
	}
	return entry
}

// deriveCategory determines the category from the file's directory path.
// For doubao-knowledge/tech/ → "tech", doubao-knowledge/life/ → "life", etc.
// For issues/ → "issues", project-journals/ → "project-journals", etc.
func deriveCategory(absPath, repoPath string) string {
	relDir, err := filepath.Rel(repoPath, filepath.Dir(absPath))
	if err != nil || relDir == "." {
		return ""
	}
	relDir = filepath.ToSlash(relDir)
	parts := strings.SplitN(relDir, "/", 2)

	// doubao-knowledge has meaningful sub-directories as categories
	if parts[0] == "doubao-knowledge" && len(parts) > 1 {
		return parts[1] // "tech", "life", "english", "work", "writing", "other"
	}
	// Also handle chatgpt-export/doubao-export sub-dirs if any
	if (parts[0] == "chatgpt-export" || parts[0] == "doubao-export") && len(parts) > 1 {
		return parts[1]
	}
	return parts[0]
}

// parseFrontmatter extracts YAML frontmatter (key: value) from the top of a markdown file.
// Returns (metadata map, body without frontmatter, true) if frontmatter exists.
func parseFrontmatter(text string) (map[string]string, string, bool) {
	trimmed := strings.TrimLeft(text, "\n\r\t ")
	if !strings.HasPrefix(trimmed, "---") {
		return nil, text, false
	}
	lines := strings.SplitN(trimmed, "\n", -1)
	if len(lines) < 2 {
		return nil, text, false
	}

	endLine := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endLine = i
			break
		}
	}
	if endLine == -1 {
		return nil, text, false
	}

	meta := make(map[string]string)
	for i := 1; i < endLine; i++ {
		line := lines[i]
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		meta[key] = val
	}

	body := strings.Join(lines[endLine+1:], "\n")
	return meta, body, true
}

// parseTagsValue parses a tags field value like "Go, concurrency" or "[Go, concurrency]".
func parseTagsValue(raw string) []string {
	raw = strings.Trim(raw, "[]")
	parts := strings.Split(raw, ",")
	var tags []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

// extractTitle finds the first markdown heading.
func extractTitle(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	// Fallback: first non-empty line
	scanner2 := bufio.NewScanner(strings.NewReader(text))
	for scanner2.Scan() {
		line := strings.TrimSpace(scanner2.Text())
		if line != "" && !strings.HasPrefix(line, "---") {
			return line
		}
	}
	return "(无标题)"
}

// extractSourceLine extracts the "> 来源：..." or "> URL: ..." metadata line.
func extractSourceLine(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "> 来源") || strings.HasPrefix(line, "> URL:") {
			return strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
	}
	return ""
}

// extractSectionBody extracts the content under a "## {heading}" section.
// For example, extractSectionBody(text, "摘要") finds "## 摘要" and returns
// the text until the next "## " heading.
func extractSectionBody(text, heading string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	inSection := false
	var lines []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Check if we're entering the target section
		if !inSection {
			if strings.HasPrefix(trimmed, "## ") {
				h := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
				if strings.EqualFold(h, heading) {
					inSection = true
				}
			}
			continue
		}

		// We're in the section — check for end conditions
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "---") {
			break // next section or horizontal rule
		}
		if trimmed == "" && len(lines) == 0 {
			continue // skip leading blank lines
		}
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	if len(lines) == 0 {
		return ""
	}
	summary := strings.Join(lines, " ")
	if len(summary) > 200 {
		summary = summary[:200] + "..."
	}
	return summary
}

// extractSectionTags extracts tags from a "## 标签" section.
// Handles multiple formats:
//
//	tag1, tag2, tag3
//	`tag1` `tag2` `tag3`
//	- tag1
//	- tag2
func extractSectionTags(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	inSection := false
	var tags []string
	seen := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if !inSection {
			if strings.HasPrefix(trimmed, "## ") {
				h := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
				if strings.EqualFold(h, "标签") || strings.EqualFold(h, "Tags") {
					inSection = true
				}
			}
			continue
		}

		// End of section
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "---") {
			break
		}
		if trimmed == "" {
			continue
		}

		// Parse tags from this line
		// Format 1: `tag1` `tag2` `tag3`  (backtick-wrapped)
		if strings.Contains(trimmed, "`") {
			for _, part := range strings.Split(trimmed, "`") {
				part = strings.TrimSpace(part)
				if part != "" && !seen[part] {
					tags = append(tags, part)
					seen[part] = true
				}
			}
			continue
		}
		// Format 2: - tag1 (list item)
		if strings.HasPrefix(trimmed, "- ") {
			tag := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if tag != "" && !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
			continue
		}
		// Format 3: tag1, tag2, tag3 (comma-separated)
		for _, tag := range strings.Split(trimmed, ",") {
			tag = strings.TrimSpace(tag)
			tag = strings.Trim(tag, "`*")
			if tag != "" && !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
		}
	}
	return tags
}

// extractSummary returns the first non-heading, non-empty paragraph.
// Used as fallback when no ## 摘要 section exists.
func extractSummary(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	var lines []string
	started := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "---" || line == "" {
			if started {
				break
			}
			continue
		}
		// Skip metadata lines (> ...)
		if strings.HasPrefix(line, ">") {
			continue
		}
		started = true
		lines = append(lines, line)
		if len(lines) >= 3 {
			break
		}
	}
	summary := strings.Join(lines, " ")
	if len(summary) > 150 {
		summary = summary[:150] + "..."
	}
	return summary
}

// extractInlineTags finds tag patterns in legacy format (**Tags**: or #tag).
func extractInlineTags(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	var tags []string
	seen := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		lower := strings.ToLower(line)
		if strings.Contains(lower, "**tags**") || strings.Contains(lower, "**标签**") {
			idx := strings.Index(line, ":")
			if idx != -1 {
				for _, tag := range strings.Split(line[idx+1:], ",") {
					tag = strings.TrimSpace(tag)
					tag = strings.Trim(tag, "`*")
					if tag != "" && !seen[tag] {
						tags = append(tags, tag)
						seen[tag] = true
					}
				}
			}
		}
		words := strings.Fields(line)
		for _, w := range words {
			if len(w) > 1 && strings.HasPrefix(w, "#") && !strings.HasPrefix(w, "##") {
				tag := strings.TrimPrefix(w, "#")
				tag = strings.TrimRight(tag, ".,;!?")
				if isTagLike(tag) && !seen[tag] {
					tags = append(tags, tag)
					seen[tag] = true
				}
			}
		}
	}
	return tags
}

// isTagLike checks if a string looks like a tag (alphanumeric, hyphen, underscore).
func isTagLike(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// modTime returns the modification time, or zero if info is nil.
func modTime(info os.FileInfo) time.Time {
	if info == nil {
		return time.Time{}
	}
	return info.ModTime()
}

// unquote removes surrounding quotes from a YAML scalar value.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
