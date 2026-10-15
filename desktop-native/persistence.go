package main

import (
	"path/filepath"
	"sync"
)

// Reserve on the UI thread; an older worker cannot overwrite a newer snapshot,
// including the final synchronous save during shutdown.
type nativeStore struct {
	mu       sync.Mutex
	versions map[string]uint64
	dir      string
}

func (s *nativeStore) reserve(name string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versions == nil {
		s.versions = map[string]uint64{}
	}
	s.versions[name]++
	return s.versions[name]
}
func (s *nativeStore) save(name string, version uint64, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version != s.versions[name] {
		return nil
	}
	return atomicJSON(filepath.Join(s.dir, name), value)
}
func (l *liveState) configBusy() bool {
	return l.Loading["model-save"] || l.Loading["host-save"] || l.Loading["configuration"]
}
func cloneConfig(cfg clientConfig) clientConfig {
	cfg.Hosts = append([]hostConfig{}, cfg.Hosts...)
	return cfg
}
