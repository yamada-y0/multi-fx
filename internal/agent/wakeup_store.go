package agent

import (
	"context"
	"sync"

	"github.com/yamada/multi-fx/internal/pool"
)

// MemoryWakeupStore は WakeupStore のインメモリ実装
type MemoryWakeupStore struct {
	mu    sync.RWMutex
	conds map[pool.SubPoolID]WakeupCondition
}

func NewMemoryWakeupStore() *MemoryWakeupStore {
	return &MemoryWakeupStore{conds: make(map[pool.SubPoolID]WakeupCondition)}
}

func (s *MemoryWakeupStore) Save(_ context.Context, id pool.SubPoolID, cond WakeupCondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conds[id] = cond
	return nil
}

func (s *MemoryWakeupStore) Load(_ context.Context, id pool.SubPoolID) (WakeupCondition, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cond, ok := s.conds[id]
	return cond, ok, nil
}

func (s *MemoryWakeupStore) Delete(_ context.Context, id pool.SubPoolID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conds, id)
	return nil
}
