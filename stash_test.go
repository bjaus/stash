package stash

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type mockClock struct {
	now time.Time
}

func (c *mockClock) Now() time.Time {
	return c.now
}

func (c *mockClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

// mockStore is a simple in-memory store for testing.
type mockStore[K comparable, V any] struct {
	data map[K]V
}

func newMockStore[K comparable, V any]() *mockStore[K, V] {
	return &mockStore[K, V]{data: make(map[K]V)}
}

func (s *mockStore[K, V]) Get(_ context.Context, key K) (V, bool, error) {
	v, ok := s.data[key]
	return v, ok, nil
}

func (s *mockStore[K, V]) Set(_ context.Context, key K, value V, _ time.Duration) error {
	s.data[key] = value
	return nil
}

func (s *mockStore[K, V]) GetMany(_ context.Context, keys []K) (map[K]V, error) {
	result := make(map[K]V)
	for _, key := range keys {
		if v, ok := s.data[key]; ok {
			result[key] = v
		}
	}
	return result, nil
}

func (s *mockStore[K, V]) SetMany(_ context.Context, entries map[K]V, _ time.Duration) error {
	for k, v := range entries {
		s.data[k] = v
	}
	return nil
}

func (s *mockStore[K, V]) Delete(_ context.Context, key K) error {
	delete(s.data, key)
	return nil
}

func (s *mockStore[K, V]) DeleteMany(_ context.Context, keys []K) error {
	for _, key := range keys {
		delete(s.data, key)
	}
	return nil
}

type errorStore[K comparable, V any] struct{}

func (s *errorStore[K, V]) Get(_ context.Context, _ K) (V, bool, error) {
	var zero V
	return zero, false, errors.New("store error")
}

func (s *errorStore[K, V]) Set(_ context.Context, _ K, _ V, _ time.Duration) error {
	return errors.New("store error")
}

func (s *errorStore[K, V]) GetMany(_ context.Context, _ []K) (map[K]V, error) {
	return nil, errors.New("store error")
}

func (s *errorStore[K, V]) SetMany(_ context.Context, _ map[K]V, _ time.Duration) error {
	return errors.New("store error")
}

func (s *errorStore[K, V]) Delete(_ context.Context, _ K) error {
	return errors.New("store error")
}

func (s *errorStore[K, V]) DeleteMany(_ context.Context, _ []K) error {
	return errors.New("store error")
}

type StashSuite struct {
	suite.Suite
	ctx context.Context
	clk *mockClock
}

func (s *StashSuite) SetupTest() {
	s.ctx = context.Background()
	s.clk = &mockClock{now: time.Now()}
}

func TestStashSuite(t *testing.T) {
	suite.Run(t, new(StashSuite))
}

func (s *StashSuite) TestGetSet() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	v, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(1, v)

	v, ok, err = c.Get(s.ctx, "b")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(2, v)

	_, ok, err = c.Get(s.ctx, "c")
	s.Require().NoError(err)
	s.False(ok)
}

func (s *StashSuite) TestSetUpdates() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "a", 2)

	v, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(2, v)
	s.Equal(1, c.Len())
}

func (s *StashSuite) TestDelete() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Delete(s.ctx, "a")

	_, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.False(ok)
}

func (s *StashSuite) TestHas() {
	c := New[string, int]()

	s.False(c.Has("a"))

	c.Set(s.ctx, "a", 1)

	s.True(c.Has("a"))
}

func (s *StashSuite) TestClear() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)
	c.Clear()

	s.Equal(0, c.Len())
	s.False(c.Has("a"))
}

func (s *StashSuite) TestTTL() {
	c := New[string, int](
		WithTTL[string, int](time.Minute),
		WithClock[string, int](s.clk),
	)

	c.Set(s.ctx, "a", 1)

	v, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(1, v)

	s.clk.Advance(2 * time.Minute)

	_, ok, err = c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.False(ok)
}

func (s *StashSuite) TestSetWithTTL() {
	c := New[string, int](
		WithTTL[string, int](time.Hour),
		WithClock[string, int](s.clk),
	)

	c.SetWithTTL(s.ctx, "a", 1, time.Second)

	s.clk.Advance(2 * time.Second)

	_, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.False(ok)
}

func (s *StashSuite) TestCapacity() {
	c := New[string, int](WithCapacity[string, int](2))

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)
	c.Set(s.ctx, "c", 3)

	s.Equal(2, c.Len())
}

func (s *StashSuite) TestLRUEviction() {
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](LRU),
	)

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	// access a to make it recently used
	c.Get(s.ctx, "a")

	// add c, should evict b (least recently used)
	c.Set(s.ctx, "c", 3)

	s.True(c.Has("a"), "a should still exist")
	s.False(c.Has("b"), "b should be evicted")
	s.True(c.Has("c"), "c should exist")
}

func (s *StashSuite) TestLFUEviction() {
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](LFU),
	)

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	// access a multiple times to increase frequency
	c.Get(s.ctx, "a")
	c.Get(s.ctx, "a")

	// add c, should evict b (least frequently used)
	c.Set(s.ctx, "c", 3)

	s.True(c.Has("a"), "a should still exist")
	s.False(c.Has("b"), "b should be evicted")
	s.True(c.Has("c"), "c should exist")
}

func (s *StashSuite) TestFIFOEviction() {
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](FIFO),
	)

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	// access a (shouldn't affect FIFO order)
	c.Get(s.ctx, "a")

	// add c, should evict a (oldest)
	c.Set(s.ctx, "c", 3)

	s.False(c.Has("a"), "a should be evicted (first in)")
	s.True(c.Has("b"), "b should still exist")
	s.True(c.Has("c"), "c should exist")
}

func (s *StashSuite) TestCostEviction() {
	c := New[string, string](
		WithCapacity[string, string](100),
		WithMaxCost[string, string](10),
		WithCost[string, string](func(v string) int64 { return int64(len(v)) }),
	)

	c.Set(s.ctx, "a", "12345") // cost 5
	c.Set(s.ctx, "b", "12345") // cost 5, total 10

	s.Equal(2, c.Len())

	c.Set(s.ctx, "c", "123") // cost 3, should evict to make room

	s.LessOrEqual(c.Len(), 2)
}

func (s *StashSuite) TestLoader() {
	loaded := 0
	c := New[string, int](
		WithLoader(func(key string) (int, error) {
			loaded++
			return len(key), nil
		}),
	)

	v, err := c.GetOrLoad(s.ctx, "abc")
	s.Require().NoError(err)
	s.Equal(3, v)
	s.Equal(1, loaded)

	// second call should use cache
	v, err = c.GetOrLoad(s.ctx, "abc")
	s.Require().NoError(err)
	s.Equal(3, v)
	s.Equal(1, loaded, "loader should not be called again (cached)")
}

func (s *StashSuite) TestLoaderError() {
	testErr := errors.New("load failed")
	c := New[string, int](
		WithLoader(func(key string) (int, error) {
			return 0, testErr
		}),
	)

	_, err := c.GetOrLoad(s.ctx, "a")
	s.Require().ErrorIs(err, testErr)

	// should not be cached
	s.False(c.Has("a"), "failed load should not cache")
}

func (s *StashSuite) TestLoaderSingleFlight() {
	var loadCount atomic.Int32
	proceed := make(chan struct{})

	c := New[string, int](
		WithLoader(func(key string) (int, error) {
			loadCount.Add(1)
			<-proceed
			return 42, nil
		}),
	)

	var wg sync.WaitGroup
	results := make([]int, 3)
	errs := make([]error, 3)

	for i := range 3 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = c.GetOrLoad(s.ctx, "key")
		}(i)
	}

	// give goroutines time to start and coalesce on the same load call
	time.Sleep(10 * time.Millisecond)

	// signal to proceed
	close(proceed)
	wg.Wait()

	s.Equal(int32(1), loadCount.Load(), "single-flight should coalesce loads")

	for i, err := range errs {
		s.NoError(err, "goroutine %d error", i)
		s.Equal(42, results[i], "goroutine %d result", i)
	}
}

func (s *StashSuite) TestStats() {
	c := New[string, int](WithCapacity[string, int](2))

	c.Set(s.ctx, "a", 1)
	c.Get(s.ctx, "a") // hit
	c.Get(s.ctx, "b") // miss
	c.Set(s.ctx, "b", 2)
	c.Set(s.ctx, "c", 3) // eviction

	stats := c.Stats()
	s.Equal(int64(1), stats.Hits)
	s.Equal(int64(1), stats.Misses)
	s.Equal(int64(1), stats.Evictions)
}

func (s *StashSuite) TestHitRate() {
	tests := map[string]struct {
		stats    Stats
		expected float64
	}{
		"normal": {
			stats:    Stats{Hits: 3, Misses: 1},
			expected: 0.75,
		},
		"no accesses": {
			stats:    Stats{},
			expected: 0,
		},
	}

	for name, tc := range tests {
		s.Run(name, func() {
			s.Equal(tc.expected, tc.stats.HitRate())
		})
	}
}

func (s *StashSuite) TestCallbacks() {
	var hitKey string
	var hitVal int
	var missKey string
	var evictKey string
	var evictVal int

	c := New[string, int](
		WithCapacity[string, int](1),
		OnHit[string, int](func(k string, v int) { hitKey = k; hitVal = v }),
		OnMiss[string, int](func(k string) { missKey = k }),
		OnEvict[string, int](func(k string, v int) { evictKey = k; evictVal = v }),
	)

	c.Set(s.ctx, "a", 1)
	c.Get(s.ctx, "a")
	s.Equal("a", hitKey)
	s.Equal(1, hitVal)

	c.Get(s.ctx, "b")
	s.Equal("b", missKey)

	c.Set(s.ctx, "c", 3) // evicts a
	s.Equal("a", evictKey)
	s.Equal(1, evictVal)
}

func (s *StashSuite) TestConcurrentAccess() {
	c := New[int, int](WithCapacity[int, int](100))

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Set(s.ctx, n, n*2)
			c.Get(s.ctx, n)
			c.Has(n)
			c.Delete(s.ctx, n)
		}(i)
	}
	wg.Wait()
}

func (s *StashSuite) TestHasWithExpiry() {
	c := New[string, int](
		WithTTL[string, int](time.Minute),
		WithClock[string, int](s.clk),
	)

	c.Set(s.ctx, "a", 1)

	s.True(c.Has("a"))

	s.clk.Advance(2 * time.Minute)

	s.False(c.Has("a"))
}

func (s *StashSuite) TestGetOrLoadWithoutLoader() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	v, err := c.GetOrLoad(s.ctx, "a")
	s.Require().NoError(err)
	s.Equal(1, v)

	v, err = c.GetOrLoad(s.ctx, "b")
	s.Require().NoError(err)
	s.Equal(0, v)
}

func (s *StashSuite) TestStoreGet() {
	store := newMockStore[string, int]()
	store.data["external"] = 42

	c := New[string, int](WithStore(store))

	// not in memory, should fetch from store
	v, ok, err := c.Get(s.ctx, "external")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(42, v)

	// should now be in memory
	s.True(c.Has("external"))
}

func (s *StashSuite) TestStoreSet() {
	store := newMockStore[string, int]()
	c := New[string, int](WithStore(store))

	err := c.Set(s.ctx, "key", 123)
	s.Require().NoError(err)

	// should be in memory
	s.True(c.Has("key"))

	// should be in store
	s.Equal(123, store.data["key"])
}

func (s *StashSuite) TestStoreDelete() {
	store := newMockStore[string, int]()
	store.data["key"] = 1

	c := New[string, int](WithStore(store))
	c.Set(s.ctx, "key", 1)

	err := c.Delete(s.ctx, "key")
	s.Require().NoError(err)

	// should be gone from memory
	s.False(c.Has("key"))

	// should be gone from store
	_, ok := store.data["key"]
	s.False(ok)
}

func (s *StashSuite) TestGetWithoutStore() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	v, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(1, v)

	v, ok, err = c.Get(s.ctx, "b")
	s.Require().NoError(err)
	s.False(ok)
	s.Equal(0, v)
}

func (s *StashSuite) TestLFUDelete() {
	c := New[string, int](WithPolicy[string, int](LFU))

	c.Set(s.ctx, "a", 1)
	c.Get(s.ctx, "a") // increase frequency
	c.Delete(s.ctx, "a")

	s.False(c.Has("a"))
}

func (s *StashSuite) TestFIFODelete() {
	c := New[string, int](WithPolicy[string, int](FIFO))

	c.Set(s.ctx, "a", 1)
	c.Delete(s.ctx, "a")

	s.False(c.Has("a"))
}

func (s *StashSuite) TestStoreGetError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	_, _, err := c.Get(s.ctx, "key")
	s.Error(err)
}

func (s *StashSuite) TestStoreSetError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.Set(s.ctx, "key", 1)
	s.Error(err)
}

func (s *StashSuite) TestStoreDeleteError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.Delete(s.ctx, "key")
	s.Error(err)
}

func (s *StashSuite) TestSetWithTTLStore() {
	store := newMockStore[string, int]()
	c := New[string, int](WithStore(store))

	err := c.SetWithTTL(s.ctx, "key", 123, time.Hour)
	s.Require().NoError(err)

	s.Equal(123, store.data["key"])
}

func (s *StashSuite) TestGetOrLoadWithStore() {
	store := newMockStore[string, int]()
	store.data["external"] = 99

	loaded := 0
	c := New[string, int](
		WithStore(store),
		WithLoader(func(key string) (int, error) {
			loaded++
			return 42, nil
		}),
	)

	// should find in store, not call loader
	v, err := c.GetOrLoad(s.ctx, "external")
	s.Require().NoError(err)
	s.Equal(99, v)
	s.Equal(0, loaded, "loader should not be called when value is in store")

	// should call loader for missing key
	v, err = c.GetOrLoad(s.ctx, "missing")
	s.Require().NoError(err)
	s.Equal(42, v)
	s.Equal(1, loaded)
}

func (s *StashSuite) TestGetOrLoadStoreError() {
	c := New[string, int](
		WithStore[string, int](&errorStore[string, int]{}),
		WithLoader(func(key string) (int, error) {
			return 42, nil
		}),
	)

	_, err := c.GetOrLoad(s.ctx, "key")
	s.Error(err)
}

func (s *StashSuite) TestSetWithTTLStoreError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.SetWithTTL(s.ctx, "key", 1, time.Hour)
	s.Error(err)
}

func (s *StashSuite) TestFIFOAccess() {
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](FIFO),
	)

	c.Set(s.ctx, "a", 1)
	// access should be a no-op for FIFO but still gets called
	c.Get(s.ctx, "a")
	c.Get(s.ctx, "a")

	s.True(c.Has("a"))
}

func (s *StashSuite) TestLFUInsertExisting() {
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](LFU),
	)

	c.Set(s.ctx, "a", 1)
	c.Get(s.ctx, "a")    // freq 2
	c.Set(s.ctx, "a", 2) // update existing, should increase freq

	v, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(2, v)
}

func (s *StashSuite) TestFIFOInsertExisting() {
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](FIFO),
	)

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "a", 2) // update existing, FIFO should not move it

	v, ok, err := c.Get(s.ctx, "a")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(2, v)
}

func (s *StashSuite) TestLFUAccessNonExistent() {
	c := New[string, int](WithPolicy[string, int](LFU))

	// access non-existent key (miss path)
	_, ok, err := c.Get(s.ctx, "nonexistent")
	s.Require().NoError(err)
	s.False(ok)
}

func (s *StashSuite) TestLFURemoveNonExistent() {
	c := New[string, int](WithPolicy[string, int](LFU))

	// delete non-existent key should not panic
	c.Delete(s.ctx, "nonexistent")
}

func (s *StashSuite) TestFIFORemoveNonExistent() {
	c := New[string, int](WithPolicy[string, int](FIFO))

	// delete non-existent key should not panic
	c.Delete(s.ctx, "nonexistent")
}

func (s *StashSuite) TestPeek() {
	c := New[string, int](WithCapacity[string, int](2))

	c.Set(s.ctx, "a", 1)

	// peek should return value
	v, ok := c.Peek("a")
	s.True(ok)
	s.Equal(1, v)

	// peek should not affect stats
	stats := c.Stats()
	s.Equal(int64(0), stats.Hits, "peek should not increment hits")

	// peek non-existent
	_, ok = c.Peek("b")
	s.False(ok)
}

func (s *StashSuite) TestPeekExpired() {
	c := New[string, int](
		WithTTL[string, int](time.Minute),
		WithClock[string, int](s.clk),
	)

	c.Set(s.ctx, "a", 1)
	s.clk.Advance(2 * time.Minute)

	_, ok := c.Peek("a")
	s.False(ok, "peek expired entry should return false")
}

func (s *StashSuite) TestGetMany() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	found, missing, err := c.GetMany(s.ctx, []string{"a", "b", "c"})
	s.Require().NoError(err)

	s.Len(found, 2)
	s.Equal(1, found["a"])
	s.Equal(2, found["b"])
	s.Equal([]string{"c"}, missing)
}

func (s *StashSuite) TestGetManyWithStore() {
	store := newMockStore[string, int]()
	store.data["c"] = 3
	store.data["d"] = 4

	c := New[string, int](WithStore(store))
	c.Set(s.ctx, "a", 1)

	found, missing, err := c.GetMany(s.ctx, []string{"a", "c", "e"})
	s.Require().NoError(err)

	s.Len(found, 2)
	s.Equal(1, found["a"])
	s.Equal(3, found["c"])
	s.Equal([]string{"e"}, missing)
}

func (s *StashSuite) TestSetMany() {
	c := New[string, int]()

	err := c.SetMany(s.ctx, map[string]int{"a": 1, "b": 2, "c": 3})
	s.Require().NoError(err)

	s.Equal(3, c.Len())

	v, ok, err := c.Get(s.ctx, "b")
	s.Require().NoError(err)
	s.True(ok)
	s.Equal(2, v)
}

func (s *StashSuite) TestSetManyWithStore() {
	store := newMockStore[string, int]()
	c := New[string, int](WithStore(store))

	err := c.SetMany(s.ctx, map[string]int{"a": 1, "b": 2})
	s.Require().NoError(err)

	s.Equal(1, store.data["a"])
	s.Equal(2, store.data["b"])
}

func (s *StashSuite) TestDeleteMany() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)
	c.Set(s.ctx, "c", 3)

	err := c.DeleteMany(s.ctx, []string{"a", "c"})
	s.Require().NoError(err)

	s.False(c.Has("a"))
	s.False(c.Has("c"))
	s.True(c.Has("b"))
}

func (s *StashSuite) TestDeleteManyWithStore() {
	store := newMockStore[string, int]()
	store.data["a"] = 1
	store.data["b"] = 2

	c := New[string, int](WithStore(store))
	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	err := c.DeleteMany(s.ctx, []string{"a"})
	s.Require().NoError(err)

	_, ok := store.data["a"]
	s.False(ok, "a should be deleted from store")

	_, ok = store.data["b"]
	s.True(ok, "b should still exist in store")
}

func (s *StashSuite) TestGetOrLoadWithPerCallLoader() {
	defaultLoaded := 0
	perCallLoaded := 0

	c := New[string, int](
		WithLoader(func(key string) (int, error) {
			defaultLoaded++
			return 1, nil
		}),
	)

	// use per-call loader
	v, err := c.GetOrLoad(s.ctx, "a", func(_ context.Context, key string) (int, error) {
		perCallLoaded++
		return 99, nil
	})
	s.Require().NoError(err)
	s.Equal(99, v)
	s.Equal(1, perCallLoaded)
	s.Equal(0, defaultLoaded)
}

func (s *StashSuite) TestGetManyOrLoad() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)

	result, err := c.GetManyOrLoad(s.ctx, []string{"a", "b", "c"}, func(_ context.Context, keys []string) (map[string]int, error) {
		loaded := make(map[string]int)
		for _, k := range keys {
			loaded[k] = len(k) * 10
		}
		return loaded, nil
	})
	s.Require().NoError(err)

	s.Equal(1, result["a"], "cached value")
	s.Equal(10, result["b"], "loaded value")
	s.Equal(10, result["c"], "loaded value")
}

func (s *StashSuite) TestGetManyOrLoadNilLoader() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)

	result, err := c.GetManyOrLoad(s.ctx, []string{"a", "b"}, nil)
	s.Require().NoError(err)

	s.Equal(1, result["a"])
	_, ok := result["b"]
	s.False(ok, "b should not be in result without loader")
}

func (s *StashSuite) TestGetManyOrLoadAllCached() {
	c := New[string, int]()

	c.Set(s.ctx, "a", 1)
	c.Set(s.ctx, "b", 2)

	loaderCalled := false
	result, err := c.GetManyOrLoad(s.ctx, []string{"a", "b"}, func(_ context.Context, keys []string) (map[string]int, error) {
		loaderCalled = true
		return nil, nil
	})
	s.Require().NoError(err)

	s.False(loaderCalled, "loader should not be called when all keys are cached")
	s.Equal(1, result["a"])
	s.Equal(2, result["b"])
}

func (s *StashSuite) TestStoreErrorHandler() {
	handlerCalled := false

	c := New[string, int](
		WithStore[string, int](&errorStore[string, int]{}),
		WithStoreErrorHandler[string, int](func(err error) error {
			handlerCalled = true
			return nil // swallow the error
		}),
	)

	_, _, err := c.Get(s.ctx, "key")
	s.NoError(err, "error should be swallowed by handler")
	s.True(handlerCalled)
}

func (s *StashSuite) TestStoreErrorHandlerPropagate() {
	customErr := errors.New("custom error")

	c := New[string, int](
		WithStore[string, int](&errorStore[string, int]{}),
		WithStoreErrorHandler[string, int](func(err error) error {
			return customErr
		}),
	)

	_, _, err := c.Get(s.ctx, "key")
	s.Require().ErrorIs(err, customErr)
}

func (s *StashSuite) TestGetManyStoreError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	_, _, err := c.GetMany(s.ctx, []string{"a", "b"})
	s.Error(err)
}

func (s *StashSuite) TestSetManyStoreError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.SetMany(s.ctx, map[string]int{"a": 1})
	s.Error(err)
}

func (s *StashSuite) TestDeleteManyStoreError() {
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.DeleteMany(s.ctx, []string{"a"})
	s.Error(err)
}

func (s *StashSuite) TestGetManyOrLoadError() {
	c := New[string, int]()
	loaderErr := errors.New("loader failed")

	_, err := c.GetManyOrLoad(s.ctx, []string{"a"}, func(_ context.Context, _ []string) (map[string]int, error) {
		return nil, loaderErr
	})
	s.Require().ErrorIs(err, loaderErr)
}
