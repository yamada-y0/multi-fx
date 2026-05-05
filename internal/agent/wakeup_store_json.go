package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/yamada/multi-fx/internal/pool"
)

// JSONWakeupStore は WakeupStore のJSONファイルベース実装
// kickとcliプロセス間でWakeupConditionを共有する
type JSONWakeupStore struct {
	path string // ファイルパス（例: ~/.multi-fx/wakeup.json）
	mu   sync.Mutex
}

func NewJSONWakeupStore(path string) *JSONWakeupStore {
	return &JSONWakeupStore{path: path}
}

func (s *JSONWakeupStore) Save(_ context.Context, id pool.SubPoolID, cond WakeupCondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	m[id] = cond
	return s.write(m)
}

func (s *JSONWakeupStore) Load(_ context.Context, id pool.SubPoolID) (WakeupCondition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return WakeupCondition{}, false, err
	}
	cond, ok := m[id]
	return cond, ok, nil
}

func (s *JSONWakeupStore) Delete(_ context.Context, id pool.SubPoolID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	delete(m, id)
	return s.write(m)
}

func (s *JSONWakeupStore) load() (map[pool.SubPoolID]WakeupCondition, error) {
	m := make(map[pool.SubPoolID]WakeupCondition)
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("wakeup store: read %s: %w", s.path, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("wakeup store: unmarshal %s: %w", s.path, err)
	}
	return m, nil
}

func (s *JSONWakeupStore) write(m map[pool.SubPoolID]WakeupCondition) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("wakeup store: marshal: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("wakeup store: write %s: %w", s.path, err)
	}
	return nil
}
