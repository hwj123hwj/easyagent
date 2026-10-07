package kbtools

import (
	"sort"
	"strings"
)

// ── Search Strategy Interface ─────────────────────────────────────────────
//
// The search layer is intentionally decoupled from the index and the tool.
// kb_search delegates to a SearchStrategy, making the retrieval method
// pluggable: KeywordSearcher today, VectorSearcher tomorrow.
//
// ┌────────────┐     ┌──────────────────┐     ┌─────────┐
// │ kb_search  │────▶│  SearchStrategy   │────▶│  Index  │
// │  (tool)    │     │ (interface)      │     │ (data)  │
// └────────────┘     └──────────────────┘     └─────────┘

// SearchQuery is the input to a search strategy.
type SearchQuery struct {
	Query    string   // free-text keywords (space-separated)
	Tag      string   // exact tag filter (case-insensitive)
	Category string   // category filter
	Limit    int      // max results (0 = no limit)
}

// SearchResult is a single hit.
type SearchResult struct {
	Entry Entry
	Score float64
}

// SearchStrategy is the pluggable retrieval interface.
type SearchStrategy interface {
	// Name returns the strategy identifier (e.g. "keyword", "vector").
	Name() string
	// Search executes the query against the given entries.
	Search(entries []Entry, q SearchQuery) []SearchResult
}

// ── KeywordSearcher (default strategy) ────────────────────────────────────

// KeywordSearcher implements weighted full-text matching.
// This is the default and currently only strategy.
type KeywordSearcher struct{}

func (KeywordSearcher) Name() string { return "keyword" }

func (KeywordSearcher) Search(entries []Entry, q SearchQuery) []SearchResult {
	keywords := strings.Fields(q.Query)
	limit := q.Limit

	var results []SearchResult
	for _, entry := range entries {
		// ── Filters (tag / category) apply regardless of strategy ──
		if q.Category != "" && !strings.EqualFold(entry.Category, q.Category) {
			continue
		}
		if q.Tag != "" {
			if !entryHasTag(entry, q.Tag) {
				continue
			}
		}

		// ── Score ──
		var score float64
		if len(keywords) > 0 {
			score = scoreEntry(entry, keywords)
			if score == 0 {
				continue
			}
		} else {
			score = 1.0 // no query → return all (already filtered)
		}
		results = append(results, SearchResult{Entry: entry, Score: score})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Tie-breaker: more recently modified first
		return results[i].Entry.Modified.After(results[j].Entry.Modified)
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

// entryHasTag checks whether an entry has a tag (case-insensitive exact match).
func entryHasTag(entry Entry, tag string) bool {
	for _, t := range entry.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// scoreEntry computes a weighted relevance score.
//
//	Matching area        Weight
//	─────────────────    ──────
//	Title contains kw      5.0
//	Tag exact match        3.0
//	Tag partial match      2.0
//	Summary contains kw    2.0
//	Path contains kw       1.0
func scoreEntry(entry Entry, keywords []string) float64 {
	var total float64
	for _, kw := range keywords {
		kwLower := strings.ToLower(kw)
		if kwLower == "" {
			continue
		}
		// Title match (weight 5)
		if strings.Contains(strings.ToLower(entry.Title), kwLower) {
			total += 5.0
		}
		// Tag match (weight 3 exact, 2 partial)
		for _, tag := range entry.Tags {
			if strings.EqualFold(tag, kw) {
				total += 3.0
			} else if strings.Contains(strings.ToLower(tag), kwLower) {
				total += 2.0
			}
		}
		// Summary match (weight 2)
		if strings.Contains(strings.ToLower(entry.Summary), kwLower) {
			total += 2.0
		}
		// Path match (weight 1)
		if strings.Contains(strings.ToLower(entry.RelPath), kwLower) {
			total += 1.0
		}
	}
	return total
}
