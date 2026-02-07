package stash

import (
	"context"
	"strconv"
	"testing"
)

func BenchmarkCache_Get(b *testing.B) {
	ctx := context.Background()
	cache := New[string, int](WithCapacity[string, int](1000))

	keys := make([]string, 100)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
		cache.Set(ctx, keys[i], i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(ctx, keys[i%100])
	}
}

func BenchmarkCache_Set(b *testing.B) {
	ctx := context.Background()
	cache := New[string, int](WithCapacity[string, int](b.N + 1))

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(ctx, keys[i], i)
	}
}

func BenchmarkCache_SetWithEviction(b *testing.B) {
	ctx := context.Background()
	cache := New[string, int](WithCapacity[string, int](100))

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(ctx, keys[i], i)
	}
}

func BenchmarkCache_Parallel(b *testing.B) {
	ctx := context.Background()
	cache := New[string, int](WithCapacity[string, int](1000))

	keys := make([]string, 100)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
		cache.Set(ctx, keys[i], i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				cache.Get(ctx, keys[i%100])
			} else {
				cache.Set(ctx, keys[i%100], i)
			}
			i++
		}
	})
}

func BenchmarkCache_Policies(b *testing.B) {
	policies := []struct {
		name   string
		policy Policy
	}{
		{"LRU", LRU},
		{"LFU", LFU},
		{"FIFO", FIFO},
	}

	for _, tc := range policies {
		b.Run(tc.name, func(b *testing.B) {
			policy := tc.policy
			ctx := context.Background()
			cache := New[string, int](
				WithCapacity[string, int](100),
				WithPolicy[string, int](policy),
			)

			keys := make([]string, 200)
			for i := range keys {
				keys[i] = strconv.Itoa(i)
			}

			for i := 0; i < 100; i++ {
				cache.Set(ctx, keys[i], i)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := keys[i%200]
				if _, ok, _ := cache.Get(ctx, key); !ok {
					cache.Set(ctx, key, i)
				}
			}
		})
	}
}
