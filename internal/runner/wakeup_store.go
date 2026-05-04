package runner

import (
	"context"
	"sync"

	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/pool"
)

// WakeupStore は SubPool ごとのウェイクアップ条件を永続化する
type WakeupStore interface {
	Save(ctx context.Context, id pool.SubPoolID, cond agent.WakeupCondition) error
	Load(ctx context.Context, id pool.SubPoolID) (agent.WakeupCondition, bool, error)
	Delete(ctx context.Context, id pool.SubPoolID) error
}

// MemoryWakeupStore は WakeupStore のインメモリ実装
type MemoryWakeupStore struct {
	mu   sync.RWMutex
	conds map[pool.SubPoolID]agent.WakeupCondition
}

func NewMemoryWakeupStore() *MemoryWakeupStore {
	return &MemoryWakeupStore{conds: make(map[pool.SubPoolID]agent.WakeupCondition)}
}

func (s *MemoryWakeupStore) Save(_ context.Context, id pool.SubPoolID, cond agent.WakeupCondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conds[id] = cond
	return nil
}

func (s *MemoryWakeupStore) Load(_ context.Context, id pool.SubPoolID) (agent.WakeupCondition, bool, error) {
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
