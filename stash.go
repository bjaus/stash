package stash

import (
	"context"
	"sync"
	"time"
)

// Cache is a generic in-memory cache with TTL and eviction policies.
type Cache[K comparable, V any] struct {
	mu        sync.RWMutex
	data      map[K]*entry[V]
	evictor   evictor[K]
	cfg       config[K, V]
	stats     Stats
	totalCost int64

	// single-flight for loader
	loading sync.Map // K -> *loadCall[V]
}

type loadCall[V any] struct {
	done  chan struct{}
	value V
	err   error
}

// New creates a new Cache with the given options.
func New[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	cfg := defaultConfig[K, V]()
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Cache[K, V]{
		data:    make(map[K]*entry[V]),
		evictor: newEvictor[K](cfg.policy),
		cfg:     cfg,
	}
}

// Get retrieves a value from the cache.
// If a store is configured and the key is not in memory, it checks the store.
// Returns the value and true if found, zero value and false otherwise.
func (c *Cache[K, V]) Get(ctx context.Context, key K) (V, bool, error) {
	c.mu.Lock()
	v, ok := c.get(key)
	c.mu.Unlock()

	if ok {
		return v, true, nil
	}

	if c.cfg.store == nil {
		var zero V
		return zero, false, nil
	}

	v, ok, err := c.cfg.store.Get(ctx, key)
	if err != nil {
		var zero V
		return zero, false, err
	}

	if ok {
		// populate memory cache from store
		c.mu.Lock()
		c.set(key, v, c.cfg.ttl)
		c.mu.Unlock()
	}

	return v, ok, nil
}

// get is the internal get without locking.
func (c *Cache[K, V]) get(key K) (V, bool) {
	var zero V

	ent, ok := c.data[key]
	if !ok {
		c.stats.Misses++
		if c.cfg.onMiss != nil {
			c.cfg.onMiss(key)
		}
		return zero, false
	}

	if ent.isExpired(c.cfg.clock.Now()) {
		c.delete(key)
		c.stats.Misses++
		if c.cfg.onMiss != nil {
			c.cfg.onMiss(key)
		}
		return zero, false
	}

	c.evictor.onAccess(key)
	ent.freq++
	c.stats.Hits++
	if c.cfg.onHit != nil {
		c.cfg.onHit(key, ent.value)
	}
	return ent.value, true
}

// GetOrLoad retrieves a value from the cache, loading it if not present.
// Uses single-flight to prevent thundering herd.
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, key K) (V, error) {
	var zero V

	if c.cfg.loader == nil {
		v, ok, err := c.Get(ctx, key)
		if err != nil {
			return zero, err
		}
		if !ok {
			return zero, nil
		}
		return v, nil
	}

	// fast path: check cache first
	c.mu.Lock()
	v, ok := c.get(key)
	c.mu.Unlock()
	if ok {
		return v, nil
	}

	// check store before loading
	if c.cfg.store != nil {
		v, ok, err := c.cfg.store.Get(ctx, key)
		if err != nil {
			return zero, err
		}
		if ok {
			c.mu.Lock()
			c.set(key, v, c.cfg.ttl)
			c.mu.Unlock()
			return v, nil
		}
	}

	// single-flight: deduplicate concurrent loads
	call := &loadCall[V]{done: make(chan struct{})}
	actual, loaded := c.loading.LoadOrStore(key, call)
	if loaded {
		// another goroutine is loading
		existing := actual.(*loadCall[V])
		<-existing.done
		return existing.value, existing.err
	}

	// we're the loader
	defer c.loading.Delete(key)

	v, err := c.cfg.loader(key)
	call.value = v
	call.err = err
	close(call.done)

	if err != nil {
		return zero, err
	}

	if err := c.Set(ctx, key, v); err != nil {
		return v, err
	}
	return v, nil
}

// Set adds or updates a value in the cache using the default TTL.
// If a store is configured, writes to both memory and store.
func (c *Cache[K, V]) Set(ctx context.Context, key K, value V) error {
	c.mu.Lock()
	c.set(key, value, c.cfg.ttl)
	c.mu.Unlock()

	if c.cfg.store != nil {
		return c.cfg.store.Set(ctx, key, value, c.cfg.ttl)
	}
	return nil
}

// SetWithTTL adds or updates a value with a specific TTL.
// If a store is configured, writes to both memory and store.
func (c *Cache[K, V]) SetWithTTL(ctx context.Context, key K, value V, ttl time.Duration) error {
	c.mu.Lock()
	c.set(key, value, ttl)
	c.mu.Unlock()

	if c.cfg.store != nil {
		return c.cfg.store.Set(ctx, key, value, ttl)
	}
	return nil
}

func (c *Cache[K, V]) set(key K, value V, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = c.cfg.clock.Now().Add(ttl)
	}

	var cost int64 = 1
	if c.cfg.costFn != nil {
		cost = c.cfg.costFn(value)
	}

	// update existing entry
	if ent, ok := c.data[key]; ok {
		c.totalCost -= ent.cost
		ent.value = value
		ent.expiresAt = expiresAt
		ent.cost = cost
		c.totalCost += cost
		c.evictor.onInsert(key)
		c.evictIfNeeded()
		return
	}

	// new entry
	c.data[key] = &entry[V]{
		value:     value,
		expiresAt: expiresAt,
		cost:      cost,
	}
	c.totalCost += cost
	c.evictor.onInsert(key)
	c.evictIfNeeded()
}

func (c *Cache[K, V]) evictIfNeeded() {
	// evict by count
	for len(c.data) > c.cfg.capacity {
		c.evictOne()
	}

	// evict by cost
	if c.cfg.maxCost > 0 {
		for c.totalCost > c.cfg.maxCost && len(c.data) > 0 {
			c.evictOne()
		}
	}
}

func (c *Cache[K, V]) evictOne() {
	key := c.evictor.evict()
	if ent, ok := c.data[key]; ok {
		c.totalCost -= ent.cost
		delete(c.data, key)
		c.stats.Evictions++
		if c.cfg.onEvict != nil {
			c.cfg.onEvict(key, ent.value)
		}
	}
}

// Delete removes a key from the cache.
// If a store is configured, deletes from both memory and store.
func (c *Cache[K, V]) Delete(ctx context.Context, key K) error {
	c.mu.Lock()
	c.delete(key)
	c.mu.Unlock()

	if c.cfg.store != nil {
		return c.cfg.store.Delete(ctx, key)
	}
	return nil
}

func (c *Cache[K, V]) delete(key K) bool {
	ent, ok := c.data[key]
	if !ok {
		return false
	}

	c.totalCost -= ent.cost
	delete(c.data, key)
	c.evictor.remove(key)
	return true
}

// Has checks if a key exists in memory and is not expired.
func (c *Cache[K, V]) Has(key K) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ent, ok := c.data[key]
	if !ok {
		return false
	}
	return !ent.isExpired(c.cfg.clock.Now())
}

// Clear removes all entries from the in-memory cache.
// Does not affect the external store.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[K]*entry[V])
	c.evictor = newEvictor[K](c.cfg.policy)
	c.totalCost = 0
}

// Len returns the number of entries in the in-memory cache.
// May include expired entries that haven't been cleaned up yet.
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

// Stats returns a snapshot of cache statistics.
func (c *Cache[K, V]) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.stats
}
