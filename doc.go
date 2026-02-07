// Package stash provides a generic in-memory cache with TTL, eviction policies,
// and pluggable external storage.
//
// # Overview
//
// Stash is a type-safe, concurrent cache for Go applications. It supports multiple
// eviction policies (LRU, LFU, FIFO), time-to-live expiration, cost-based eviction,
// automatic loading with single-flight deduplication, and optional external storage
// backends like Redis.
//
// # Basic Usage
//
// Create a cache and perform basic operations:
//
//	ctx := context.Background()
//
//	cache := stash.New[string, int](
//		stash.WithCapacity[string, int](1000),
//		stash.WithTTL[string, int](5 * time.Minute),
//	)
//
//	// Set a value
//	cache.Set(ctx, "key", 42)
//
//	// Get a value
//	value, ok, err := cache.Get(ctx, "key")
//	if err != nil {
//		return err
//	}
//	if ok {
//		fmt.Println(value)
//	}
//
//	// Delete a value
//	cache.Delete(ctx, "key")
//
// # Eviction Policies
//
// Three eviction policies determine which entries are removed when the cache
// reaches capacity:
//
//	// LRU - Least Recently Used (default)
//	cache := stash.New[string, int](stash.WithPolicy[string, int](stash.LRU))
//
//	// LFU - Least Frequently Used
//	cache := stash.New[string, int](stash.WithPolicy[string, int](stash.LFU))
//
//	// FIFO - First In, First Out
//	cache := stash.New[string, int](stash.WithPolicy[string, int](stash.FIFO))
//
// # Automatic Loading
//
// Use a loader function to automatically fetch missing entries. The loader
// uses single-flight deduplication to prevent thundering herd:
//
//	cache := stash.New[string, *User](
//		stash.WithLoader(func(id string) (*User, error) {
//			return db.GetUser(id)
//		}),
//	)
//
//	// GetOrLoad checks cache, then calls loader on miss
//	user, err := cache.GetOrLoad(ctx, "user:123")
//
// # Cost-Based Eviction
//
// Limit the cache by total cost rather than entry count. Useful for caching
// variable-size data like images or documents:
//
//	cache := stash.New[string, []byte](
//		stash.WithMaxCost[string, []byte](100 * 1024 * 1024), // 100MB
//		stash.WithCost(func(v []byte) int64 {
//			return int64(len(v))
//		}),
//	)
//
// # External Storage
//
// Plug in external storage backends by implementing the Store interface:
//
//	type Store[K comparable, V any] interface {
//		Get(ctx context.Context, key K) (V, bool, error)
//		Set(ctx context.Context, key K, value V, ttl time.Duration) error
//		Delete(ctx context.Context, key K) error
//	}
//
// When a store is configured, Get checks memory first then the store, and
// Set writes to both memory and the store:
//
//	cache := stash.New[string, *User](stash.WithStore(redisStore))
//
// # Lifecycle Hooks
//
// Monitor cache behavior for metrics and logging:
//
//	cache := stash.New[string, int](
//		stash.OnHit(func(key string, value int) {
//			metrics.Increment("cache.hit")
//		}),
//		stash.OnMiss(func(key string) {
//			metrics.Increment("cache.miss")
//		}),
//		stash.OnEvict(func(key string, value int) {
//			logger.Debug("evicted", "key", key)
//		}),
//	)
//
// # Testing
//
// Inject a custom clock to control time in tests:
//
//	type fakeClock struct{ now time.Time }
//	func (c *fakeClock) Now() time.Time { return c.now }
//
//	clock := &fakeClock{now: time.Now()}
//	cache := stash.New[string, int](
//		stash.WithTTL[string, int](time.Minute),
//		stash.WithClock[string, int](clock),
//	)
//
//	cache.Set(ctx, "key", 42)
//	clock.now = clock.now.Add(2 * time.Minute) // TTL expired
//	_, ok, _ := cache.Get(ctx, "key")          // ok == false
//
// # Thread Safety
//
// All Cache methods are safe for concurrent use. The cache uses a sync.RWMutex
// internally to protect shared state.
package stash
