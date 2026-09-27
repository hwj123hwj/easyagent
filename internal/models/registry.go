// Package models provides a config-driven model registry that replaces
// hardcoded model lists. Models are defined in a JSON config file or
// constructed programmatically.
package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/config"
)

// ModelDef defines a single model available for use.
type ModelDef struct {
	ID            string `json:"id"`               // unique identifier (e.g. "claude-sonnet-4-6")
	Provider      string `json:"provider"`         // "anthropic", "openai", etc.
	Name          string `json:"name"`             // display name (e.g. "Claude Sonnet 4.6")
	ContextWindow int    `json:"context_window"`   // max context tokens (e.g. 200000)
	MaxTokens     int    `json:"max_tokens"`       // max output tokens (e.g. 4096)
	Hidden        bool   `json:"hidden,omitempty"` // hidden from /models list but still usable
}

// Registry holds all known models and provides lookup/filtering.
type Registry struct {
	models map[string]ModelDef // keyed by ID
}

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{models: make(map[string]ModelDef)}
}

// Register adds a model to the registry. Overwrites if ID already exists.
func (r *Registry) Register(m ModelDef) {
	r.models[m.ID] = m
}

// Get returns the model with the given ID, or false if not found.
func (r *Registry) Get(id string) (ModelDef, bool) {
	m, ok := r.models[id]
	return m, ok
}

// List returns all non-hidden models, sorted by provider then name.
func (r *Registry) List() []ModelDef {
	result := make([]ModelDef, 0, len(r.models))
	for _, m := range r.models {
		if !m.Hidden {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// ListByProvider returns all non-hidden models for the given provider.
func (r *Registry) ListByProvider(provider string) []ModelDef {
	var result []ModelDef
	for _, m := range r.models {
		if m.Provider == provider && !m.Hidden {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Default returns the default model for a given provider.
// Falls back to the first model for that provider if no default is set.
func (r *Registry) Default(provider string) (ModelDef, bool) {
	models := r.ListByProvider(provider)
	if len(models) == 0 {
		return ModelDef{}, false
	}
	return models[0], true
}

// LoadFromFile loads model definitions from a JSON file.
// Format: array of ModelDef objects.
func (r *Registry) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read models file: %w", err)
	}

	var defs []ModelDef
	if err := json.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("parse models JSON: %w", err)
	}

	for _, def := range defs {
		if def.ID == "" || def.Provider == "" {
			continue // skip invalid entries
		}
		if def.Name == "" {
			def.Name = def.ID
		}
		r.Register(def)
	}

	return nil
}

// DefaultModels returns the built-in default model catalog.
// These are always available without any config file.
func DefaultModels() []ModelDef {
	return []ModelDef{
		// Anthropic
		{ID: "claude-sonnet-4-6", Provider: "anthropic", Name: "Claude Sonnet 4.6", ContextWindow: 200000, MaxTokens: 8192},
		{ID: "claude-sonnet-4-5", Provider: "anthropic", Name: "Claude Sonnet 4.5", ContextWindow: 200000, MaxTokens: 8192},
		{ID: "claude-sonnet-4", Provider: "anthropic", Name: "Claude Sonnet 4", ContextWindow: 200000, MaxTokens: 8192},
		{ID: "claude-opus-4", Provider: "anthropic", Name: "Claude Opus 4", ContextWindow: 200000, MaxTokens: 8192},
		{ID: "claude-haiku-4", Provider: "anthropic", Name: "Claude Haiku 4", ContextWindow: 200000, MaxTokens: 4096},
		// OpenAI
		{ID: "gpt-4o", Provider: "openai", Name: "GPT-4o", ContextWindow: 128000, MaxTokens: 4096},
		{ID: "gpt-4o-mini", Provider: "openai", Name: "GPT-4o Mini", ContextWindow: 128000, MaxTokens: 4096},
		{ID: "gpt-5", Provider: "openai", Name: "GPT-5", ContextWindow: 256000, MaxTokens: 8192},
		{ID: "gpt-5-mini", Provider: "openai", Name: "GPT-5 Mini", ContextWindow: 256000, MaxTokens: 4096},
		// DeepSeek (OpenAI-compatible)
		{ID: "deepseek-chat", Provider: "openai", Name: "DeepSeek Chat", ContextWindow: 64000, MaxTokens: 4096},
		{ID: "deepseek-coder", Provider: "openai", Name: "DeepSeek Coder", ContextWindow: 64000, MaxTokens: 4096},
		// Qwen (OpenAI-compatible)
		{ID: "qwen-max", Provider: "openai", Name: "Qwen Max", ContextWindow: 32000, MaxTokens: 4096},
		{ID: "qwen-plus", Provider: "openai", Name: "Qwen Plus", ContextWindow: 128000, MaxTokens: 4096},
	}
}

// NewDefaultRegistry creates a registry pre-loaded with the default model catalog,
// then optionally merges a user config file.
func NewDefaultRegistry(configPath string) *Registry {
	r := NewRegistry()
	for _, m := range DefaultModels() {
		r.Register(m)
	}

	// Try to load user override file
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			_ = r.LoadFromFile(configPath)
		}
	}

	return r
}

// ResolveConfigPath returns the default path for the models config file.
// Checks: EA_MODELS_FILE → config.HomeDir()/models.json → data dir
func ResolveConfigPath(dataDir string) string {
	if env := config.Env("EA_MODELS_FILE"); env != "" {
		return env
	}
	p := filepath.Join(config.HomeDir(), "models.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if dataDir != "" {
		return filepath.Join(dataDir, "models.json")
	}
	return ""
}

// FormatForDisplay returns a human-readable summary of a model.
func (m ModelDef) FormatForDisplay() string {
	ctx := "unknown"
	if m.ContextWindow > 0 {
		ctx = fmt.Sprintf("%dK", m.ContextWindow/1000)
	}
	return fmt.Sprintf("%s/%s (%s context)", m.Provider, m.Name, ctx)
}

// ProviderSummary returns a summary of providers and their model counts.
func (r *Registry) ProviderSummary() string {
	counts := make(map[string]int)
	for _, m := range r.models {
		if !m.Hidden {
			counts[m.Provider]++
		}
	}
	var providers []string
	for p := range counts {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	var parts []string
	for _, p := range providers {
		parts = append(parts, fmt.Sprintf("%s (%d)", p, counts[p]))
	}
	return strings.Join(parts, ", ")
}
