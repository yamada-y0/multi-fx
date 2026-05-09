package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/internal/pool"
)

type MemoryStore struct {
	mu              sync.RWMutex
	subPools        map[pool.SubPoolID]pool.SubPoolSnapshot
	fills           []pool.Fill
	masterBalance   decimal.Decimal
	lastFillEventID string
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

func (s *MemoryStore) SaveLastFillEventID(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFillEventID = id
	return nil
}

func (s *MemoryStore) LoadLastFillEventID(_ context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastFillEventID, nil
}

