package sessionmgr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/session"
)

// Manager manages session persistence and indexing.
// It handles file-based session storage: create, open, fork, list, delete.
// This is the storage/indexing layer — NOT the runtime behavior layer.
type Manager struct {
	dataDir string
}

// SessionInfo holds metadata about a session for listing/indexing.
type SessionInfo struct {
	ID           string `json:"id"`
	CreatedAt    int64  `json:"created_at"`
	MessageCount int    `json:"message_count"`
	LastActive   int64  `json:"last_active"`
	Workspace    string `json:"workspace,omitempty"`
	Title        string `json:"title,omitempty"`
	Application  string `json:"application,omitempty"` // e.g. "coding", "music", "kb"
}

// NewManager creates a new session manager rooted at dataDir.
// Sessions are stored under {dataDir}/sessions/{sessionID}/session.jsonl.
func NewManager(dataDir string) *Manager {
	return &Manager{dataDir: dataDir}
}

// SessionsDir returns the directory containing all sessions.
func (m *Manager) SessionsDir() string {
	return filepath.Join(m.dataDir, "sessions")
}

// SessionPath returns the JSONL file path for a given session ID.
func (m *Manager) SessionPath(id string) string {
	return filepath.Join(m.SessionsDir(), id, "session.jsonl")
}

// Create creates a new session directory and initializes the storage.
// Returns the session ID and the session file path.
func (m *Manager) Create(ctx context.Context) (string, string, error) {
	id := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	sessionDir := filepath.Join(m.SessionsDir(), id)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create session dir: %w", err)
	}

	sessionPath := m.SessionPath(id)
	storage := session.NewJSONLStorage(sessionPath)
	if err := storage.Init(); err != nil {
		return "", "", fmt.Errorf("init session storage: %w", err)
	}
	storage.Close()

	return id, sessionPath, nil
}

// SaveMeta writes session metadata (e.g. workspace, application) to meta.json in the session directory.
func (m *Manager) SaveMeta(sessionID string, workspace string, application string) error {
	sessionDir := filepath.Join(m.SessionsDir(), sessionID)
	metaPath := filepath.Join(sessionDir, "meta.json")
	meta := map[string]string{"workspace": workspace}
	if application != "" {
		meta["application"] = application
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// readMeta reads session metadata from meta.json if it exists.
func readMeta(sessionDir string) (workspace string, application string) {
	metaPath := filepath.Join(sessionDir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return "", ""
	}
	var meta struct {
		Workspace   string `json:"workspace"`
		Application string `json:"application"`
	}
	if json.Unmarshal(data, &meta) == nil {
		return meta.Workspace, meta.Application
	}
	return "", ""
}

// Metadata restores the workspace and application that own a persisted session.
func (m *Manager) Metadata(id string) (workspace, application string) {
	return readMeta(filepath.Join(m.SessionsDir(), id))
}

// Open opens an existing session by ID.
// Returns the Session object and the session file path.
func (m *Manager) Open(ctx context.Context, id string) (*session.Session, string, error) {
	sessionPath := m.SessionPath(id)

	// Check existence
	sessionDir := filepath.Join(m.SessionsDir(), id)
	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("session %q not found: %w", id, err)
	}

	storage := session.NewJSONLStorage(sessionPath)
	if err := storage.Init(); err != nil {
		return nil, "", fmt.Errorf("init storage for session %q: %w", id, err)
	}

	sess := session.New(storage)
	if err := sess.InitFromStorage(ctx); err != nil {
		storage.Close()
		return nil, "", fmt.Errorf("init session from storage: %w", err)
	}

	return sess, sessionPath, nil
}

// Fork creates a new session by copying an existing session file
// and optionally branching at a specific entry.
// Returns the new session ID and path.
func (m *Manager) Fork(ctx context.Context, sourceID string, entryID string) (string, string, error) {
	sourcePath := m.SessionPath(sourceID)
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("source session %q not found", sourceID)
	}

	// Create new session
	newID, newPath, err := m.Create(ctx)
	if err != nil {
		return "", "", err
	}

	// Copy source JSONL to new session
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("read source session: %w", err)
	}
	if err := os.WriteFile(newPath, data, 0o644); err != nil {
		return "", "", fmt.Errorf("write forked session: %w", err)
	}

	// If entryID specified, set leaf to that entry
	if entryID != "" {
		storage := session.NewJSONLStorage(newPath)
		if err := storage.Init(); err != nil {
			return "", "", fmt.Errorf("init forked storage: %w", err)
		}
		sess := session.New(storage)
		if err := sess.InitFromStorage(ctx); err != nil {
			storage.Close()
			return "", "", fmt.Errorf("init forked session: %w", err)
		}
		if err := sess.MoveTo(ctx, entryID, ""); err != nil {
			storage.Close()
			return "", "", fmt.Errorf("move forked session to entry %q: %w", entryID, err)
		}
		storage.Close()
	}

	return newID, newPath, nil
}

// List returns metadata for all sessions, sorted by LastActive descending.
func (m *Manager) List(ctx context.Context) ([]SessionInfo, error) {
	sessionsDir := m.SessionsDir()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var infos []SessionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		id := entry.Name()
		sessionPath := m.SessionPath(id)

		info := SessionInfo{
			ID: id,
		}

		// Get dir mod time as created at
		if fi, err := entry.Info(); err == nil {
			info.CreatedAt = fi.ModTime().Unix()
			info.LastActive = fi.ModTime().Unix()
		}

		// Read workspace and application from meta.json
		info.Workspace, info.Application = readMeta(filepath.Join(sessionsDir, id))

		// Count messages and extract title by scanning JSONL
		msgCount, lastActive, firstUserMsg, err := countMessages(sessionPath)
		if err == nil {
			info.MessageCount = msgCount
			if lastActive > info.LastActive {
				info.LastActive = lastActive
			}
			info.Title = firstUserMsg
		}

		infos = append(infos, info)
	}

	// Sort by LastActive descending
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].LastActive > infos[j].LastActive
	})

	return infos, nil
}

// Delete removes a session directory entirely.
func (m *Manager) Delete(id string) error {
	sessionDir := filepath.Join(m.SessionsDir(), id)
	if err := os.RemoveAll(sessionDir); err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	return nil
}

// Exists checks whether a session with the given ID exists.
func (m *Manager) Exists(id string) bool {
	sessionDir := filepath.Join(m.SessionsDir(), id)
	fi, err := os.Stat(sessionDir)
	return err == nil && fi.IsDir()
}

// countMessages counts message entries in a JSONL file.
// It only counts entries with Type == "message", not leaf/compaction entries.
// Also returns the latest timestamp found and the first user message content (for title).
func countMessages(path string) (int, int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	defer file.Close()

	count := 0
	var lastTS int64
	var firstUserMsg string

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large messages
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			Type string `json:"type"`
			TS   int64  `json:"timestamp"`
			// User messages have user.content as an array of {type, text}
			User *struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"user,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "message" {
			count++
			if entry.User != nil && firstUserMsg == "" {
				for _, c := range entry.User.Content {
					if c.Type == "text" && c.Text != "" {
						firstUserMsg = c.Text
						break
					}
				}
			}
		}
		if entry.TS > lastTS {
			lastTS = entry.TS
		}
	}
	// 会话条目时间戳是毫秒（session.UnixMilli），归一化为秒，
	// 与 List 中目录 ModTime().Unix() 保持同一单位
	if lastTS > 1e12 {
		lastTS /= 1000
	}
	return count, lastTS, firstUserMsg, scanner.Err()
}
