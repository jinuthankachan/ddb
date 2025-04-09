package kvstore

import (
	"sync"
)

// SafeKVStore is a thread-safe in-memory key-value store
type SafeKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewSafeKVStore creates a new instance of SafeKVStore
func NewSafeKVStore() *SafeKVStore {
	return &SafeKVStore{
		data: make(map[string][]byte),
	}
}

// Set inserts or updates a key-value pair safely
func (s *SafeKVStore) Set(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a copy of the value to prevent external modification
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	s.data[key] = valueCopy
}

// Get retrieves the value associated with a key safely
func (s *SafeKVStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, ok := s.data[key]
	if !ok {
		return nil, false
	}

	// Return a copy to prevent external modification
	valueCopy := make([]byte, len(val))
	copy(valueCopy, val)

	return valueCopy, true
}

// Delete removes a key-value pair safely
func (s *SafeKVStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
}

// Size returns the number of key-value pairs in the store
func (s *SafeKVStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.data)
}

// GetAll returns a copy of all key-value pairs
// Useful for checkpointing or state synchronization
func (s *SafeKVStore) GetAll() map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]byte, len(s.data))
	for k, v := range s.data {
		valueCopy := make([]byte, len(v))
		copy(valueCopy, v)
		result[k] = valueCopy
	}

	return result
}
