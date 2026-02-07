package stash

import "time"

type entry[V any] struct {
	value     V
	expiresAt time.Time // zero means no expiry
	cost      int64
	freq      int64 // for LFU
}

func (e *entry[V]) isExpired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}
