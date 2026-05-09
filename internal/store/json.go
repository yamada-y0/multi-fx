package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JSONStore は StateStore のJSONファイルベース実装
type JSONStore struct {
	dir string
	mu  sync.Mutex
}

func NewJSONStore(dir string) (*JSONStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("json store: mkdir %s: %w", dir, err)
	}
	return &JSONStore{dir: dir}, nil
}

func (s *JSONStore) lastFillEventIDPath() string { return filepath.Join(s.dir, "last_fill_event_id.json") }
func (s *JSONStore) sessionIDPath() string       { return filepath.Join(s.dir, "session_id.json") }

func (s *JSONStore) SaveLastFillEventID(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(s.lastFillEventIDPath(), id)
}

func (s *JSONStore) LoadLastFillEventID(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	if err := s.readJSON(s.lastFillEventIDPath(), &id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *JSONStore) SaveSessionID(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(s.sessionIDPath(), id)
}

func (s *JSONStore) LoadSessionID(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	if err := s.readJSON(s.sessionIDPath(), &id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *JSONStore) readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("json store: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("json store: unmarshal %s: %w", path, err)
	}
	return nil
}

func (s *JSONStore) writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("json store: marshal %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("json store: write tmp %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("json store: rename %s: %w", path, err)
	}
	return nil
}
