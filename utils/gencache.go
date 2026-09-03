package utils

import (
	"fmt"
	"sync"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

// CacheGen is a snapshot of a GenCache's generation for a key.
// Fillers capture it before reading the database and pass it to SetIf so a
// concurrent invalidation cannot be overwritten by a stale result.
type CacheGen struct {
	global uint64
	local  uint64
}

// GenCache is an in-process TTL cache that folds a generation counter into keys.
// Flush increments a cache-wide generation; Delete increments a per-key generation.
// Lookups always use the current generation, so a fill that raced with invalidation
// cannot store a value later reads will see.
type GenCache struct {
	cache *gocache.Cache

	mu   sync.Mutex
	gen  uint64
	keys map[string]uint64
}

// NewGenCache creates a generation-aware cache with the given default TTL and
// expired-entry cleanup interval.
func NewGenCache(defaultExpiration, cleanupInterval time.Duration) *GenCache {
	return &GenCache{
		cache: gocache.New(defaultExpiration, cleanupInterval),
		keys:  make(map[string]uint64),
	}
}

func (c *GenCache) stampedKey(gen CacheGen, key string) string {
	return fmt.Sprintf("%d:%d:%s", gen.global, gen.local, key)
}

func (c *GenCache) snapshotLocked(key string) CacheGen {
	return CacheGen{global: c.gen, local: c.keys[key]}
}

// Snapshot returns the current generation for key. Capture it before a cache-fill
// database read and pass it to SetIf.
func (c *GenCache) Snapshot(key string) CacheGen {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked(key)
}

// Get returns the cached value for key at the current generation.
func (c *GenCache) Get(key string) (any, bool) {
	return c.GetWith(c.Snapshot(key), key)
}

// GetWith returns the cached value for key at gen.
func (c *GenCache) GetWith(gen CacheGen, key string) (any, bool) {
	return c.cache.Get(c.stampedKey(gen, key))
}

// SetIf stores value under key only if gen is still current.
func (c *GenCache) SetIf(gen CacheGen, key string, value any, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen.global || c.keys[key] != gen.local {
		return
	}
	c.cache.Set(c.stampedKey(gen, key), value, d)
}

// Delete invalidates a single key so in-flight fills for that key cannot restore it.
func (c *GenCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Delete(c.stampedKey(c.snapshotLocked(key), key))
	c.keys[key]++
}

// Flush invalidates every entry so in-flight fills cannot restore purged values.
func (c *GenCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.cache.Flush()
	c.keys = make(map[string]uint64)
}

// GetOrLoad returns the cached value for key, or calls load on a miss.
// If the cache is invalidated while load runs, the result is returned but not stored.
func GetOrLoad[T any](c *GenCache, key string, load func() (T, error)) (T, error) {
	return GetOrLoadExpire(c, key, gocache.DefaultExpiration, load)
}

// GetOrLoadExpire is GetOrLoad with an explicit TTL.
func GetOrLoadExpire[T any](c *GenCache, key string, expire time.Duration, load func() (T, error)) (T, error) {
	gen := c.Snapshot(key)
	if v, ok := c.GetWith(gen, key); ok {
		return v.(T), nil
	}

	v, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	c.SetIf(gen, key, v, expire)
	return v, nil
}
