package stash_test

import (
	"context"
	"fmt"
	"time"

	"github.com/bjaus/stash"
)

func ExampleCache() {
	ctx := context.Background()
	cache := stash.New[string, int](
		stash.WithCapacity[string, int](100),
		stash.WithTTL[string, int](5*time.Minute),
	)

	cache.Set(ctx, "answer", 42)

	if v, ok, _ := cache.Get(ctx, "answer"); ok {
		fmt.Println(v)
	}
	// Output: 42
}

func ExampleCache_policies() {
	ctx := context.Background()

	// LRU evicts least recently used
	lru := stash.New[string, int](
		stash.WithCapacity[string, int](2),
		stash.WithPolicy[string, int](stash.LRU),
	)
	lru.Set(ctx, "a", 1)
	lru.Set(ctx, "b", 2)
	lru.Get(ctx, "a")    // a is now most recently used
	lru.Set(ctx, "c", 3) // evicts b
	_, hasB, _ := lru.Get(ctx, "b")
	fmt.Println("LRU has b:", hasB)

	// LFU evicts least frequently used
	lfu := stash.New[string, int](
		stash.WithCapacity[string, int](2),
		stash.WithPolicy[string, int](stash.LFU),
	)
	lfu.Set(ctx, "a", 1)
	lfu.Set(ctx, "b", 2)
	lfu.Get(ctx, "a")
	lfu.Get(ctx, "a")    // a has higher frequency
	lfu.Set(ctx, "c", 3) // evicts b
	_, hasB, _ = lfu.Get(ctx, "b")
	fmt.Println("LFU has b:", hasB)

	// Output:
	// LRU has b: false
	// LFU has b: false
}

func ExampleWithLoader() {
	ctx := context.Background()
	cache := stash.New[string, string](
		stash.WithLoader(func(key string) (string, error) {
			// simulate loading from database
			return "loaded:" + key, nil
		}),
	)

	// first call loads and caches
	v1, _ := cache.GetOrLoad(ctx, "user-123")
	fmt.Println(v1)

	// second call returns cached value
	v2, _ := cache.GetOrLoad(ctx, "user-123")
	fmt.Println(v2)

	// Output:
	// loaded:user-123
	// loaded:user-123
}

func ExampleWithCost() {
	ctx := context.Background()
	cache := stash.New[string, []byte](
		stash.WithMaxCost[string, []byte](100),
		stash.WithCost[string, []byte](func(v []byte) int64 {
			return int64(len(v))
		}),
	)

	cache.Set(ctx, "small", make([]byte, 10))  // cost 10, total 10
	cache.Set(ctx, "medium", make([]byte, 50)) // cost 50, total 60
	cache.Set(ctx, "large", make([]byte, 60))  // cost 60, total 120 -> evicts until <= 100

	fmt.Println("entries:", cache.Len())
	// Output: entries: 1
}

func ExampleOnEvict() {
	ctx := context.Background()
	cache := stash.New[string, int](
		stash.WithCapacity[string, int](2),
		stash.OnEvict(func(key string, value int) {
			fmt.Printf("evicted: %s=%d\n", key, value)
		}),
	)

	cache.Set(ctx, "a", 1)
	cache.Set(ctx, "b", 2)
	cache.Set(ctx, "c", 3) // triggers eviction of a

	// Output: evicted: a=1
}

func ExampleCache_Stats() {
	ctx := context.Background()
	cache := stash.New[string, int]()

	cache.Set(ctx, "a", 1)
	cache.Get(ctx, "a") // hit
	cache.Get(ctx, "b") // miss

	stats := cache.Stats()
	fmt.Printf("hits: %d, misses: %d, rate: %.0f%%\n",
		stats.Hits, stats.Misses, stats.HitRate()*100)

	// Output: hits: 1, misses: 1, rate: 50%
}
