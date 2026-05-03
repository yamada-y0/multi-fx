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
	fills         []pool.Fill
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

// ListActiveSubPools は Active/Suspended の SubPool のみ返す
func (s *MemoryStore) ListActiveSubPools(_ context.Context) ([]pool.SubPoolSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]pool.SubPoolSnapshot, 0)
	for _, snap := range s.subPools {
		if snap.State == pool.StateActive || snap.State == pool.StateSuspended {
			result = append(result, snap)
		}
	}
	return result, nil
}

func (s *MemoryStore) SaveFill(_ context.Context, fill pool.Fill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fills = append(s.fills, fill)
	return nil
}

func (s *MemoryStore) ListFills(_ context.Context, subPoolID pool.SubPoolID) ([]pool.Fill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]pool.Fill, 0)
	for _, f := range s.fills {
		if f.SubPoolID == subPoolID {
			result = append(result, f)
		}
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
