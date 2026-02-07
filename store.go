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

	// GetMany retrieves multiple values from the store.
	// Returns found values mapped by key. Missing keys are not in the map.
	GetMany(ctx context.Context, keys []K) (map[K]V, error)

	// Set stores a value with an optional TTL.
	// If ttl is zero, the entry should not expire.
	Set(ctx context.Context, key K, value V, ttl time.Duration) error

	// SetMany stores multiple values with an optional TTL.
	SetMany(ctx context.Context, entries map[K]V, ttl time.Duration) error

	// Delete removes a key from the store.
	Delete(ctx context.Context, key K) error

	// DeleteMany removes multiple keys from the store.
	DeleteMany(ctx context.Context, keys []K) error
}

// WithStore sets an external backing store for the cache.
// When set, the cache will read from/write to both the in-memory cache
// and the external store.
func WithStore[K comparable, V any](s Store[K, V]) Option[K, V] {
	return func(c *config[K, V]) {
		c.store = s
	}
}

// WithStoreErrorHandler sets a function to handle store errors.
// The handler receives the error and returns the error to propagate (or nil to swallow).
// Default behavior propagates all errors.
func WithStoreErrorHandler[K comparable, V any](fn func(error) error) Option[K, V] {
	return func(c *config[K, V]) {
		c.storeErrHandler = fn
	}
}
