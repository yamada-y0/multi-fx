package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// JSONWakeupStore は WakeupStore のJSONファイルベース実装
type JSONWakeupStore struct {
	path string
	mu   sync.Mutex
}

func NewJSONWakeupStore(path string) *JSONWakeupStore {
	return &JSONWakeupStore{path: path}
}

func (s *JSONWakeupStore) Save(_ context.Context, cond WakeupCondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(cond, "", "  ")
	if err != nil {
		return fmt.Errorf("wakeup store: marshal: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("wakeup store: write %s: %w", s.path, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("wakeup store: rename %s: %w", s.path, err)
	}
	return nil
}

func (s *JSONWakeupStore) Load(_ context.Context) (WakeupCondition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return WakeupCondition{}, false, nil
	}
	if err != nil {
		return WakeupCondition{}, false, fmt.Errorf("wakeup store: read %s: %w", s.path, err)
	}
	var cond WakeupCondition
	if err := json.Unmarshal(data, &cond); err != nil {
		return WakeupCondition{}, false, fmt.Errorf("wakeup store: unmarshal %s: %w", s.path, err)
	}
	return cond, true, nil
}

func (s *JSONWakeupStore) Delete(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wakeup store: delete %s: %w", s.path, err)
	}
	return nil
}
