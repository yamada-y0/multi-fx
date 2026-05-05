package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/yamada/multi-fx/internal/pool"
)

// JSONStore は StateStore のJSONファイルベース実装
// プロセス間で状態を共有するためのローカル永続化層
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

func (s *JSONStore) subPoolsPath() string     { return filepath.Join(s.dir, "subpools.json") }
func (s *JSONStore) fillsPath() string        { return filepath.Join(s.dir, "fills.json") }
func (s *JSONStore) masterBalancePath() string { return filepath.Join(s.dir, "master_balance.json") }

func (s *JSONStore) SaveSubPool(_ context.Context, snap pool.SubPoolSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadSubPools()
	if err != nil {
		return err
	}
	m[snap.ID] = snap
	return s.writeJSON(s.subPoolsPath(), m)
}

func (s *JSONStore) LoadSubPool(_ context.Context, id pool.SubPoolID) (pool.SubPoolSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadSubPools()
	if err != nil {
		return pool.SubPoolSnapshot{}, err
	}
	snap, ok := m[id]
	if !ok {
		return pool.SubPoolSnapshot{}, fmt.Errorf("store: subpool not found: %s", id)
	}
	return snap, nil
}


func (s *JSONStore) SaveFill(_ context.Context, fill pool.Fill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fills, err := s.loadFills()
	if err != nil {
		return err
	}
	fills = append(fills, fill)
	return s.writeJSON(s.fillsPath(), fills)
}

func (s *JSONStore) ListFills(_ context.Context, subPoolID pool.SubPoolID) ([]pool.Fill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fills, err := s.loadFills()
	if err != nil {
		return nil, err
	}
	result := make([]pool.Fill, 0)
	for _, f := range fills {
		if f.SubPoolID == subPoolID {
			result = append(result, f)
		}
	}
	return result, nil
}


func (s *JSONStore) loadSubPools() (map[pool.SubPoolID]pool.SubPoolSnapshot, error) {
	m := make(map[pool.SubPoolID]pool.SubPoolSnapshot)
	if err := s.readJSON(s.subPoolsPath(), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *JSONStore) loadFills() ([]pool.Fill, error) {
	var fills []pool.Fill
	if err := s.readJSON(s.fillsPath(), &fills); err != nil {
		return nil, err
	}
	return fills, nil
}

func (s *JSONStore) readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // ファイルがなければゼロ値のまま
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
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("json store: write %s: %w", path, err)
	}
	return nil
}
