package kbtools

// ── Maintenance (second-brain health) ─────────────────────────────────────
//
// The KB agent is not just a reader — it's the **owner** of the second brain.
// This module provides maintenance capabilities: health checks, deduplication
// detection, tag normalization suggestions, and metadata completeness audits.
//
// All operations are READ-ONLY by default — they produce *recommendations*.
// The AI reviews them and decides what to act on (with user confirmation).

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── Health Report ────────────────────────────────────────────────────────

// HealthReport is the top-level result of a KB health check.
type HealthReport struct {
	GeneratedAt       time.Time
	TotalEntries      int
	Categories        int
	Tags              int
	EntriesMissingSummary []Entry
	EntriesMissingTags    []Entry
	EntriesMissingTitle   []Entry
	DuplicateGroups   []DuplicateGroup
	TagClusters       []TagCluster
}

// DuplicateGroup is a set of entries with highly similar titles.
type DuplicateGroup struct {
	CanonicalTitle string
	Entries        []Entry
}

// TagCluster is a group of tags that likely refer to the same concept.
type TagCluster struct {
	Canonical string   // suggested canonical form
	Variants  []string // the variants found
	Count     int      // total entries using any variant
}

// Summary returns a human-readable one-liner of the report.
func (r HealthReport) Summary() string {
	issues := 0
	issues += len(r.EntriesMissingSummary)
	issues += len(r.EntriesMissingTags)
	issues += len(r.EntriesMissingTitle)
	issues += len(r.DuplicateGroups)
	issues += len(r.TagClusters)
	return fmt.Sprintf("知识库健康检查：%d 条目、%d 分类、%d 标签，发现 %d 个待改善点", r.TotalEntries, r.Categories, r.Tags, issues)
}

// GenerateHealthReport scans the index and produces a health report.
func GenerateHealthReport(idx *Index) HealthReport {
	report := HealthReport{
		GeneratedAt: time.Now(),
	}

	catSet := make(map[string]bool)
	tagSet := make(map[string]bool)

	// ── Check each entry for missing metadata ──
	for _, e := range idx.Entries {
		report.TotalEntries++
		if e.Category != "" {
			catSet[e.Category] = true
		}
		for _, t := range e.Tags {
			tagSet[t] = true
		}
		if e.Title == "" || e.Title == "(无标题)" {
			report.EntriesMissingTitle = append(report.EntriesMissingTitle, e)
		}
		if e.Summary == "" {
			report.EntriesMissingSummary = append(report.EntriesMissingSummary, e)
		}
		if len(e.Tags) == 0 {
			report.EntriesMissingTags = append(report.EntriesMissingTags, e)
		}
	}

	report.Categories = len(catSet)
	report.Tags = len(tagSet)

	// ── Detect duplicate titles (normalized) ──
	report.DuplicateGroups = detectDuplicateTitles(idx.Entries)

	// ── Detect tag clusters (same concept, different spelling) ──
	report.TagClusters = detectTagClusters(idx.Entries)

	return report
}

// ── Duplicate Detection ───────────────────────────────────────────────────

// detectDuplicateTitles groups entries whose normalized titles are identical
// or near-identical (case/spacing differences).
func detectDuplicateTitles(entries []Entry) []DuplicateGroup {
	// Build a map of normalized title → entries
	groups := make(map[string][]Entry)
	for _, e := range entries {
		norm := normalizeTitle(e.Title)
		if norm == "" {
			continue
		}
		groups[norm] = append(groups[norm], e)
	}

	var result []DuplicateGroup
	for _, group := range groups {
		if len(group) > 1 {
			result = append(result, DuplicateGroup{
				CanonicalTitle: group[0].Title,
				Entries:        group,
			})
		}
	}
	// Sort by group size (largest first)
	sort.Slice(result, func(i, j int) bool {
		return len(result[i].Entries) > len(result[j].Entries)
	})
	return result
}

// normalizeTitle lowercases, trims, and collapses whitespace for comparison.
func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	// Remove common punctuation
	s = strings.Trim(s, ".,;:!?\"'()[]")
	return s
}

// ── Tag Cluster Detection ─────────────────────────────────────────────────

// detectTagClusters finds tags that differ only in case or trivial variations.
// Example: "Ollama" / "ollama" / "Ollama AI" would be clustered.
func detectTagClusters(entries []Entry) []TagCluster {
	// Count tag usage and build a lowercased index
	tagLower := make(map[string][]string) // lowercase → list of original forms
	tagCount := make(map[string]int)      // lowercase → total count

	for _, e := range entries {
		for _, t := range e.Tags {
			lower := strings.ToLower(strings.TrimSpace(t))
			tagLower[lower] = appendUnique(tagLower[lower], t)
			tagCount[lower]++
		}
	}

	// Find lowercase keys that have >1 distinct original form
	var clusters []TagCluster
	for lower, forms := range tagLower {
		if len(forms) > 1 {
			// Pick canonical: the most common form (first by count, then alphabetically)
			canonical := mostCommonForm(entries, forms)
			clusters = append(clusters, TagCluster{
				Canonical: canonical,
				Variants:  forms,
				Count:     tagCount[lower],
			})
		}
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Count > clusters[j].Count
	})
	return clusters
}

// mostCommonForm returns the variant that appears most frequently across all entries.
func mostCommonForm(entries []Entry, forms []string) string {
	counts := make(map[string]int)
	for _, e := range entries {
		for _, t := range e.Tags {
			for _, f := range forms {
				if t == f {
					counts[f]++
				}
			}
		}
	}
	best := forms[0]
	bestCount := 0
	for _, f := range forms {
		if counts[f] > bestCount || (counts[f] == bestCount && f < best) {
			best = f
			bestCount = counts[f]
		}
	}
	return best
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// ── Category Overview ─────────────────────────────────────────────────────

// CategoryStat holds stats for a single category.
type CategoryStat struct {
	Name  string
	Count int
}

// CategoryOverview returns a sorted list of categories with entry counts.
func CategoryOverview(idx *Index) []CategoryStat {
	counts := make(map[string]int)
	for _, e := range idx.Entries {
		cat := e.Category
		if cat == "" {
			cat = "other"
		}
		counts[cat]++
	}
	var stats []CategoryStat
	for name, count := range counts {
		stats = append(stats, CategoryStat{Name: name, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})
	return stats
}

// ── Tag Overview ──────────────────────────────────────────────────────────

// TagStat holds stats for a single tag.
type TagStat struct {
	Name  string
	Count int
}

// TagOverview returns tags sorted by usage frequency.
func TagOverview(idx *Index) []TagStat {
	counts := make(map[string]int)
	for _, e := range idx.Entries {
		for _, t := range e.Tags {
			counts[t]++
		}
	}
	var stats []TagStat
	for name, count := range counts {
		stats = append(stats, TagStat{Name: name, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})
	return stats
}

// ── Tag Stats helper ──────────────────────────────────────────────────────

// countTagsDistinct returns the number of distinct tags across all entries.
func countTagsDistinct(idx *Index) int {
	seen := make(map[string]bool)
	for _, e := range idx.Entries {
		for _, t := range e.Tags {
			seen[t] = true
		}
	}
	return len(seen)
}
