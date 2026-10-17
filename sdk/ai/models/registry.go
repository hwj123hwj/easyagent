package models

import "strings"

// ContextWindow returns the context window size for a given model ID.
// Returns 128000 as the default for unknown models.
func ContextWindow(modelID string) int {
	if window, ok := LookupContextWindow(modelID); ok {
		return window
	}
	return 128000
}

// LookupContextWindow distinguishes known capacities from the runtime fallback.
func LookupContextWindow(modelID string) (int, bool) {
	windows := map[string]int{
		"claude-3-5-sonnet":      200000,
		"claude-3-5-haiku":       200000,
		"claude-3-opus":          200000,
		"claude-sonnet-4":        200000,
		"claude-sonnet-4-5":      200000,
		"gpt-4o":                 128000,
		"gpt-4o-mini":            128000,
		"gpt-4-turbo":            128000,
		"gpt-4":                  8192,
		"o1":                     200000,
		"o1-mini":                128000,
		"o3-mini":                200000,
		"claude-sonnet-4-6":      200000,
		"glm-5":                  128000,
		"glm-5.3":                128000,
		"glm-5.3-flash":          128000,
		"deepseek-v4-flash":      128000,
		"deepseek-v4.1-flash":    160000,
		"deepseek-flash":         160000,
		"gemini-3.8-flash-high":  1000000,
		"gemini-2.5-flash":       1000000,
		"gemini-2.5-pro":         1000000,
		"gemini-1.5-pro":         1000000,
		"gemini-1.5-flash":       1000000,
	}
	if w, ok := windows[modelID]; ok {
		return w, true
	}
	if strings.HasPrefix(modelID, "gemini-") {
		return 1000000, true
	}
	return 0, false
}

// MaxOutput returns the max output tokens to request for a model ID.
// Values are conservative per-model/family caps; unknown models fall back
// to 4096, matching the provider-level default.
func MaxOutput(modelID string) int {
	if v, ok := maxOutputs[modelID]; ok {
		return v
	}
	for prefix, v := range prefixMaxOutputs {
		if strings.HasPrefix(modelID, prefix) {
			return v
		}
	}
	return 4096
}

var maxOutputs = map[string]int{
	"claude-3-5-sonnet":     8192,
	"claude-3-5-haiku":      8192,
	"claude-3-opus":         4096,
	"claude-sonnet-4":       8192,
	"claude-sonnet-4-5":     8192,
	"claude-sonnet-4-6":     8192,
	"gpt-4o":                4096,
	"gpt-4o-mini":           4096,
	"gpt-4-turbo":           4096,
	"gpt-4":                 4096,
	"o1":                    32768,
	"o1-mini":               65536,
	"o3-mini":               65536,
	"glm-5":                 32768,
	"glm-5.3":               32768,
	"glm-5.3-flash":         32768,
	"deepseek-v4-flash":     8192,
	"deepseek-v4.1-flash":   8192,
	"deepseek-flash":        8192,
	"gemini-3.8-flash-high": 65536,
	"gemini-2.5-flash":      65536,
	"gemini-2.5-pro":        65536,
	"gemini-1.5-pro":        8192,
	"gemini-1.5-flash":      8192,
}

// prefixMaxOutputs covers gateway model variants (glm-5.3-air etc.).
// Prefixes are disjoint, so map iteration order does not matter.
var prefixMaxOutputs = map[string]int{
	"glm-":      32768,
	"deepseek-": 8192,
	"gemini-":   8192,
}
