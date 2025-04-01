package utils

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/samber/lo"
)

type SyncedMap[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K][]V
}

func (m *SyncedMap[K, V]) Get(key K) []V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.m[key]
}

func (m *SyncedMap[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return lo.Keys(m.m)
}

func (m *SyncedMap[K, V]) Each(fn func(K, []V)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.m {
		fn(k, v)
	}
}

func (m *SyncedMap[K, V]) Append(key K, value V) {
	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K][]V)
	}
	if m.m[key] == nil {
		m.m[key] = []V{}
	}
	m.m[key] = append(m.m[key], value)
	m.mu.Unlock()
}

// StringMapToString converts a map[string]string to a string
func StringMapToString(m map[string]string) string {
	if m == nil {
		return "{}"
	}

	b, _ := json.Marshal(m)
	return string(b)
}

// StringToStringMap converts a string back to a map[string]string
func StringToStringMap(s string) (map[string]string, error) {
	m := make(map[string]string)
	if s == "" {
		return m, nil
	}

	err := json.Unmarshal([]byte(s), &m)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling string: %w", err)
	}

	return m, nil
}
