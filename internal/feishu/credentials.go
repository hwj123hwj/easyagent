package feishu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hwj123hwj/easyagent/sdk/config"
)

// Credentials holds the Feishu app credentials persisted to disk.
type Credentials struct {
	AppID            string `json:"app_id"`
	AppSecret        string `json:"app_secret"`
	UserOpenID       string `json:"user_open_id,omitempty"`
	UserAccessToken  string `json:"user_access_token,omitempty"`
	UserRefreshToken string `json:"user_refresh_token,omitempty"`
	BotName          string `json:"bot_name,omitempty"`
	BotOpenID        string `json:"bot_open_id,omitempty"`
	Platform         string `json:"platform,omitempty"` // "feishu" or "lark"
}

// credentialsPath returns the path to the feishu credentials file.
func credentialsPath() string {
	return filepath.Join(config.HomeDir(), "feishu-credentials.json")
}

// SaveCredentials writes feishu credentials to ~/.easyagent/feishu-credentials.json.
func SaveCredentials(creds Credentials) error {
	dir := filepath.Dir(credentialsPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	f, err := os.CreateTemp(dir, ".feishu-credentials-*")
	if err != nil {
		return fmt.Errorf("create credentials: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync credentials: %w", err)
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), credentialsPath()); err != nil {
		return fmt.Errorf("replace credentials: %w", err)
	}

	return nil
}

// LoadCredentials reads feishu credentials from disk.
// Returns nil if the file does not exist.
func LoadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	return &creds, nil
}

// DeleteCredentials removes the credentials file.
func DeleteCredentials() error {
	err := os.Remove(credentialsPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete credentials: %w", err)
	}
	return nil
}

// HasCredentials returns true if credentials file exists and has valid app_id/secret.
func HasCredentials() bool {
	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		return false
	}
	return creds.AppID != "" && creds.AppSecret != ""
}
