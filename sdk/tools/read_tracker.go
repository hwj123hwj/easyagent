package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sync"
	"time"
)

// FileReadRecord captures the state of a file at the time it was read.
type FileReadRecord struct {
	Hash     string
	ModTime  time.Time
	ReadTime time.Time
}

// ReadTracker records when files were read and their content hashes/mtimes,
// enabling edit/write tools to detect external modifications before mutating files.
type ReadTracker struct {
	mu      sync.RWMutex
	records map[string]FileReadRecord // normalized path -> record
}

// NewReadTracker creates a new ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{
		records: make(map[string]FileReadRecord),
	}
}

// normalizePath ensures consistent key lookups across tools.
func (rt *ReadTracker) normalizePath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// Record captures a read event for a file with its content and modification time.
func (rt *ReadTracker) Record(path string, content []byte, modTime time.Time) {
	if rt == nil {
		return
	}
	h := sha256.Sum256(content)
	hashStr := hex.EncodeToString(h[:])

	clean := rt.normalizePath(path)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.records[clean] = FileReadRecord{
		Hash:     hashStr,
		ModTime:  modTime,
		ReadTime: time.Now(),
	}
}

// CheckStale checks if the current file content/modTime differs from when it was last read.
// Returns (isStale, recordedHash, currentHash).
// If the file was never recorded by this tracker, it is considered not stale (returns false, "", "").
func (rt *ReadTracker) CheckStale(path string, currentContent []byte, currentModTime time.Time) (bool, string, string) {
	if rt == nil {
		return false, "", ""
	}
	clean := rt.normalizePath(path)

	rt.mu.RLock()
	record, exists := rt.records[clean]
	rt.mu.RUnlock()

	if !exists {
		return false, "", ""
	}

	h := sha256.Sum256(currentContent)
	currentHash := hex.EncodeToString(h[:])

	if record.Hash != currentHash {
		return true, record.Hash, currentHash
	}

	return false, record.Hash, currentHash
}

// Invalidate removes or updates the tracked record after an internal modification.
func (rt *ReadTracker) Invalidate(path string) {
	if rt == nil {
		return
	}
	clean := rt.normalizePath(path)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.records, clean)
}
