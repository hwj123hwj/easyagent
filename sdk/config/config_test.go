package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	assert.Equal(t, "easyagent", cfg.Name)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "", cfg.Provider)
	assert.Equal(t, 200, cfg.MaxTurns)
}

func TestLoadFromEnv(t *testing.T) {
	cfg := Default()

	os.Setenv("EA_PROVIDER", "openai")
	os.Setenv("OPENAI_API_KEY", "test-key")
	os.Setenv("OPENAI_MODEL", "gpt-4")
	os.Setenv("OPENAI_BASE_URL", "https://api.test.com")
	os.Setenv("EA_PORT", "9090")
	defer func() {
		os.Unsetenv("EA_PROVIDER")
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("OPENAI_MODEL")
		os.Unsetenv("OPENAI_BASE_URL")
		os.Unsetenv("EA_PORT")
	}()

	cfg.LoadFromEnv()
	assert.Equal(t, "openai", cfg.Provider)
	assert.Equal(t, "test-key", cfg.OpenAIAPIKey)
	assert.Equal(t, "gpt-4", cfg.OpenAIModel)
	assert.Equal(t, "https://api.test.com", cfg.OpenAIBaseURL)
	assert.Equal(t, 9090, cfg.Port)
}

func TestEnvReadsEAPrefixOnly(t *testing.T) {
	t.Setenv("EA_PROVIDER", "openai")
	t.Setenv("PI_GO_PROVIDER", "anthropic")
	assert.Equal(t, "openai", Env("EA_PROVIDER"))

	t.Setenv("EA_PROVIDER", "")
	assert.Equal(t, "", Env("EA_PROVIDER"))
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "TEST_VAR_123=hello_world\n# comment\n\nANOTHER_VAR=456\n"
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0o644))

	err := LoadDotEnv(envFile)
	require.NoError(t, err)
	defer func() {
		os.Unsetenv("TEST_VAR_123")
		os.Unsetenv("ANOTHER_VAR")
	}()

	assert.Equal(t, "hello_world", os.Getenv("TEST_VAR_123"))
	assert.Equal(t, "456", os.Getenv("ANOTHER_VAR"))
}

func TestLoadDotEnv_NotExist(t *testing.T) {
	err := LoadDotEnv("/nonexistent/.env")
	assert.Error(t, err)
}
