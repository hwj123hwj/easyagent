package models

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
		"claude-3-5-sonnet": 200000,
		"claude-3-5-haiku":  200000,
		"claude-3-opus":     200000,
		"claude-sonnet-4":   200000,
		"claude-sonnet-4-5": 200000,
		"gpt-4o":            128000,
		"gpt-4o-mini":       128000,
		"gpt-4-turbo":       128000,
		"gpt-4":             8192,
		"o1":                200000,
		"o1-mini":           128000,
		"o3-mini":           200000,
		"claude-sonnet-4-6": 200000,
		"glm-5":             128000,
		"deepseek-v4-flash": 128000,
	}
	if w, ok := windows[modelID]; ok {
		return w, true
	}
	return 0, false
}
