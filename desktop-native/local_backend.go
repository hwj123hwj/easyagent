package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type secretStore interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}
type hostConfig struct {
	ID, Name, URL, Workspace string
	Local                    bool
	HasToken                 bool
}
type modelConfig struct {
	Mode, Provider, BaseURL, Model string
	HasKey                         bool
}
type clientConfig struct {
	Hosts      []hostConfig
	ActiveHost string
	Model      modelConfig
	Workspace  string
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".native-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_ = f.Chmod(0600)
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

type localBackend struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	done   chan struct{}
	client *backendClient
	closed bool
}

func coreBinary() (string, error) {
	if raw := os.Getenv("EASYAGENT_NATIVE_CORE"); raw != "" {
		if info, err := os.Stat(raw); err == nil && !info.IsDir() {
			return raw, nil
		}
		return "", errors.New("EASYAGENT_NATIVE_CORE 指向的核心不存在")
	}
	exe, _ := os.Executable()
	candidate := filepath.Clean(filepath.Join(filepath.Dir(exe), "../Resources/easyagent"))
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	if raw, err := exec.LookPath("easyagent"); err == nil {
		return raw, nil
	}
	return "", errors.New("安装包缺少 EasyAgent 核心，请重新构建原生试用版")
}
func filterEnvironment(overrides map[string]string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[key]; !ok {
			env = append(env, entry)
		}
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}
func (m *localBackend) start(ctx context.Context, dir string, model modelConfig, secrets secretStore) (*backendClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, context.Canceled
	}
	if m.client != nil {
		select {
		case <-m.done:
			m.client = nil
			m.cmd = nil
		default:
			return m.client, nil
		}
	}
	binary, err := coreBinary()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := listener.Addr().String()
	_ = listener.Close()
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(random[:])
	if err = os.MkdirAll(filepath.Join(dir, "core-data"), 0700); err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	env := map[string]string{"EA_DATA_DIR": filepath.Join(dir, "core-data"), "EA_SERVER_API_KEY": token, "EA_ALLOW_NO_AUTH": "0", "EA_ALLOWED_ORIGINS": "", "EA_ENABLE_TERMINAL": "1", "EA_HOME": filepath.Join(dir, "core-home"), "EA_MCP_CONFIG": filepath.Join(dir, "core-home", "mcp.json")}
	for k, v := range inheritedCoreEnvironment(home) {
		if _, fixed := env[k]; !fixed && os.Getenv(k) == "" {
			env[k] = v
		}
	}
	if os.Getenv("EA_ENV_FILE") == "" {
		env["EA_ENV_FILE"] = filepath.Join(home, ".easyagent", ".env")
	}
	if os.Getenv("EA_WORKSPACE") == "" && env["EA_WORKSPACE"] == "" {
		env["EA_WORKSPACE"] = home
	}
	if model.Mode == "override" {
		key, err := secrets.Get("model")
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, errors.New("已保存的模型配置缺少密钥，请重新填写")
		}
		env["EA_PROVIDER"] = model.Provider
		if model.Provider == "anthropic" {
			env["ANTHROPIC_API_KEY"] = key
			env["ANTHROPIC_BASE_URL"] = model.BaseURL
			env["ANTHROPIC_MODEL"] = model.Model
		} else {
			env["EA_API_KEY"] = key
			env["EA_BASE_URL"] = model.BaseURL
			env["EA_MODEL"] = model.Model
			env["OPENAI_API_KEY"] = key
			env["OPENAI_BASE_URL"] = model.BaseURL
			env["OPENAI_MODEL"] = model.Model
		}
	}
	resources := filepath.Clean(filepath.Join(filepath.Dir(binary)))
	if _, err := os.Stat(filepath.Join(resources, "workflow-runtime.mjs")); err == nil {
		env["EA_WORKFLOW_RUNTIME"] = filepath.Join(resources, "workflow-runtime.mjs")
	}
	if _, err := os.Stat(filepath.Join(resources, "runtime/bin")); err == nil {
		env["PATH"] = filepath.Join(resources, "runtime/bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	cmd := exec.Command(binary, "serve", "--listen", address)
	cmd.Dir = home
	cmd.Env = filterEnvironment(env)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return nil, errors.New("无法启动本地 EasyAgent 核心")
	}
	m.cmd = cmd
	m.done = make(chan struct{})
	done := m.done
	go func() { _ = cmd.Wait(); close(done) }()
	ready := false
	defer func() {
		if !ready {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			m.cmd = nil
			m.client = nil
		}
	}()
	client, _ := newBackendClient("http://"+address, token)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			return nil, ctx.Err()
		case <-done:
			m.cmd = nil
			return nil, errors.New("本地核心已退出，请检查模型配置后重试")
		case <-deadline.C:
			_ = cmd.Process.Signal(os.Interrupt)
			return nil, errors.New("本地核心 15 秒内未就绪")
		case <-tick.C:
			probe, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			req, _ := http.NewRequestWithContext(probe, "GET", client.URL+"/health", nil)
			res, err := client.HTTP.Do(req)
			cancel()
			if err == nil {
				_ = res.Body.Close()
				if res.StatusCode == 200 {
					m.client = client
					ready = true
					return client, nil
				}
			}
		}
	}
}
func (m *localBackend) stop() {
	m.mu.Lock()
	cmd, done := m.cmd, m.done
	m.cmd = nil
	m.client = nil
	m.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(16 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}
func (m *localBackend) close() { m.mu.Lock(); m.closed = true; m.mu.Unlock(); m.stop() }
func validatedModel(cfg modelConfig) (modelConfig, error) {
	if cfg.Mode == "inherit" {
		return modelConfig{Mode: "inherit"}, nil
	}
	if cfg.Mode != "override" || (cfg.Provider != "openai" && cfg.Provider != "anthropic") {
		return cfg, errors.New("请选择有效的模型服务类型")
	}
	b, err := newBackendClient(cfg.BaseURL, "")
	if err != nil {
		return cfg, err
	}
	cfg.BaseURL = strings.TrimSuffix(strings.TrimRight(b.URL, "/"), "/v1")
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" || len(cfg.Model) > 200 || strings.ContainsAny(cfg.Model, "\r\n\t") {
		return cfg, errors.New("请输入有效的模型 ID")
	}
	return cfg, nil
}
func modelEnvironmentDescription(cfg modelConfig) string {
	if cfg.Mode != "override" {
		return "沿用命令行配置"
	}
	return fmt.Sprintf("%s · %s", cfg.Provider, cfg.Model)
}

// Read the regular CLI's model/tool defaults into the child environment only.
// The native core's writable home, receipts and MCP configuration remain private.
func inheritedCoreEnvironment(home string) map[string]string {
	result := map[string]string{}
	var values map[string]any
	if data, err := os.ReadFile(filepath.Join(home, ".easyagent", "config.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &values)
	}
	fields := map[string]string{"workspace": "EA_WORKSPACE", "provider": "EA_PROVIDER", "openai_api_key": "OPENAI_API_KEY", "openai_base_url": "OPENAI_BASE_URL", "openai_model": "OPENAI_MODEL", "anthropic_api_key": "ANTHROPIC_API_KEY", "anthropic_base_url": "ANTHROPIC_BASE_URL", "anthropic_model": "ANTHROPIC_MODEL", "enable_bash": "EA_ENABLE_BASH", "enable_web": "EA_ENABLE_WEB", "enable_web_search": "EA_ENABLE_WEB_SEARCH", "kb_repo_path": "EA_KB_REPO_PATH", "asr_api_key": "ASR_API_KEY", "asr_base_url": "ASR_BASE_URL", "asr_model": "ASR_MODEL"}
	for field, name := range fields {
		if v, ok := values[field]; ok {
			result[name] = fmt.Sprint(v)
		}
	}
	return result
}
func (m *localBackend) ensureIdle(ctx context.Context) error {
	m.mu.Lock()
	b := m.client
	m.mu.Unlock()
	if b == nil {
		return nil
	}
	var sessions []map[string]any
	if err := b.request(ctx, "GET", "/sessions", nil, &sessions); err != nil {
		return err
	}
	for _, item := range sessions {
		var snapshot envelope
		if err := b.request(ctx, "GET", sessionPath(str(item["id"]))+"/run", nil, &snapshot); err != nil {
			return err
		}
		if snapshot.Run.active() {
			return errors.New("本地核心仍有运行中的任务，请先停止任务再应用模型配置")
		}
	}
	return nil
}
