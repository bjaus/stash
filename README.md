# stash

[![Go Reference](https://pkg.go.dev/badge/github.com/bjaus/stash.svg)](https://pkg.go.dev/github.com/bjaus/stash)
[![Go Report Card](https://goreportcard.com/badge/github.com/bjaus/stash)](https://goreportcard.com/report/github.com/bjaus/stash)
[![CI](https://github.com/bjaus/stash/actions/workflows/ci.yml/badge.svg)](https://github.com/bjaus/stash/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/bjaus/stash/branch/main/graph/badge.svg)](https://codecov.io/gh/bjaus/stash)

Generic cache for Go with TTL, eviction policies, and pluggable storage.

## Features

- **Generic Types** — Type-safe caching with `Cache[K, V]`
- **Eviction Policies** — LRU, LFU, and FIFO out of the box
- **TTL Support** — Per-cache default and per-entry overrides
- **Cost-Based Eviction** — Limit by total cost, not just entry count
- **Automatic Loading** — Loader functions with single-flight deduplication
- **Pluggable Storage** — `Store` interface for Redis, Valkey, or custom backends
- **Lifecycle Hooks** — OnHit, OnMiss, OnEvict for observability
- **Injectable Clock** — Control time in tests without real sleeps
- **Zero Dependencies** — Only the Go standard library

## Installation

```bash
go get github.com/bjaus/stash
```

Requires Go 1.25 or later.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/bjaus/stash"
)

func main() {
    ctx := context.Background()

    cache := stash.New[string, int](
        stash.WithCapacity[string, int](1000),
        stash.WithTTL[string, int](5*time.Minute),
    )

    cache.Set(ctx, "answer", 42)

    if v, ok, _ := cache.Get(ctx, "answer"); ok {
        fmt.Println(v) // 42
    }
}
```

## Usage

### Basic Operations

```go
ctx := context.Background()
cache := stash.New[string, *User]()

// Set a value
cache.Set(ctx, "user:123", user)

// Set with custom TTL
cache.SetWithTTL(ctx, "session:abc", session, 30*time.Minute)

// Get a value
user, ok, err := cache.Get(ctx, "user:123")

// Check existence (in-memory only)
if cache.Has("user:123") {
    // ...
}

// Delete
cache.Delete(ctx, "user:123")

// Clear all entries
cache.Clear()
```

### Eviction Policies

Three policies control which entries are evicted when the cache is full:

```go
// LRU - Least Recently Used (default)
cache := stash.New[string, int](
    stash.WithCapacity[string, int](100),
    stash.WithPolicy[string, int](stash.LRU),
)

// LFU - Least Frequently Used
cache := stash.New[string, int](
    stash.WithCapacity[string, int](100),
    stash.WithPolicy[string, int](stash.LFU),
)

// FIFO - First In, First Out
cache := stash.New[string, int](
    stash.WithCapacity[string, int](100),
    stash.WithPolicy[string, int](stash.FIFO),
)
```

| Policy | Evicts | Best For |
|--------|--------|----------|
| LRU | Least recently accessed | General purpose, temporal locality |
| LFU | Least frequently accessed | Hot/cold data, popularity-based |
| FIFO | Oldest entry | Time-ordered data, simple queues |

### Automatic Loading

Use a loader function to automatically fetch missing entries:

```go
cache := stash.New[string, *User](
    stash.WithLoader(func(id string) (*User, error) {
        return db.GetUser(id)
    }),
)

// GetOrLoad checks cache first, then calls loader on miss
user, err := cache.GetOrLoad(ctx, "user:123")
```

The loader uses **single-flight** deduplication — concurrent requests for the same key share one load call, preventing thundering herd.

### Cost-Based Eviction

Limit cache by total cost instead of entry count:

```go
cache := stash.New[string, []byte](
    stash.WithMaxCost[string, []byte](100*1024*1024), // 100MB
    stash.WithCost(func(v []byte) int64 {
        return int64(len(v))
    }),
)

cache.Set(ctx, "small", make([]byte, 1024))      // 1KB
cache.Set(ctx, "large", make([]byte, 50*1024*1024)) // 50MB
// Evicts entries to stay under 100MB
```

### External Storage

Plug in Redis, Valkey, or any custom backend:

```go
// Implement the Store interface
type Store[K comparable, V any] interface {
    Get(ctx context.Context, key K) (V, bool, error)
    Set(ctx context.Context, key K, value V, ttl time.Duration) error
    Delete(ctx context.Context, key K) error
}

// Use with cache
cache := stash.New[string, *User](
    stash.WithStore(redisStore),
)

// Get checks memory first, then store
user, ok, err := cache.Get(ctx, "user:123")

// Set writes to both memory and store
cache.Set(ctx, "user:123", user)
```

### Lifecycle Hooks

Monitor cache behavior for observability:

```go
cache := stash.New[string, int](
    stash.OnHit(func(key string, value int) {
        metrics.Increment("cache.hit")
    }),
    stash.OnMiss(func(key string) {
        metrics.Increment("cache.miss")
    }),
    stash.OnEvict(func(key string, value int) {
        logger.Debug("evicted", "key", key)
    }),
)
```

### Statistics

Track cache performance:

```go
stats := cache.Stats()

fmt.Printf("Hits: %d\n", stats.Hits)
fmt.Printf("Misses: %d\n", stats.Misses)
fmt.Printf("Evictions: %d\n", stats.Evictions)
fmt.Printf("Hit Rate: %.2f%%\n", stats.HitRate()*100)
```

## Testing

Inject a fake clock to control time:

```go
type fakeClock struct {
    now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestTTL(t *testing.T) {
    clock := &fakeClock{now: time.Now()}
    cache := stash.New[string, int](
        stash.WithTTL[string, int](time.Minute),
        stash.WithClock[string, int](clock),
    )

    ctx := context.Background()
    cache.Set(ctx, "key", 42)

    // Value exists
    _, ok, _ := cache.Get(ctx, "key")
    assert.True(t, ok)

    // Advance past TTL
    clock.Advance(2 * time.Minute)

    // Value expired
    _, ok, _ = cache.Get(ctx, "key")
    assert.False(t, ok)
}
```

## API Reference

### Constructor Options

| Option | Description |
|--------|-------------|
| `WithCapacity(n)` | Maximum number of entries (default: 1000) |
| `WithTTL(d)` | Default TTL for entries (default: no expiry) |
| `WithPolicy(p)` | Eviction policy: LRU, LFU, FIFO (default: LRU) |
| `WithLoader(fn)` | Function to load missing entries |
| `WithCost(fn)` | Function to compute entry cost |
| `WithMaxCost(n)` | Maximum total cost |
| `WithStore(s)` | External storage backend |
| `WithStoreErrorHandler(fn)` | Custom handler for store errors |
| `WithClock(c)` | Clock for time operations (testing) |
| `OnHit(fn)` | Callback on cache hit |
| `OnMiss(fn)` | Callback on cache miss |
| `OnEvict(fn)` | Callback on entry eviction |

### Cache Methods

| Method | Description |
|--------|-------------|
| `Get(ctx, key)` | Get value, checking store on miss |
| `GetMany(ctx, keys)` | Get multiple values at once |
| `Set(ctx, key, value)` | Set value with default TTL |
| `SetMany(ctx, entries)` | Set multiple values at once |
| `SetWithTTL(ctx, key, value, ttl)` | Set value with custom TTL |
| `Delete(ctx, key)` | Remove entry from cache and store |
| `DeleteMany(ctx, keys)` | Remove multiple entries at once |
| `GetOrLoad(ctx, key, loader?)` | Get or load via loader (per-call override) |
| `GetManyOrLoad(ctx, keys, loader)` | Get or batch-load missing values |
| `Peek(key)` | Get value without affecting stats/eviction |
| `Has(key)` | Check if key exists in memory |
| `Clear()` | Remove all entries from memory |
| `Len()` | Number of entries in memory |
| `Stats()` | Cache statistics snapshot |

## Design Philosophy

This package separates concerns into layers:

**Cache Configuration** (set at creation):
- Capacity limits
- Eviction policy
- TTL defaults
- Storage backend

**Per-Operation** (set at each call):
- Custom TTL via `SetWithTTL`
- Context for cancellation/timeout

**Observability** (callbacks):
- Metrics via OnHit/OnMiss
- Logging via OnEvict

This separation enables clean dependency injection and consistent cache behavior across an application.

## License

MIT License - see [LICENSE](LICENSE) for details.
