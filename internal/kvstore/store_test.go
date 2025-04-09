package kvstore

import (
	"fmt"
	"sync"
	"testing"
)

func TestSafeKVStore(t *testing.T) {
	store := NewSafeKVStore()

	// Test Set and Get
	key := "test-key"
	value := []byte("test-value")

	store.Set(key, value)

	retrievedValue, exists := store.Get(key)
	if !exists {
		t.Fatalf("Expected key %s to exist after Set", key)
	}

	if string(retrievedValue) != string(value) {
		t.Fatalf("Expected value %s, got %s", string(value), string(retrievedValue))
	}

	// Modify the retrieved value to verify we got a copy
	retrievedValue[0] = 'X'

	// Get the value again and verify the original wasn't changed
	retrievedValue2, _ := store.Get(key)
	if string(retrievedValue2) != string(value) {
		t.Fatalf("Original value was modified. Expected %s, got %s",
			string(value), string(retrievedValue2))
	}

	// Test Delete
	store.Delete(key)
	_, exists = store.Get(key)
	if exists {
		t.Fatalf("Expected key %s to not exist after Delete", key)
	}

	// Test concurrent access
	var wg sync.WaitGroup
	concurrentKeys := 100

	for i := 0; i < concurrentKeys; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			k := fmt.Sprintf("key-%d", index)
			v := []byte(fmt.Sprintf("value-%d", index))
			store.Set(k, v)
		}(i)
	}

	wg.Wait()

	// Verify all keys were set
	for i := 0; i < concurrentKeys; i++ {
		k := fmt.Sprintf("key-%d", i)
		expectedV := []byte(fmt.Sprintf("value-%d", i))

		actualV, exists := store.Get(k)
		if !exists {
			t.Fatalf("Expected key %s to exist", k)
		}

		if string(actualV) != string(expectedV) {
			t.Fatalf("Expected value %s, got %s", string(expectedV), string(actualV))
		}
	}

	// Test GetAll
	allKV := store.GetAll()
	if len(allKV) != concurrentKeys {
		t.Fatalf("Expected %d entries, got %d", concurrentKeys, len(allKV))
	}

	// Test Size
	if store.Size() != concurrentKeys {
		t.Fatalf("Expected Size() to return %d, got %d", concurrentKeys, store.Size())
	}
}
