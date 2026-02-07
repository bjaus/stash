package stash

import "time"

// Clock provides time operations for the cache.
// The default implementation uses time.Now().
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}
