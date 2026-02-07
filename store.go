package stash

import (
	"context"
	"time"
)

// Store defines an external backing store for the cache.
// Implementations can persist cache entries to Redis, disk, or other storage.
type Store[K comparable, V any] interface {
	// Get retrieves a value from the store.
	// Returns the value and true if found, zero value and false otherwise.
	Get(ctx context.Context, key K) (V, bool, error)

	// Set stores a value with an optional TTL.
	// If ttl is zero, the entry should not expire.
	Set(ctx context.Context, key K, value V, ttl time.Duration) error

	// Delete removes a key from the store.
	Delete(ctx context.Context, key K) error
}

// WithStore sets an external backing store for the cache.
// When set, the cache will read from/write to both the in-memory cache
// and the external store.
func WithStore[K comparable, V any](s Store[K, V]) Option[K, V] {
	return func(c *config[K, V]) {
		c.store = s
	}
}
