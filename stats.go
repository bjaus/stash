package stash

import "sync/atomic"

// Stats holds cache statistics using atomic counters for lock-free updates.
type Stats struct {
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
}

// Hits returns the number of cache hits.
func (s *Stats) Hits() int64 {
	return s.hits.Load()
}

// Misses returns the number of cache misses.
func (s *Stats) Misses() int64 {
	return s.misses.Load()
}

// Evictions returns the number of evictions.
func (s *Stats) Evictions() int64 {
	return s.evictions.Load()
}

// HitRate returns the cache hit rate as a value between 0 and 1.
// Returns 0 if there have been no accesses.
func (s *Stats) HitRate() float64 {
	hits := s.hits.Load()
	misses := s.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

func (s *Stats) hit() {
	s.hits.Add(1)
}

func (s *Stats) miss() {
	s.misses.Add(1)
}

func (s *Stats) evict() {
	s.evictions.Add(1)
}

// Snapshot is a point-in-time copy of cache statistics.
type Snapshot struct {
	Hits      int64
	Misses    int64
	Evictions int64
}

// HitRate returns the cache hit rate as a value between 0 and 1.
// Returns 0 if there have been no accesses.
func (s Snapshot) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Snapshot returns a point-in-time copy of the stats.
func (s *Stats) Snapshot() Snapshot {
	return Snapshot{
		Hits:      s.hits.Load(),
		Misses:    s.misses.Load(),
		Evictions: s.evictions.Load(),
	}
}
