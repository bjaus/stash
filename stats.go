package stash

// Stats holds cache statistics.
type Stats struct {
	Hits      int64
	Misses    int64
	Evictions int64
}

// HitRate returns the cache hit rate as a value between 0 and 1.
// Returns 0 if there have been no accesses.
func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}
