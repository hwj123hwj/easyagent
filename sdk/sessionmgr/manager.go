package sessionmgr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/session"
)

// Manager manages session persistence and indexing.
// It handles file-based session storage: create, open, fork, list, delete.
// This is the storage/indexing layer — NOT the runtime behavior layer.
type Manager struct {
	dataDir string
	metaMu  sync.Mutex
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
	Pinned       bool   `json:"pinned"`
	Archived     bool   `json:"archived"`
	ForkedFrom   string `json:"forked_from,omitempty"` // source session id when this session is a fork
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
	return m.updateMeta(sessionID, func(meta map[string]any) {
		meta["workspace"], meta["application"] = workspace, application
	})
}

// PreferencePatch changes only explicitly supplied indexing preferences.
type PreferencePatch struct {
	Title    *string `json:"title,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

func (m *Manager) UpdatePreferences(id string, patch PreferencePatch) error {
	if patch.Title != nil && (len([]rune(strings.TrimSpace(*patch.Title))) == 0 || len([]rune(*patch.Title)) > 160) {
		return fmt.Errorf("title must contain 1–160 characters")
	}
	return m.updateMeta(id, func(meta map[string]any) {
		if patch.Title != nil {
			meta["title"] = strings.TrimSpace(*patch.Title)
		}
		if patch.Pinned != nil {
			meta["pinned"] = *patch.Pinned
		}
		if patch.Archived != nil {
			meta["archived"] = *patch.Archived
		}
	})
}

func (m *Manager) updateMeta(id string, update func(map[string]any)) error {
	m.metaMu.Lock()
	defer m.metaMu.Unlock()
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("invalid session id")
	}
	dir := filepath.Join(m.SessionsDir(), id)
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("session directory is not a regular directory")
	}
	path := filepath.Join(dir, "meta.json")
	meta := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &meta); err != nil {
			return fmt.Errorf("read session metadata: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	update(meta)
	data, err = json.Marshal(meta)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".meta-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
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
//
// The fork inherits the source session's meta.json (workspace, application,
// title preferences) so it stays grouped under the same project, with a
// "(分叉)" title suffix and a forked_from marker pointing at the source.
// Attachment metadata is copied too, so prompts in the forked session can
// still reference attachments uploaded to the source conversation.
func (m *Manager) Fork(ctx context.Context, sourceID string, entryID string) (string, string, error) {
	var target *string
	if entryID != "" {
		target = &entryID
	}
	return m.ForkAt(ctx, sourceID, target)
}

// ForkAt copies the active branch. nil means the entire branch; an empty ID
// means an empty conversation. Retained compaction messages preserve the summary
// and the retained prefix, instead of resurrecting discarded tool outputs.
// The runtime caller must keep source writes idle during the snapshot.
func (m *Manager) ForkAt(ctx context.Context, sourceID string, entryID *string) (newID, newPath string, err error) {
	if sourceID == "" || strings.ContainsAny(sourceID, `/\\`) || sourceID == "." || sourceID == ".." {
		return "", "", fmt.Errorf("invalid source session")
	}
	storage := session.NewJSONLStorage(m.SessionPath(sourceID))
	if !m.Exists(sourceID) {
		return "", "", fmt.Errorf("source session not found")
	}
	if err = storage.Init(); err != nil {
		return
	}
	defer storage.Close()
	entries, err := storage.GetPathToRoot(ctx, "")
	if err != nil {
		return "", "", err
	}
	if entryID != nil {
		if *entryID == "" {
			entries = nil
		} else {
			cut := -1
			// Prefer the latest retained snapshot over the original pre-compaction entry.
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].Type != session.EntryTypeCompaction {
					continue
				}
				for j, retained := range entries[i].Retained {
					if retained.ID == *entryID {
						entries[i].Retained = append([]session.Entry(nil), entries[i].Retained[:j+1]...)
						cut = i + 1
						break
					}
				}
				break
			}
			if cut < 0 {
				for i, entry := range entries {
					if entry.ID == *entryID {
						cut = i + 1
						break
					}
				}
			}
			if cut < 0 {
				return "", "", fmt.Errorf("entry not found on active branch")
			}
			entries = entries[:cut]
		}
	}
	data := make([]byte, 0)
	for _, entry := range entries {
		encoded, encodeErr := json.Marshal(entry)
		if encodeErr != nil {
			return "", "", encodeErr
		}
		data = append(data, encoded...)
		data = append(data, '\n')
	}
	newID, newPath, err = m.Create(ctx)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			_ = m.Delete(newID)
		}
	}()
	if err = os.WriteFile(newPath, data, 0600); err != nil {
		return
	}
	if err = m.copyMeta(sourceID, newID); err != nil {
		return
	}
	err = m.copyAttachments(sourceID, newID)
	return
}

// copyMeta clones the source meta.json into the forked session, appending a
// "(分叉)" title and recording the source id as forked_from. Missing source
// metadata is tolerated: the fork simply starts with defaults.
func (m *Manager) copyMeta(sourceID, newID string) error {
	m.metaMu.Lock()
	defer m.metaMu.Unlock()
	meta := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(m.SessionsDir(), sourceID, "meta.json")); err == nil {
		if json.Unmarshal(data, &meta) != nil {
			meta = map[string]any{}
		}
	}
	title, _ := meta["title"].(string)
	if strings.TrimSpace(title) == "" {
		// Match List()'s fallback title derivation so forks of untitled
		// sessions still read as "源标题 (分叉)" once the source gets named.
		_, _, firstUserMsg, err := countMessages(m.SessionPath(sourceID))
		if err == nil && firstUserMsg != "" {
			title = strings.SplitN(firstUserMsg, "\n", 2)[0]
		}
	}
	if strings.TrimSpace(title) != "" {
		meta["title"] = strings.TrimSpace(title) + " (分叉)"
	}
	meta["forked_from"] = sourceID
	meta["archived"], meta["pinned"] = false, false
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.SessionsDir(), newID, "meta.json"), data, 0o644)
}

// copyAttachments copies the attachment metadata directory so the forked
// session can resolve attachment ids uploaded in the source conversation.
// Only the small JSON metadata files are copied; the actual bytes stay in the
// source workspace path they already point at (paths are absolute).
func (m *Manager) copyAttachments(sourceID, newID string) error {
	sourceDir := filepath.Join(m.SessionsDir(), sourceID, "attachments")
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	targetDir := filepath.Join(m.SessionsDir(), newID, "attachments")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(targetDir, entry.Name()), data, 0o600); err != nil {
			return err
		}
	}
	return nil
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
			info.Title = strings.SplitN(firstUserMsg, "\n", 2)[0]
		}
		var preferences struct {
			Title      string `json:"title"`
			Pinned     bool   `json:"pinned"`
			Archived   bool   `json:"archived"`
			ForkedFrom string `json:"forked_from"`
		}
		if data, err := os.ReadFile(filepath.Join(sessionsDir, id, "meta.json")); err == nil && json.Unmarshal(data, &preferences) == nil {
			if preferences.Title != "" {
				info.Title = preferences.Title
			}
			info.Pinned, info.Archived = preferences.Pinned, preferences.Archived
			info.ForkedFrom = preferences.ForkedFrom
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
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry struct {
			Type string `json:"type"`
			TS   int64  `json:"timestamp"`
			// User messages have user.content as an array of {type, text}
			User *struct {
				DisplayText string `json:"display_text"`
				Content     []struct {
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
				firstUserMsg = entry.User.DisplayText
				for _, c := range entry.User.Content {
					if firstUserMsg != "" {
						break
					}
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
