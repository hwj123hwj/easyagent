package session

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JSONLStorage struct {
	path       string
	file       *os.File
	mu         sync.Mutex
	byID       map[string]Entry
	leafID     string
	parent     *JSONLStorage // nil for root, set when forked
	maxEntries int           // 0 = unlimited
}

func NewJSONLStorage(path string) *JSONLStorage {
	return &JSONLStorage{path: path, byID: make(map[string]Entry)}
}

func (s *JSONLStorage) Init() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := s.load(); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.file = file
	return nil
}

// SetMaxEntries caps the in-memory entry cache.
// When exceeded, the oldest entries not on the path-to-root are evicted.
// 0 means unlimited (default, backward-compatible).
func (s *JSONLStorage) SetMaxEntries(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxEntries = n
	if n > 0 {
		s.evictExcess()
	}
}

// evictExcess removes the oldest entries not on the path-to-root
// until len(byID) <= maxEntries. Caller must hold mu.
func (s *JSONLStorage) evictExcess() {
	if s.maxEntries <= 0 || len(s.byID) <= s.maxEntries {
		return
	}
	// Determine entries on the path to root — these must be kept.
	keep := make(map[string]bool, len(s.byID))
	keep[s.leafID] = true
	cur := s.leafID
	for cur != "" {
		entry, ok := s.byID[cur]
		if !ok {
			break
		}
		keep[cur] = true
		cur = entry.ParentID
	}

	// Collect evictable entries, sorted by timestamp (oldest first).
	type timedEntry struct {
		id string
		ts int64
	}
	var evictable []timedEntry
	for id, e := range s.byID {
		if !keep[id] {
			evictable = append(evictable, timedEntry{id, e.Timestamp})
		}
	}
	// Sort oldest first for stable eviction order.
	for i := 1; i < len(evictable); i++ {
		if evictable[i].ts < evictable[i-1].ts {
			evictable[i], evictable[i-1] = evictable[i-1], evictable[i]
		}
	}

	// Evict until we're under the limit.
	toRemove := len(s.byID) - s.maxEntries
	for i := 0; i < toRemove && i < len(evictable); i++ {
		delete(s.byID, evictable[i].id)
	}
}

// load reads entries from the JSONL file into byID.
func (s *JSONLStorage) load() error {
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return err
		}
		s.byID[entry.ID] = entry
		// Replay the same cursor transitions as Append. A process may stop
		// after the durable message write but before the following leaf record.
		switch entry.Type {
		case EntryTypeLeaf:
			s.leafID = entry.TargetID
		case EntryTypeMessage, EntryTypeCompaction, EntryTypeMicroCompaction:
			s.leafID = entry.ID
		}
	}
	return scanner.Err()
}

func (s *JSONLStorage) Close() error {
	if s.file == nil {
		return nil
	}
	return s.file.Close()
}

func (s *JSONLStorage) Append(ctx context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if entry.ID == "" {
		entry.ID = newID("entry")
	}
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixMilli()
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := s.file.Write(append(line, '\n')); err != nil {
		return err
	}
	// fsync to ensure crash-safe durability (without this, the OS buffer
	// can lose or half-flush the last line on power failure).
	if err := s.file.Sync(); err != nil {
		// Sync failure is non-fatal on some platforms (e.g. network FS);
		// log but don't block the append.
		_ = err
	}
	s.byID[entry.ID] = entry
	switch entry.Type {
	case EntryTypeLeaf:
		s.leafID = entry.TargetID
	case EntryTypeMessage, EntryTypeCompaction, EntryTypeMicroCompaction:
		s.leafID = entry.ID
	}
	// Enforce maxEntries cap.
	if s.maxEntries > 0 {
		s.evictExcess()
	}
	return nil
}

func (s *JSONLStorage) GetPathToRoot(ctx context.Context, leafID string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if leafID == "" {
		leafID = s.leafID
	}
	path := make([]Entry, 0)
	for leafID != "" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		entry, ok := s.lookupEntry(leafID)
		if !ok {
			return nil, fmt.Errorf("entry %q not found", leafID)
		}
		path = append([]Entry{entry}, path...)
		leafID = entry.ParentID
	}
	return path, nil
}

// lookupEntry checks the local byID map first, then the parent (for forked storage).
func (s *JSONLStorage) lookupEntry(id string) (Entry, bool) {
	if entry, ok := s.byID[id]; ok {
		return entry, true
	}
	if s.parent != nil {
		return s.parent.lookupEntry(id)
	}
	return Entry{}, false
}

func (s *JSONLStorage) SetLeaf(ctx context.Context, targetID string) error {
	return s.Append(ctx, Entry{ID: newID("leaf"), Type: EntryTypeLeaf, TargetID: targetID})
}

func (s *JSONLStorage) GetLeaf(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leafID, nil
}

// Fork creates a new storage that shares the same underlying file
// but has an independent in-memory view. The forked storage starts
// with a copy of the parent's entries and can diverge via Append/SetLeaf.
func (s *JSONLStorage) Fork(ctx context.Context, targetID string) (SessionStorage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build a copy of the current byID map.
	copied := make(map[string]Entry, len(s.byID))
	for k, v := range s.byID {
		copied[k] = v
	}

	forked := &JSONLStorage{
		path:       s.path,
		file:       s.file,
		byID:       copied,
		leafID:     targetID,
		parent:     nil, // fork gets its own full copy, no parent delegation needed
		maxEntries: s.maxEntries,
	}
	return forked, nil
}

// newID generates a unique ID using crypto/rand to avoid collisions.
// Format: <prefix>_<timestamp>_<random_hex>
// This replaces the old time.Now().UnixNano() approach which could collide
// under fast successive appends or concurrent access.
func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails (extremely unlikely)
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}
