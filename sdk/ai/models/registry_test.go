package models

import "testing"

func TestContextWindow_KnownModels(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"claude-sonnet-4-6", 200000},
		{"claude-sonnet-4-5", 200000},
		{"claude-3-5-sonnet", 200000},
		{"gpt-4o", 128000},
		{"gpt-4", 8192},
		{"o1", 200000},
		{"glm-5", 128000},
	}
	for _, tt := range tests {
		got := ContextWindow(tt.model)
		if got != tt.want {
			t.Errorf("ContextWindow(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestContextWindow_UnknownModel(t *testing.T) {
	got := ContextWindow("nonexistent-model-v1")
	if got != 128000 {
		t.Errorf("ContextWindow(unknown) = %d, want 128000", got)
	}
}

func TestMaxOutput_KnownModels(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"glm-5.3-flash", 32768},
		{"glm-5.3", 32768},
		{"glm-5.3-air", 32768}, // 家族前缀兜底
		{"deepseek-v4.1-flash", 8192},
		{"gemini-3.8-flash-high", 65536},
		{"gemini-2.5-pro", 65536},
		{"gemini-1.5-flash", 8192},
		{"gemini-4.0-pro", 8192}, // 家族前缀兜底
		{"claude-sonnet-4-6", 8192},
		{"claude-3-opus", 4096},
		{"gpt-4o", 4096},
		{"o1", 32768},
	}
	for _, tt := range tests {
		got := MaxOutput(tt.model)
		if got != tt.want {
			t.Errorf("MaxOutput(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestMaxOutput_UnknownModel(t *testing.T) {
	got := MaxOutput("nonexistent-model-v1")
	if got != 4096 {
		t.Errorf("MaxOutput(unknown) = %d, want 4096", got)
	}
}
