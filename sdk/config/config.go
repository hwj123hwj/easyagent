package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ansiEscapeRegex matches complete ANSI escape sequences so they can be
// stripped from config values. Covers:
//   - OSC sequences: ESC ] ... BEL            (e.g. \x1b]11;rgb:...\x07)
//   - CSI sequences: ESC [ params... letter    (e.g. \x1b[A, \x1b[2;3H)
//   - Incomplete CSI opener: ESC [             (leftover when only ESC+[ leaked)
var ansiEscapeRegex = regexp.MustCompile(`\x1b\][^\x07]*\x07|\x1b\[[0-9;?<=>]*[a-zA-Z]|\x1b\[`)

// sanitizeConfigString strips ANSI escape sequences and other non-printable
// control characters from configuration string values.
//
// Root cause of the infamous "\x1b[A\x1b[/v1/chat/completions" bug:
// When a user presses arrow keys during interactive input (e.g. during
// install.sh's read prompts, or in the TUI before the fix), the raw ESC
// byte (0x1B) and subsequent CSI sequences can get written into .env
// files or environment variables. These control characters are invisible
// in most editors but cause URL parse failures and other subtle bugs.
//
// This function strips complete ANSI escape sequences FIRST (using regex),
// then removes any remaining stray control characters.
func sanitizeConfigString(s string) string {
	// Step 1: Remove complete ANSI escape sequences (ESC + printable chars)
	s = ansiEscapeRegex.ReplaceAllString(s, "")
	// Step 2: Remove any remaining individual control characters
	var b strings.Builder
	for _, r := range s {
		if r == '\t' || r >= 0x20 && r != 0x7F {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type Config struct {
	Name     string
	Host     string
	Port     int
	DataDir  string
	MaxTurns int
	Timeout  time.Duration

	// Provider
	Provider string // anthropic / openai

	// Anthropic
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicBaseURL string

	// OpenAI-compatible
	OpenAIAPIKey  string
	OpenAIModel   string
	OpenAIBaseURL string

	// Tool sandbox
	AllowOutsideWorkspace  bool // Explicit file-path policy; independent of confirmation.
	Workspace              string
	EnableBash             bool
	AutoApprove            bool // 全权模式：跳过危险工具的 y/n 确认（信得过自己环境再开）
	EnableWeb              bool
	WebTimeoutSeconds      int
	EnableWebSearch        bool
	EnableComputerUse      bool     // experimental: expose the computer-use tool
	ComputerApprovalPolicy string   // computer perform_action 审批策略: "ask"(默认) | "auto"(仅高危确认)
	ApprovedApps           []string // 已批准执行键鼠控制的应用（bundle id 或名称）
	MCPConfigPath          string   // User MCP configuration; empty uses ~/.easyagent/mcp.json.

	// Execution backend
	ExecutionMode string // "local" (default) or "ssh"
	SSHHost       string // user@host for SSH mode
	SSHPort       int    // SSH port (default 22)
	SSHWorkDir    string // Remote working directory for SSH mode

	// Tool output
	MaxOutputLen int

	// Tool filtering
	AllowedTools []string
	BlockedTools []string

	// Prompt
	PromptTemplate string

	// Knowledge base (second-brain)
	KBRepoPath string // path to the personal knowledge repo (default: ~/agent-lessons)

	// KB vector search (optional, for semantic search)
	KBEmbeddingAPIKey  string // API key for embedding provider (e.g. SiliconFlow)
	KBEmbeddingModel   string // embedding model name (e.g. BAAI/bge-m3)
	KBEmbeddingBaseURL string // embedding API base URL

	// ASR (speech-to-text)
	ASRAPIKey  string // API key for SiliconFlow ASR
	ASRModel   string // ASR model name (default: TeleAI/TeleSpeechASR)
	ASRBaseURL string // ASR API base URL (default: https://api.siliconflow.cn)

	// Skill marketplace (EasyCode official skill store)
	SkillMarketEnabled  bool   // expose skill_market tool and REST endpoints (default: true)
	SkillMarketBaseURL  string // store endpoint (default: https://skills.deepvlab.ai)
	SkillMarketSkillDir string // local install root (default: ~/.agents/skills)

	// Server security
	APIKey string // Bearer token for HTTP API auth (empty = no auth, backward compatible)
}

// HomeDirName is the directory name used for persistent EasyAgent user data.
const HomeDirName = ".easyagent"

func Default() Config {
	return Config{
		Name:     "easyagent",
		Host:     "127.0.0.1",
		Port:     8080,
		DataDir:  "./data",
		MaxTurns: 200,
		Timeout:  5 * time.Minute,

		Provider: "",

		AnthropicBaseURL: "https://api.anthropic.com",

		OpenAIAPIKey:  "sk-local-gateway-hwj123hwj",
		OpenAIBaseURL: "http://localhost:4001",
		OpenAIModel:   "longcat-opus",

		Workspace:              "", // empty = use cwd
		EnableBash:             false,
		EnableWeb:              false,
		WebTimeoutSeconds:      30,
		EnableWebSearch:        false,
		EnableComputerUse:      false, // opt-in experimental
		ComputerApprovalPolicy: "ask",

		MaxOutputLen: 30000,

		KBRepoPath:         "", // empty → defaults to ~/agent-lessons at runtime
		SkillMarketEnabled: true,
	}
}

// HomeDir returns the user's persistent EasyAgent data directory.
func HomeDir() string {
	if dir := os.Getenv("EA_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return HomeDirName
	}
	return filepath.Join(home, HomeDirName)
}

// ServerAPIKey separates the local client credential from the upstream model
// key while retaining EA_API_KEY compatibility for existing deployments.
func ServerAPIKey() string {
	if value := Env("EA_SERVER_API_KEY"); value != "" {
		return sanitizeConfigString(value)
	}
	return sanitizeConfigString(Env("EA_API_KEY"))
}

// Env reads an EasyAgent-prefixed environment variable (EA_*).
func Env(name string) string {
	return os.Getenv(name)
}

func getEnv(name, fallback string) string {
	if value := Env(name); value != "" {
		return value
	}
	return fallback
}

func getEnvBool(name string, fallback bool) bool {
	value := Env(name)
	if value == "" {
		return fallback
	}
	return strings.EqualFold(value, "true") || value == "1"
}

func getEnvInt(name string, fallback int) int {
	value := Env(name)
	if parsed, err := strconv.Atoi(value); value != "" && err == nil {
		return parsed
	}
	return fallback
}

// LoadFromEnv 从环境变量中加载配置，覆盖默认值。
func (c *Config) LoadFromEnv() {
	if v := getEnv("EA_PROVIDER", ""); v != "" {
		c.Provider = v
	}
	if v := getEnv("EA_HOST", ""); v != "" {
		c.Host = v
	}
	if v := getEnvInt("EA_PORT", 0); v > 0 {
		c.Port = v
	}
	if v := getEnv("EA_DATA_DIR", ""); v != "" {
		c.DataDir = v
	}
	if v := getEnv("EA_WORKSPACE", ""); v != "" {
		c.Workspace = v
	}
	if v := getEnv("EA_MCP_CONFIG", ""); v != "" {
		c.MCPConfigPath = v
	}
	c.EnableBash = getEnvBool("EA_ENABLE_BASH", c.EnableBash)
	c.AutoApprove = getEnvBool("EA_AUTO_APPROVE", c.AutoApprove)
	c.AllowOutsideWorkspace = getEnvBool("EA_ALLOW_OUTSIDE_WORKSPACE", c.AllowOutsideWorkspace)
	c.EnableWeb = getEnvBool("EA_ENABLE_WEB", c.EnableWeb)
	c.WebTimeoutSeconds = getEnvInt("EA_WEB_TIMEOUT_SECONDS", c.WebTimeoutSeconds)
	c.EnableWebSearch = getEnvBool("EA_ENABLE_WEB_SEARCH", c.EnableWebSearch)
	c.EnableComputerUse = getEnvBool("EA_ENABLE_COMPUTER_USE", c.EnableComputerUse)
	if p := os.Getenv("EA_COMPUTER_APPROVAL_POLICY"); p == "auto" || p == "ask" {
		c.ComputerApprovalPolicy = p
	}

	// Anthropic
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		c.AnthropicAPIKey = sanitizeConfigString(v)
	}
	if v := os.Getenv("ANTHROPIC_MODEL"); v != "" {
		c.AnthropicModel = sanitizeConfigString(v)
	}
	if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "" {
		c.AnthropicBaseURL = sanitizeConfigString(v)
	}

	// OpenAI-compatible gateway
	// EA_API_KEY is preferred; OPENAI_API_KEY remains accepted.
	if v := getEnv("EA_API_KEY", ""); v != "" {
		c.OpenAIAPIKey = sanitizeConfigString(v)
	} else if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		c.OpenAIAPIKey = sanitizeConfigString(v)
	}
	if v := getEnv("EA_MODEL", ""); v != "" {
		c.OpenAIModel = sanitizeConfigString(v)
	} else if v := os.Getenv("OPENAI_MODEL"); v != "" {
		c.OpenAIModel = sanitizeConfigString(v)
	}
	if v := getEnv("EA_BASE_URL", ""); v != "" {
		c.OpenAIBaseURL = sanitizeConfigString(v)
	} else if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		c.OpenAIBaseURL = sanitizeConfigString(v)
	}

	// Tool output
	if n := getEnvInt("EA_MAX_OUTPUT_LEN", c.MaxOutputLen); n > 0 {
		c.MaxOutputLen = n
	}

	// Execution backend
	if v := getEnv("EA_EXECUTION_MODE", ""); v != "" {
		c.ExecutionMode = v
	}
	if v := getEnv("EA_SSH_HOST", ""); v != "" {
		c.SSHHost = v
	}
	if p := getEnvInt("EA_SSH_PORT", c.SSHPort); p > 0 {
		c.SSHPort = p
	}
	if v := getEnv("EA_SSH_WORKDIR", ""); v != "" {
		c.SSHWorkDir = v
	}

	// Tool filtering
	if v := getEnv("EA_ALLOWED_TOOLS", ""); v != "" {
		c.AllowedTools = strings.Split(v, ",")
	}
	if v := getEnv("EA_BLOCKED_TOOLS", ""); v != "" {
		c.BlockedTools = strings.Split(v, ",")
	}

	// Prompt
	if v := getEnv("EA_PROMPT_TEMPLATE", ""); v != "" {
		c.PromptTemplate = v
	}

	// Knowledge base
	if v := getEnv("EA_KB_REPO_PATH", ""); v != "" {
		c.KBRepoPath = v
	}

	// Skill marketplace
	c.SkillMarketEnabled = getEnvBool("EA_SKILL_MARKET", c.SkillMarketEnabled)
	if v := getEnv("EA_SKILL_MARKET_BASE_URL", ""); v != "" {
		c.SkillMarketBaseURL = sanitizeConfigString(v)
	}
	if v := getEnv("EA_SKILL_MARKET_SKILL_DIR", ""); v != "" {
		c.SkillMarketSkillDir = v
	}

	// KB vector search
	if v := os.Getenv("SILICONFLOW_API_KEY"); v != "" {
		c.KBEmbeddingAPIKey = v
	}
	if v := os.Getenv("SILICONFLOW_EMBEDDING_MODEL"); v != "" {
		c.KBEmbeddingModel = v
	}
	if v := os.Getenv("SILICONFLOW_BASE_URL"); v != "" {
		c.KBEmbeddingBaseURL = v
	}

	// ASR: credentials and endpoint must come from the same provider. Never
	// forward a gateway key to the default SiliconFlow endpoint (or vice versa).
	asrKey, siliconKey, asrURL := os.Getenv("ASR_API_KEY"), os.Getenv("SILICONFLOW_API_KEY"), os.Getenv("ASR_BASE_URL")
	savedASRKey, savedASRURL := c.ASRAPIKey, c.ASRBaseURL
	c.ASRBaseURL = "https://api.siliconflow.cn"
	if asrURL != "" {
		c.ASRBaseURL = asrURL
	}
	if asrKey != "" {
		c.ASRAPIKey = asrKey
	} else if savedASRKey != "" && asrURL == "" {
		c.ASRAPIKey = savedASRKey
		if savedASRURL != "" {
			c.ASRBaseURL = savedASRURL
		}
	} else if siliconKey != "" && asrURL == "" {
		c.ASRAPIKey = siliconKey
	} else if asrURL == "" && c.OpenAIAPIKey != "" && c.OpenAIBaseURL != "" {
		c.ASRAPIKey, c.ASRBaseURL = c.OpenAIAPIKey, c.OpenAIBaseURL
	} else {
		c.ASRAPIKey = ""
	}
	if v := os.Getenv("ASR_MODEL"); v != "" {
		c.ASRModel = v
	} else if c.ASRModel == "" {
		c.ASRModel = "TeleAI/TeleSpeechASR"
	}

	// Server security
	if v := getEnv("EA_API_KEY", ""); v != "" {
		c.APIKey = sanitizeConfigString(v)
	}
	if v := getEnv("EA_SERVER_API_KEY", ""); v != "" {
		c.APIKey = sanitizeConfigString(v)
	}
}

// LoadDotEnv 从 .env 文件读取键值对，设置到环境变量中。
// 已有的环境变量不会被覆盖（即环境变量优先级 > .env 文件）。
func LoadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// Strip ANSI escape codes from .env values — prevents the
		// "\x1b...http://localhost:4001/v1/chat/completions" URL bug.
		value = sanitizeConfigString(value)
		// 不覆盖已有环境变量
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}

// yamlConfig is the YAML representation of Config for file-based configuration.
// Uses yaml tags so users can write easyagent.yaml (legacy pi-go.yaml is still accepted).
type yamlConfig struct {
	AllowOutsideWorkspace bool   `yaml:"allow_outside_workspace,omitempty"`
	Name                  string `yaml:"name,omitempty"`
	Host                  string `yaml:"host,omitempty"`
	Port                  int    `yaml:"port,omitempty"`
	DataDir               string `yaml:"data_dir,omitempty"`

	Provider string `yaml:"provider,omitempty"`

	AnthropicAPIKey  string `yaml:"anthropic_api_key,omitempty"`
	AnthropicModel   string `yaml:"anthropic_model,omitempty"`
	AnthropicBaseURL string `yaml:"anthropic_base_url,omitempty"`

	OpenAIAPIKey  string `yaml:"openai_api_key,omitempty"`
	OpenAIModel   string `yaml:"openai_model,omitempty"`
	OpenAIBaseURL string `yaml:"openai_base_url,omitempty"`

	Workspace         string `yaml:"workspace,omitempty"`
	MCPConfigPath     string `yaml:"mcp_config,omitempty"`
	EnableBash        bool   `yaml:"enable_bash,omitempty"`
	AutoApprove       bool   `yaml:"auto_approve,omitempty"`
	EnableWeb         bool   `yaml:"enable_web,omitempty"`
	WebTimeoutSeconds int    `yaml:"web_timeout_seconds,omitempty"`
	EnableWebSearch   bool   `yaml:"enable_web_search,omitempty"`

	ExecutionMode string `yaml:"execution_mode,omitempty"`
	SSHHost       string `yaml:"ssh_host,omitempty"`
	SSHPort       int    `yaml:"ssh_port,omitempty"`
	SSHWorkDir    string `yaml:"ssh_work_dir,omitempty"`

	MaxOutputLen int      `yaml:"max_output_len,omitempty"`
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
	BlockedTools []string `yaml:"blocked_tools,omitempty"`

	MaxTurns int           `yaml:"max_turns,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`

	APIKey string `yaml:"api_key,omitempty"`

	KBRepoPath         string `yaml:"kb_repo_path,omitempty"`
	KBEmbeddingAPIKey  string `yaml:"kb_embedding_api_key,omitempty"`
	KBEmbeddingModel   string `yaml:"kb_embedding_model,omitempty"`
	KBEmbeddingBaseURL string `yaml:"kb_embedding_base_url,omitempty"`

	SkillMarketBaseURL  string `yaml:"skill_market_base_url,omitempty"`
	SkillMarketSkillDir string `yaml:"skill_market_skill_dir,omitempty"`

	ASRAPIKey  string `yaml:"asr_api_key,omitempty"`
	ASRModel   string `yaml:"asr_model,omitempty"`
	ASRBaseURL string `yaml:"asr_base_url,omitempty"`
}

// LoadFromYAML loads configuration from a YAML file.
// This is the lowest-priority source: values here are overridden by env vars.
// Call this BEFORE LoadFromEnv for correct precedence.
func (c *Config) LoadFromYAML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var yc yamlConfig
	if err := yaml.Unmarshal(data, &yc); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	// Apply YAML values only if non-zero (env vars override later)
	if yc.Name != "" {
		c.Name = yc.Name
	}
	if yc.Host != "" {
		c.Host = yc.Host
	}
	if yc.Port != 0 {
		c.Port = yc.Port
	}
	if yc.DataDir != "" {
		c.DataDir = yc.DataDir
	}
	if yc.Provider != "" {
		c.Provider = yc.Provider
	}
	if yc.AnthropicAPIKey != "" {
		c.AnthropicAPIKey = yc.AnthropicAPIKey
	}
	if yc.AnthropicModel != "" {
		c.AnthropicModel = yc.AnthropicModel
	}
	if yc.AnthropicBaseURL != "" {
		c.AnthropicBaseURL = yc.AnthropicBaseURL
	}
	if yc.OpenAIAPIKey != "" {
		c.OpenAIAPIKey = yc.OpenAIAPIKey
	}
	if yc.OpenAIModel != "" {
		c.OpenAIModel = yc.OpenAIModel
	}
	if yc.OpenAIBaseURL != "" {
		c.OpenAIBaseURL = yc.OpenAIBaseURL
	}
	if yc.Workspace != "" {
		c.Workspace = yc.Workspace
	}
	if yc.MCPConfigPath != "" {
		c.MCPConfigPath = yc.MCPConfigPath
	}
	if yc.EnableBash {
		c.EnableBash = true
	}
	if yc.AllowOutsideWorkspace {
		c.AllowOutsideWorkspace = true
	}
	if yc.AutoApprove {
		c.AutoApprove = true
	}
	if yc.EnableWeb {
		c.EnableWeb = true
	}
	if yc.WebTimeoutSeconds > 0 {
		c.WebTimeoutSeconds = yc.WebTimeoutSeconds
	}
	if yc.EnableWebSearch {
		c.EnableWebSearch = true
	}
	if yc.ExecutionMode != "" {
		c.ExecutionMode = yc.ExecutionMode
	}
	if yc.SSHHost != "" {
		c.SSHHost = yc.SSHHost
	}
	if yc.SSHPort > 0 {
		c.SSHPort = yc.SSHPort
	}
	if yc.SSHWorkDir != "" {
		c.SSHWorkDir = yc.SSHWorkDir
	}
	if yc.MaxOutputLen > 0 {
		c.MaxOutputLen = yc.MaxOutputLen
	}
	if len(yc.AllowedTools) > 0 {
		c.AllowedTools = yc.AllowedTools
	}
	if len(yc.BlockedTools) > 0 {
		c.BlockedTools = yc.BlockedTools
	}
	if yc.MaxTurns > 0 {
		c.MaxTurns = yc.MaxTurns
	}
	if yc.Timeout > 0 {
		c.Timeout = yc.Timeout
	}
	if yc.APIKey != "" {
		c.APIKey = yc.APIKey
	}
	if yc.KBRepoPath != "" {
		c.KBRepoPath = yc.KBRepoPath
	}
	if yc.SkillMarketBaseURL != "" {
		c.SkillMarketBaseURL = yc.SkillMarketBaseURL
	}
	if yc.SkillMarketSkillDir != "" {
		c.SkillMarketSkillDir = yc.SkillMarketSkillDir
	}
	if yc.KBEmbeddingAPIKey != "" {
		c.KBEmbeddingAPIKey = yc.KBEmbeddingAPIKey
	}
	if yc.KBEmbeddingModel != "" {
		c.KBEmbeddingModel = yc.KBEmbeddingModel
	}
	if yc.KBEmbeddingBaseURL != "" {
		c.KBEmbeddingBaseURL = yc.KBEmbeddingBaseURL
	}
	if yc.ASRAPIKey != "" {
		c.ASRAPIKey = yc.ASRAPIKey
	}
	if yc.ASRModel != "" {
		c.ASRModel = yc.ASRModel
	}
	if yc.ASRBaseURL != "" {
		c.ASRBaseURL = yc.ASRBaseURL
	}

	return nil
}

// ─── Runtime overrides (settings.json) ──────────────────────────────────────
//
// Desktop settings toggles need persistence that survives restarts without
// editing the user's yaml or env files. runtimeOverrides is a small
// machine-written overlay stored in <dataDir>/settings.json; this overlay is applied after
// boot-time env and yaml sources so saved runtime changes survive restarts.

type runtimeOverrides struct {
	EnableComputerUse      *bool    `json:"enable_computer_use,omitempty"`
	ComputerApprovalPolicy *string  `json:"computer_approval_policy,omitempty"`
	ApprovedApps           []string `json:"approved_apps,omitempty"`
}

// LoadRuntimeOverrides applies <dataDir>/settings.json on top of c. Missing
// or malformed files are ignored (boot must never fail on settings).
func (c *Config) LoadRuntimeOverrides(dataDir string) {
	data, err := os.ReadFile(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		return
	}
	var o runtimeOverrides
	if json.Unmarshal(data, &o) != nil {
		return
	}
	if o.EnableComputerUse != nil {
		c.EnableComputerUse = *o.EnableComputerUse
	}
	if o.ComputerApprovalPolicy != nil && (*o.ComputerApprovalPolicy == "ask" || *o.ComputerApprovalPolicy == "auto") {
		c.ComputerApprovalPolicy = *o.ComputerApprovalPolicy
	}
	c.ApprovedApps = o.ApprovedApps
}

// SaveRuntimeOverrideComputerPolicy persists the approval policy ("ask"/"auto").
func SaveRuntimeOverrideComputerPolicy(dataDir, policy string) error {
	if policy != "ask" && policy != "auto" {
		return fmt.Errorf("非法审批策略 %q（仅 ask/auto）", policy)
	}
	return mutateRuntimeOverrides(dataDir, func(o *runtimeOverrides) {
		o.ComputerApprovalPolicy = &policy
	})
}

// RecordApprovedApp dedupe-appends an app to the approved list (cap 50).
func RecordApprovedApp(dataDir, app string) error {
	if app == "" {
		return nil
	}
	return mutateRuntimeOverrides(dataDir, func(o *runtimeOverrides) {
		for _, existing := range o.ApprovedApps {
			if existing == app {
				return
			}
		}
		o.ApprovedApps = append(o.ApprovedApps, app)
		if len(o.ApprovedApps) > 50 {
			o.ApprovedApps = o.ApprovedApps[len(o.ApprovedApps)-50:]
		}
	})
}

// ClearApprovedApps empties the approved-apps list.
func ClearApprovedApps(dataDir string) error {
	return mutateRuntimeOverrides(dataDir, func(o *runtimeOverrides) {
		o.ApprovedApps = []string{}
	})
}

// runtimeOverridesMu serializes read-modify-write updates in this server.
var runtimeOverridesMu sync.Mutex

// mutateRuntimeOverrides writes an atomic overlay, retaining unrelated keys.
func mutateRuntimeOverrides(dataDir string, mutate func(o *runtimeOverrides)) error {
	runtimeOverridesMu.Lock()
	defer runtimeOverridesMu.Unlock()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dataDir, "settings.json")
	raw := map[string]json.RawMessage{}
	o := runtimeOverrides{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal(data, &o); err != nil {
			return err
		}
		if raw == nil {
			return fmt.Errorf("settings.json must be an object")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	mutate(&o)
	known, err := json.Marshal(o)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(known, &fields); err != nil {
		return err
	}
	for _, key := range []string{"enable_computer_use", "computer_approval_policy", "approved_apps"} {
		delete(raw, key)
	}
	for key, value := range fields {
		raw[key] = value
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dataDir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// UpdateComputerRuntimeSettings validates and persists one settings request.
func UpdateComputerRuntimeSettings(dataDir string, enabled *bool, policy *string, clearApps bool) error {
	if policy != nil && *policy != "ask" && *policy != "auto" {
		return fmt.Errorf("非法审批策略 %q（仅 ask/auto）", *policy)
	}
	return mutateRuntimeOverrides(dataDir, func(o *runtimeOverrides) {
		if enabled != nil {
			o.EnableComputerUse = enabled
		}
		if policy != nil {
			o.ComputerApprovalPolicy = policy
		}
		if clearApps {
			o.ApprovedApps = []string{}
		}
	})
}

// SaveRuntimeOverrideComputerUse persists the computer-use toggle only.
func SaveRuntimeOverrideComputerUse(dataDir string, enabled bool) error {
	return UpdateComputerRuntimeSettings(dataDir, &enabled, nil, false)
}
