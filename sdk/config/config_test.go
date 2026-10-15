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

func TestServerKeyDoesNotOverrideModelKey(t *testing.T) {
	t.Setenv("EA_API_KEY", "upstream-key")
	t.Setenv("EA_SERVER_API_KEY", "desktop-session-key")
	t.Setenv("EA_MCP_CONFIG", "/tmp/user-mcp.json")
	cfg := Default()
	cfg.LoadFromEnv()
	assert.Equal(t, "upstream-key", cfg.OpenAIAPIKey)
	assert.Equal(t, "desktop-session-key", cfg.APIKey)
	assert.Equal(t, "desktop-session-key", ServerAPIKey())
	assert.Equal(t, "/tmp/user-mcp.json", cfg.MCPConfigPath)
	t.Setenv("EA_SERVER_API_KEY", "")
	assert.Equal(t, "upstream-key", ServerAPIKey())
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

func TestASRProviderCredentialPairing(t *testing.T) {
	for _, tc := range []struct{ name, asr, silicon, gateway, base, override, wantKey, wantURL string }{
		{name: "gateway", gateway: "gw", base: "https://gateway.test/v1", wantKey: "gw", wantURL: "https://gateway.test/v1"},
		{name: "gateway without endpoint", gateway: "gw", wantURL: "https://api.siliconflow.cn"},
		{name: "explicit URL must not receive gateway key", gateway: "gw", base: "https://gateway.test", override: "https://other.test", wantURL: "https://other.test"},
		{name: "explicit URL must not receive silicon key", silicon: "sf", override: "https://other.test", wantURL: "https://other.test"},
		{name: "silicon precedence", silicon: "sf", gateway: "gw", base: "https://gateway.test", wantKey: "sf", wantURL: "https://api.siliconflow.cn"},
		{name: "explicit provider", asr: "explicit", gateway: "gw", base: "https://gateway.test", override: "https://asr.test", wantKey: "explicit", wantURL: "https://asr.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"ASR_API_KEY", "ASR_BASE_URL", "SILICONFLOW_API_KEY", "EA_API_KEY", "OPENAI_API_KEY", "EA_BASE_URL", "OPENAI_BASE_URL"} {
				t.Setenv(name, "")
			}
			t.Setenv("ASR_API_KEY", tc.asr)
			t.Setenv("ASR_BASE_URL", tc.override)
			t.Setenv("SILICONFLOW_API_KEY", tc.silicon)
			c := Default()
			c.OpenAIAPIKey, c.OpenAIBaseURL = tc.gateway, tc.base
			c.LoadFromEnv()
			assert.Equal(t, tc.wantKey, c.ASRAPIKey)
			assert.Equal(t, tc.wantURL, c.ASRBaseURL)
		})
	}
}

func TestASRPreservesFileConfiguration(t *testing.T) {
	for _, name := range []string{"ASR_API_KEY", "ASR_BASE_URL", "ASR_MODEL", "SILICONFLOW_API_KEY"} {
		t.Setenv(name, "")
	}
	c := Default()
	c.ASRAPIKey = "file-key"
	c.ASRBaseURL = "https://file.test/v1"
	c.ASRModel = "file-model"
	c.LoadFromEnv()
	assert.Equal(t, "file-key", c.ASRAPIKey)
	assert.Equal(t, "https://file.test/v1", c.ASRBaseURL)
	assert.Equal(t, "file-model", c.ASRModel)
}
