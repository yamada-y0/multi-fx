package store

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mu              sync.RWMutex
	lastFillEventID string
	sessionID       string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
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

func (s *MemoryStore) SaveSessionID(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = id
	return nil
}

func (s *MemoryStore) LoadSessionID(_ context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID, nil
}
