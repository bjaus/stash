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

// result is used to return values from locked sections without heap allocation.
type result[V any] struct {
	value   V
	ok      bool
	expired bool
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
	r := c.get(key)
	c.mu.Unlock()

	// update stats outside lock
	if r.ok {
		c.stats.hit()
		if c.cfg.onHit != nil {
			c.cfg.onHit(key, r.value)
		}
		return r.value, true, nil
	}

	c.stats.miss()
	if c.cfg.onMiss != nil {
		c.cfg.onMiss(key)
	}

	if c.cfg.store == nil {
		var zero V
		return zero, false, nil
	}

	v, ok, err := c.cfg.store.Get(ctx, key)
	if err != nil {
		var zero V
		return zero, false, c.handleStoreErr(err)
	}

	if ok {
		c.mu.Lock()
		c.set(key, v, c.cfg.ttl)
		c.mu.Unlock()
	}

	return v, ok, nil
}

// GetMany retrieves multiple values from the cache.
// Returns found values and the keys that were not found.
func (c *Cache[K, V]) GetMany(ctx context.Context, keys []K) (found map[K]V, missing []K, err error) {
	found = make(map[K]V, len(keys))
	missing = make([]K, 0)

	var hits, misses int64
	c.mu.Lock()
	for _, key := range keys {
		r := c.get(key)
		if r.ok {
			found[key] = r.value
			hits++
			if c.cfg.onHit != nil {
				c.cfg.onHit(key, r.value)
			}
		} else {
			missing = append(missing, key)
			misses++
			if c.cfg.onMiss != nil {
				c.cfg.onMiss(key)
			}
		}
	}
	c.mu.Unlock()

	// batch update stats outside lock
	c.stats.hits.Add(hits)
	c.stats.misses.Add(misses)

	if len(missing) == 0 || c.cfg.store == nil {
		return found, missing, nil
	}

	// check store for missing keys
	storeResults, err := c.cfg.store.GetMany(ctx, missing)
	if err != nil {
		return found, missing, c.handleStoreErr(err)
	}

	// populate found from store and update memory cache
	c.mu.Lock()
	newMissing := make([]K, 0, len(missing))
	for _, key := range missing {
		if v, ok := storeResults[key]; ok {
			found[key] = v
			c.set(key, v, c.cfg.ttl)
		} else {
			newMissing = append(newMissing, key)
		}
	}
	c.mu.Unlock()

	return found, newMissing, nil
}

// get is the internal get without locking. Returns result to allow
// stats updates outside the lock.
func (c *Cache[K, V]) get(key K) result[V] {
	ent, ok := c.data[key]
	if !ok {
		var zero V
		return result[V]{value: zero, ok: false}
	}

	if ent.isExpired(c.cfg.clock.Now()) {
		c.delete(key)
		var zero V
		return result[V]{value: zero, ok: false, expired: true}
	}

	c.evictor.onAccess(key)
	ent.freq++
	return result[V]{value: ent.value, ok: true}
}

// Peek retrieves a value without updating access stats or triggering callbacks.
// Useful for debugging or when you don't want to affect eviction order.
func (c *Cache[K, V]) Peek(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var zero V
	ent, ok := c.data[key]
	if !ok {
		return zero, false
	}

	if ent.isExpired(c.cfg.clock.Now()) {
		return zero, false
	}

	return ent.value, true
}

// LoaderFunc is a function that loads a single value.
type LoaderFunc[K comparable, V any] func(context.Context, K) (V, error)

// BatchLoaderFunc is a function that loads multiple values.
type BatchLoaderFunc[K comparable, V any] func(context.Context, []K) (map[K]V, error)

// GetOrLoad retrieves a value from the cache, loading it if not present.
// Uses single-flight to prevent thundering herd.
// If loader is provided, it overrides the default loader for this call.
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, key K, loader ...LoaderFunc[K, V]) (V, error) {
	var zero V

	// determine which loader to use
	var loadFn LoaderFunc[K, V]
	if len(loader) > 0 && loader[0] != nil {
		loadFn = loader[0]
	} else if c.cfg.loader != nil {
		loadFn = func(ctx context.Context, k K) (V, error) {
			return c.cfg.loader(k)
		}
	}

	if loadFn == nil {
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
	r := c.get(key)
	c.mu.Unlock()
	if r.ok {
		c.stats.hit()
		if c.cfg.onHit != nil {
			c.cfg.onHit(key, r.value)
		}
		return r.value, nil
	}
	c.stats.miss()
	if c.cfg.onMiss != nil {
		c.cfg.onMiss(key)
	}

	// check store before loading
	if c.cfg.store != nil {
		v, ok, err := c.cfg.store.Get(ctx, key)
		if err != nil {
			if herr := c.handleStoreErr(err); herr != nil {
				return zero, herr
			}
		} else if ok {
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
		existing := actual.(*loadCall[V])
		<-existing.done
		return existing.value, existing.err
	}

	defer c.loading.Delete(key)

	v, err := loadFn(ctx, key)
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

// GetManyOrLoad retrieves multiple values, loading missing ones via the loader.
// The loader receives only the keys not found in cache/store.
func (c *Cache[K, V]) GetManyOrLoad(ctx context.Context, keys []K, loader BatchLoaderFunc[K, V]) (map[K]V, error) {
	if loader == nil {
		found, _, err := c.GetMany(ctx, keys)
		return found, err
	}

	found, missing, err := c.GetMany(ctx, keys)
	if err != nil {
		return found, err
	}

	if len(missing) == 0 {
		return found, nil
	}

	// load missing keys
	loaded, err := loader(ctx, missing)
	if err != nil {
		return found, err
	}

	// store loaded values
	if len(loaded) > 0 {
		if err := c.SetMany(ctx, loaded); err != nil {
			// still return loaded values even if store fails
			for k, v := range loaded {
				found[k] = v
			}
			return found, err
		}
		for k, v := range loaded {
			found[k] = v
		}
	}

	return found, nil
}

// Set adds or updates a value in the cache using the default TTL.
// If a store is configured, writes to both memory and store.
func (c *Cache[K, V]) Set(ctx context.Context, key K, value V) error {
	c.mu.Lock()
	c.set(key, value, c.cfg.ttl)
	c.mu.Unlock()

	if c.cfg.store != nil {
		if err := c.cfg.store.Set(ctx, key, value, c.cfg.ttl); err != nil {
			return c.handleStoreErr(err)
		}
	}
	return nil
}

// SetMany adds or updates multiple values using the default TTL.
func (c *Cache[K, V]) SetMany(ctx context.Context, entries map[K]V) error {
	c.mu.Lock()
	for k, v := range entries {
		c.set(k, v, c.cfg.ttl)
	}
	c.mu.Unlock()

	if c.cfg.store != nil {
		if err := c.cfg.store.SetMany(ctx, entries, c.cfg.ttl); err != nil {
			return c.handleStoreErr(err)
		}
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
		if err := c.cfg.store.Set(ctx, key, value, ttl); err != nil {
			return c.handleStoreErr(err)
		}
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
		c.stats.evict()
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
		if err := c.cfg.store.Delete(ctx, key); err != nil {
			return c.handleStoreErr(err)
		}
	}
	return nil
}

// DeleteMany removes multiple keys from the cache.
func (c *Cache[K, V]) DeleteMany(ctx context.Context, keys []K) error {
	c.mu.Lock()
	for _, key := range keys {
		c.delete(key)
	}
	c.mu.Unlock()

	if c.cfg.store != nil {
		if err := c.cfg.store.DeleteMany(ctx, keys); err != nil {
			return c.handleStoreErr(err)
		}
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
// The returned Snapshot is a point-in-time copy safe for concurrent use.
func (c *Cache[K, V]) Stats() Snapshot {
	return c.stats.Snapshot()
}

// handleStoreErr processes store errors through the configured handler.
func (c *Cache[K, V]) handleStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if c.cfg.storeErrHandler != nil {
		return c.cfg.storeErrHandler(err)
	}
	return err
}
