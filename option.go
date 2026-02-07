package stash

import "time"

const (
	// DefaultCapacity is the default maximum number of entries.
	DefaultCapacity = 1000
)

type config[K comparable, V any] struct {
	capacity        int
	ttl             time.Duration
	policy          Policy
	loader          func(K) (V, error)
	costFn          func(V) int64
	maxCost         int64
	clock           Clock
	onEvict         func(K, V)
	onHit           func(K, V)
	onMiss          func(K)
	store           Store[K, V]
	storeErrHandler func(error) error
}

func defaultConfig[K comparable, V any]() config[K, V] {
	return config[K, V]{
		capacity: DefaultCapacity,
		policy:   LRU,
		clock:    realClock{},
	}
}

// Option configures a Cache.
type Option[K comparable, V any] func(*config[K, V])

// WithCapacity sets the maximum number of entries in the cache.
func WithCapacity[K comparable, V any](n int) Option[K, V] {
	return func(c *config[K, V]) {
		if n > 0 {
			c.capacity = n
		}
	}
}

// WithTTL sets the default time-to-live for cache entries.
func WithTTL[K comparable, V any](d time.Duration) Option[K, V] {
	return func(c *config[K, V]) {
		c.ttl = d
	}
}

// WithPolicy sets the eviction policy.
func WithPolicy[K comparable, V any](p Policy) Option[K, V] {
	return func(c *config[K, V]) {
		c.policy = p
	}
}

// WithLoader sets a function to load values on cache miss.
func WithLoader[K comparable, V any](fn func(K) (V, error)) Option[K, V] {
	return func(c *config[K, V]) {
		c.loader = fn
	}
}

// WithCost sets a function to compute the cost of a value.
// Used with WithMaxCost for cost-based eviction.
func WithCost[K comparable, V any](fn func(V) int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.costFn = fn
	}
}

// WithMaxCost sets the maximum total cost of all entries.
// Requires WithCost to be set.
func WithMaxCost[K comparable, V any](n int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.maxCost = n
	}
}

// WithClock sets a custom clock for time operations.
// Useful for testing TTL behavior.
func WithClock[K comparable, V any](clk Clock) Option[K, V] {
	return func(c *config[K, V]) {
		c.clock = clk
	}
}

// OnEvict sets a callback invoked when an entry is evicted.
func OnEvict[K comparable, V any](fn func(K, V)) Option[K, V] {
	return func(c *config[K, V]) {
		c.onEvict = fn
	}
}

// OnHit sets a callback invoked on cache hits.
func OnHit[K comparable, V any](fn func(K, V)) Option[K, V] {
	return func(c *config[K, V]) {
		c.onHit = fn
	}
}

// OnMiss sets a callback invoked on cache misses.
func OnMiss[K comparable, V any](fn func(K)) Option[K, V] {
	return func(c *config[K, V]) {
		c.onMiss = fn
	}
}
