package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
)

type MemoryStore struct {
	mu            sync.RWMutex
	subPools      map[pool.SubPoolID]pool.SubPoolSnapshot
	masterBalance decimal.Decimal
}

func NewMemoryStore(initialMasterBalance decimal.Decimal) *MemoryStore {
	return &MemoryStore{
		subPools:      make(map[pool.SubPoolID]pool.SubPoolSnapshot),
		masterBalance: initialMasterBalance,
	}
}

func (s *MemoryStore) SaveSubPool(_ context.Context, snap pool.SubPoolSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subPools[snap.ID] = snap
	return nil
}

func (s *MemoryStore) LoadSubPool(_ context.Context, id pool.SubPoolID) (pool.SubPoolSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.subPools[id]
	if !ok {
		return pool.SubPoolSnapshot{}, fmt.Errorf("store: subpool not found: %s", id)
	}
	return snap, nil
}

func (s *MemoryStore) ListSubPools(_ context.Context) ([]pool.SubPoolSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]pool.SubPoolSnapshot, 0, len(s.subPools))
	for _, snap := range s.subPools {
		result = append(result, snap)
	}
	return result, nil
}

func (s *MemoryStore) SaveMasterBalance(_ context.Context, balance decimal.Decimal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.masterBalance = balance
	return nil
}

func (s *MemoryStore) LoadMasterBalance(_ context.Context) (decimal.Decimal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.masterBalance, nil
}
