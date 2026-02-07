package stash

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestGetSet(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)

	v, ok, err := c.Get(ctx, "a")
	if err != nil || !ok || v != 1 {
		t.Errorf("Get(a) = %d, %v, %v; want 1, true, nil", v, ok, err)
	}

	v, ok, err = c.Get(ctx, "b")
	if err != nil || !ok || v != 2 {
		t.Errorf("Get(b) = %d, %v, %v; want 2, true, nil", v, ok, err)
	}

	_, ok, err = c.Get(ctx, "c")
	if err != nil || ok {
		t.Errorf("Get(c) = _, %v, %v; want false, nil", ok, err)
	}
}

func TestSetUpdates(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	c.Set(ctx, "a", 1)
	c.Set(ctx, "a", 2)

	v, ok, _ := c.Get(ctx, "a")
	if !ok || v != 2 {
		t.Errorf("Get(a) = %d, %v; want 2, true", v, ok)
	}

	if c.Len() != 1 {
		t.Errorf("Len() = %d; want 1", c.Len())
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	c.Set(ctx, "a", 1)
	c.Delete(ctx, "a")

	_, ok, _ := c.Get(ctx, "a")
	if ok {
		t.Error("Get(a) after delete = _, true; want false")
	}
}

func TestHas(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	if c.Has("a") {
		t.Error("Has(a) = true; want false")
	}

	c.Set(ctx, "a", 1)

	if !c.Has("a") {
		t.Error("Has(a) = false; want true")
	}
}

func TestClear(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)
	c.Clear()

	if c.Len() != 0 {
		t.Errorf("Len() = %d; want 0", c.Len())
	}

	if c.Has("a") {
		t.Error("Has(a) after clear = true; want false")
	}
}

func TestTTL(t *testing.T) {
	ctx := context.Background()
	clk := &mockClock{now: time.Now()}
	c := New[string, int](
		WithTTL[string, int](time.Minute),
		WithClock[string, int](clk),
	)

	c.Set(ctx, "a", 1)

	v, ok, _ := c.Get(ctx, "a")
	if !ok || v != 1 {
		t.Errorf("Get(a) = %d, %v; want 1, true", v, ok)
	}

	clk.Advance(2 * time.Minute)

	_, ok, _ = c.Get(ctx, "a")
	if ok {
		t.Error("Get(a) after expiry = _, true; want false")
	}
}

func TestSetWithTTL(t *testing.T) {
	ctx := context.Background()
	clk := &mockClock{now: time.Now()}
	c := New[string, int](
		WithTTL[string, int](time.Hour),
		WithClock[string, int](clk),
	)

	c.SetWithTTL(ctx, "a", 1, time.Second)

	clk.Advance(2 * time.Second)

	_, ok, _ := c.Get(ctx, "a")
	if ok {
		t.Error("Get(a) after short TTL = _, true; want false")
	}
}

func TestCapacity(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithCapacity[string, int](2))

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)
	c.Set(ctx, "c", 3)

	if c.Len() != 2 {
		t.Errorf("Len() = %d; want 2", c.Len())
	}
}

func TestLRUEviction(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](LRU),
	)

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)

	// access a to make it recently used
	c.Get(ctx, "a")

	// add c, should evict b (least recently used)
	c.Set(ctx, "c", 3)

	if !c.Has("a") {
		t.Error("a should still exist")
	}
	if c.Has("b") {
		t.Error("b should be evicted")
	}
	if !c.Has("c") {
		t.Error("c should exist")
	}
}

func TestLFUEviction(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](LFU),
	)

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)

	// access a multiple times to increase frequency
	c.Get(ctx, "a")
	c.Get(ctx, "a")

	// add c, should evict b (least frequently used)
	c.Set(ctx, "c", 3)

	if !c.Has("a") {
		t.Error("a should still exist")
	}
	if c.Has("b") {
		t.Error("b should be evicted")
	}
	if !c.Has("c") {
		t.Error("c should exist")
	}
}

func TestFIFOEviction(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](FIFO),
	)

	c.Set(ctx, "a", 1)
	c.Set(ctx, "b", 2)

	// access a (shouldn't affect FIFO order)
	c.Get(ctx, "a")

	// add c, should evict a (oldest)
	c.Set(ctx, "c", 3)

	if c.Has("a") {
		t.Error("a should be evicted (first in)")
	}
	if !c.Has("b") {
		t.Error("b should still exist")
	}
	if !c.Has("c") {
		t.Error("c should exist")
	}
}

func TestCostEviction(t *testing.T) {
	ctx := context.Background()
	c := New[string, string](
		WithCapacity[string, string](100),
		WithMaxCost[string, string](10),
		WithCost[string, string](func(v string) int64 { return int64(len(v)) }),
	)

	c.Set(ctx, "a", "12345") // cost 5
	c.Set(ctx, "b", "12345") // cost 5, total 10

	if c.Len() != 2 {
		t.Errorf("Len() = %d; want 2", c.Len())
	}

	c.Set(ctx, "c", "123") // cost 3, should evict to make room

	if c.Len() > 2 {
		t.Errorf("Len() = %d; want <= 2", c.Len())
	}
}

func TestLoader(t *testing.T) {
	ctx := context.Background()
	loaded := 0
	c := New[string, int](
		WithLoader(func(key string) (int, error) {
			loaded++
			return len(key), nil
		}),
	)

	v, err := c.GetOrLoad(ctx, "abc")
	if err != nil {
		t.Fatalf("GetOrLoad error: %v", err)
	}
	if v != 3 {
		t.Errorf("GetOrLoad = %d; want 3", v)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d; want 1", loaded)
	}

	// second call should use cache
	v, err = c.GetOrLoad(ctx, "abc")
	if err != nil {
		t.Fatalf("GetOrLoad error: %v", err)
	}
	if v != 3 {
		t.Errorf("GetOrLoad = %d; want 3", v)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d; want 1 (cached)", loaded)
	}
}

func TestLoaderError(t *testing.T) {
	ctx := context.Background()
	testErr := errors.New("load failed")
	c := New[string, int](
		WithLoader(func(key string) (int, error) {
			return 0, testErr
		}),
	)

	_, err := c.GetOrLoad(ctx, "a")
	if !errors.Is(err, testErr) {
		t.Errorf("GetOrLoad error = %v; want %v", err, testErr)
	}

	// should not be cached
	if c.Has("a") {
		t.Error("failed load should not cache")
	}
}

func TestLoaderSingleFlight(t *testing.T) {
	ctx := context.Background()
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
			results[idx], errs[idx] = c.GetOrLoad(ctx, "key")
		}(i)
	}

	// give goroutines time to start and coalesce on the same load call
	time.Sleep(10 * time.Millisecond)

	// signal to proceed
	close(proceed)
	wg.Wait()

	if loadCount.Load() != 1 {
		t.Errorf("load count = %d; want 1 (single-flight)", loadCount.Load())
	}

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error: %v", i, err)
		}
		if results[i] != 42 {
			t.Errorf("goroutine %d result = %d; want 42", i, results[i])
		}
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithCapacity[string, int](2))

	c.Set(ctx, "a", 1)
	c.Get(ctx, "a") // hit
	c.Get(ctx, "b") // miss
	c.Set(ctx, "b", 2)
	c.Set(ctx, "c", 3) // eviction

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Errorf("Hits = %d; want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses = %d; want 1", stats.Misses)
	}
	if stats.Evictions != 1 {
		t.Errorf("Evictions = %d; want 1", stats.Evictions)
	}
}

func TestHitRate(t *testing.T) {
	s := Stats{Hits: 3, Misses: 1}
	if s.HitRate() != 0.75 {
		t.Errorf("HitRate() = %f; want 0.75", s.HitRate())
	}

	s = Stats{}
	if s.HitRate() != 0 {
		t.Errorf("HitRate() with no accesses = %f; want 0", s.HitRate())
	}
}

func TestCallbacks(t *testing.T) {
	ctx := context.Background()
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

	c.Set(ctx, "a", 1)
	c.Get(ctx, "a")
	if hitKey != "a" || hitVal != 1 {
		t.Errorf("OnHit called with %s, %d; want a, 1", hitKey, hitVal)
	}

	c.Get(ctx, "b")
	if missKey != "b" {
		t.Errorf("OnMiss called with %s; want b", missKey)
	}

	c.Set(ctx, "c", 3) // evicts a
	if evictKey != "a" || evictVal != 1 {
		t.Errorf("OnEvict called with %s, %d; want a, 1", evictKey, evictVal)
	}
}

func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	c := New[int, int](WithCapacity[int, int](100))

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Set(ctx, n, n*2)
			c.Get(ctx, n)
			c.Has(n)
			c.Delete(ctx, n)
		}(i)
	}
	wg.Wait()
}

func TestHasWithExpiry(t *testing.T) {
	ctx := context.Background()
	clk := &mockClock{now: time.Now()}
	c := New[string, int](
		WithTTL[string, int](time.Minute),
		WithClock[string, int](clk),
	)

	c.Set(ctx, "a", 1)

	if !c.Has("a") {
		t.Error("Has(a) = false; want true")
	}

	clk.Advance(2 * time.Minute)

	if c.Has("a") {
		t.Error("Has(a) after expiry = true; want false")
	}
}

func TestGetOrLoadWithoutLoader(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	c.Set(ctx, "a", 1)
	v, err := c.GetOrLoad(ctx, "a")
	if err != nil || v != 1 {
		t.Errorf("GetOrLoad(a) = %d, %v; want 1, nil", v, err)
	}

	v, err = c.GetOrLoad(ctx, "b")
	if err != nil || v != 0 {
		t.Errorf("GetOrLoad(b) = %d, %v; want 0, nil", v, err)
	}
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

func (s *mockStore[K, V]) Delete(_ context.Context, key K) error {
	delete(s.data, key)
	return nil
}

func TestStoreGet(t *testing.T) {
	ctx := context.Background()
	store := newMockStore[string, int]()
	store.data["external"] = 42

	c := New[string, int](WithStore(store))

	// not in memory, should fetch from store
	v, ok, err := c.Get(ctx, "external")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !ok || v != 42 {
		t.Errorf("Get = %d, %v; want 42, true", v, ok)
	}

	// should now be in memory
	if !c.Has("external") {
		t.Error("Has after store fetch = false; want true")
	}
}

func TestStoreSet(t *testing.T) {
	ctx := context.Background()
	store := newMockStore[string, int]()
	c := New[string, int](WithStore(store))

	err := c.Set(ctx, "key", 123)
	if err != nil {
		t.Fatalf("Set error: %v", err)
	}

	// should be in memory
	if !c.Has("key") {
		t.Error("Has = false; want true")
	}

	// should be in store
	if store.data["key"] != 123 {
		t.Errorf("store.data[key] = %d; want 123", store.data["key"])
	}
}

func TestStoreDelete(t *testing.T) {
	ctx := context.Background()
	store := newMockStore[string, int]()
	store.data["key"] = 1

	c := New[string, int](WithStore(store))
	c.Set(ctx, "key", 1)

	err := c.Delete(ctx, "key")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// should be gone from memory
	if c.Has("key") {
		t.Error("Has(key) after delete = true; want false")
	}

	// should be gone from store
	if _, ok := store.data["key"]; ok {
		t.Error("key still in store after delete")
	}
}

func TestGetWithoutStore(t *testing.T) {
	ctx := context.Background()
	c := New[string, int]()

	c.Set(ctx, "a", 1)
	v, ok, err := c.Get(ctx, "a")
	if err != nil || !ok || v != 1 {
		t.Errorf("Get(a) = %d, %v, %v; want 1, true, nil", v, ok, err)
	}

	v, ok, err = c.Get(ctx, "b")
	if err != nil || ok || v != 0 {
		t.Errorf("Get(b) = %d, %v, %v; want 0, false, nil", v, ok, err)
	}
}

func TestLFUDelete(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithPolicy[string, int](LFU))

	c.Set(ctx, "a", 1)
	c.Get(ctx, "a") // increase frequency
	c.Delete(ctx, "a")

	if c.Has("a") {
		t.Error("Has(a) after delete = true; want false")
	}
}

func TestFIFODelete(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithPolicy[string, int](FIFO))

	c.Set(ctx, "a", 1)
	c.Delete(ctx, "a")

	if c.Has("a") {
		t.Error("Has(a) after delete = true; want false")
	}
}

type errorStore[K comparable, V any] struct{}

func (s *errorStore[K, V]) Get(_ context.Context, _ K) (V, bool, error) {
	var zero V
	return zero, false, errors.New("store error")
}

func (s *errorStore[K, V]) Set(_ context.Context, _ K, _ V, _ time.Duration) error {
	return errors.New("store error")
}

func (s *errorStore[K, V]) Delete(_ context.Context, _ K) error {
	return errors.New("store error")
}

func TestStoreGetError(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	_, _, err := c.Get(ctx, "key")
	if err == nil {
		t.Error("Get with store error should return error")
	}
}

func TestStoreSetError(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.Set(ctx, "key", 1)
	if err == nil {
		t.Error("Set with store error should return error")
	}
}

func TestStoreDeleteError(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.Delete(ctx, "key")
	if err == nil {
		t.Error("Delete with store error should return error")
	}
}

func TestSetWithTTLStore(t *testing.T) {
	ctx := context.Background()
	store := newMockStore[string, int]()
	c := New[string, int](WithStore(store))

	err := c.SetWithTTL(ctx, "key", 123, time.Hour)
	if err != nil {
		t.Fatalf("SetWithTTL error: %v", err)
	}

	if store.data["key"] != 123 {
		t.Errorf("store.data[key] = %d; want 123", store.data["key"])
	}
}

func TestGetOrLoadWithStore(t *testing.T) {
	ctx := context.Background()
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
	v, err := c.GetOrLoad(ctx, "external")
	if err != nil {
		t.Fatalf("GetOrLoad error: %v", err)
	}
	if v != 99 {
		t.Errorf("GetOrLoad = %d; want 99 (from store)", v)
	}
	if loaded != 0 {
		t.Error("loader should not be called when value is in store")
	}

	// should call loader for missing key
	v, err = c.GetOrLoad(ctx, "missing")
	if err != nil {
		t.Fatalf("GetOrLoad error: %v", err)
	}
	if v != 42 {
		t.Errorf("GetOrLoad = %d; want 42 (from loader)", v)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d; want 1", loaded)
	}
}

func TestGetOrLoadStoreError(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithStore[string, int](&errorStore[string, int]{}),
		WithLoader(func(key string) (int, error) {
			return 42, nil
		}),
	)

	_, err := c.GetOrLoad(ctx, "key")
	if err == nil {
		t.Error("GetOrLoad with store error should return error")
	}
}

func TestSetWithTTLStoreError(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithStore[string, int](&errorStore[string, int]{}))

	err := c.SetWithTTL(ctx, "key", 1, time.Hour)
	if err == nil {
		t.Error("SetWithTTL with store error should return error")
	}
}

func TestFIFOAccess(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](FIFO),
	)

	c.Set(ctx, "a", 1)
	// access should be a no-op for FIFO but still gets called
	c.Get(ctx, "a")
	c.Get(ctx, "a")

	if !c.Has("a") {
		t.Error("a should still exist")
	}
}

func TestLFUInsertExisting(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](LFU),
	)

	c.Set(ctx, "a", 1)
	c.Get(ctx, "a")    // freq 2
	c.Set(ctx, "a", 2) // update existing, should increase freq

	v, ok, _ := c.Get(ctx, "a")
	if !ok || v != 2 {
		t.Errorf("Get(a) = %d, %v; want 2, true", v, ok)
	}
}

func TestFIFOInsertExisting(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](
		WithCapacity[string, int](2),
		WithPolicy[string, int](FIFO),
	)

	c.Set(ctx, "a", 1)
	c.Set(ctx, "a", 2) // update existing, FIFO should not move it

	v, ok, _ := c.Get(ctx, "a")
	if !ok || v != 2 {
		t.Errorf("Get(a) = %d, %v; want 2, true", v, ok)
	}
}

func TestLFUAccessNonExistent(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithPolicy[string, int](LFU))

	// access non-existent key (miss path)
	_, ok, _ := c.Get(ctx, "nonexistent")
	if ok {
		t.Error("Get(nonexistent) = _, true; want false")
	}
}

func TestLFURemoveNonExistent(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithPolicy[string, int](LFU))

	// delete non-existent key
	c.Delete(ctx, "nonexistent")
	// should not panic
}

func TestFIFORemoveNonExistent(t *testing.T) {
	ctx := context.Background()
	c := New[string, int](WithPolicy[string, int](FIFO))

	// delete non-existent key
	c.Delete(ctx, "nonexistent")
	// should not panic
}
