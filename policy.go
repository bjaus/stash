package stash

import "container/list"

// Policy defines the eviction policy for the cache.
type Policy int

const (
	// LRU evicts the least recently used entry.
	LRU Policy = iota
	// LFU evicts the least frequently used entry.
	LFU
	// FIFO evicts the oldest entry.
	FIFO
)

// evictor manages eviction order for cache entries.
type evictor[K comparable] interface {
	onAccess(key K)
	onInsert(key K)
	evict() K
	remove(key K)
}

// Compile-time interface assertions.
var (
	_ evictor[string] = (*lruEvictor[string])(nil)
	_ evictor[string] = (*lfuEvictor[string])(nil)
	_ evictor[string] = (*fifoEvictor[string])(nil)
)

// lruEvictor implements LRU eviction using a doubly-linked list.
type lruEvictor[K comparable] struct {
	order *list.List
	items map[K]*list.Element
}

func newLRUEvictor[K comparable]() *lruEvictor[K] {
	return &lruEvictor[K]{
		order: list.New(),
		items: make(map[K]*list.Element),
	}
}

func (e *lruEvictor[K]) onAccess(key K) {
	if elem, ok := e.items[key]; ok {
		e.order.MoveToFront(elem)
	}
}

func (e *lruEvictor[K]) onInsert(key K) {
	if elem, ok := e.items[key]; ok {
		e.order.MoveToFront(elem)
		return
	}
	elem := e.order.PushFront(key)
	e.items[key] = elem
}

func (e *lruEvictor[K]) evict() K {
	elem := e.order.Back()
	if elem == nil {
		var zero K
		return zero
	}
	key := elem.Value.(K)
	e.order.Remove(elem)
	delete(e.items, key)
	return key
}

func (e *lruEvictor[K]) remove(key K) {
	if elem, ok := e.items[key]; ok {
		e.order.Remove(elem)
		delete(e.items, key)
	}
}

// lfuEvictor implements LFU eviction using frequency buckets.
type lfuEvictor[K comparable] struct {
	freqs   map[int64]*list.List // freq -> list of keys
	items   map[K]*list.Element  // key -> element in freq list
	keyFreq map[K]int64          // key -> frequency
	minFreq int64
}

func newLFUEvictor[K comparable]() *lfuEvictor[K] {
	return &lfuEvictor[K]{
		freqs:   make(map[int64]*list.List),
		items:   make(map[K]*list.Element),
		keyFreq: make(map[K]int64),
		minFreq: 0,
	}
}

func (e *lfuEvictor[K]) onAccess(key K) {
	freq, ok := e.keyFreq[key]
	if !ok {
		return
	}

	// remove from current freq list
	elem := e.items[key]
	e.freqs[freq].Remove(elem)
	if e.freqs[freq].Len() == 0 {
		delete(e.freqs, freq)
		if e.minFreq == freq {
			e.minFreq++
		}
	}

	// add to next freq list
	freq++
	e.keyFreq[key] = freq
	if e.freqs[freq] == nil {
		e.freqs[freq] = list.New()
	}
	e.items[key] = e.freqs[freq].PushFront(key)
}

func (e *lfuEvictor[K]) onInsert(key K) {
	if _, ok := e.keyFreq[key]; ok {
		e.onAccess(key)
		return
	}

	e.keyFreq[key] = 1
	if e.freqs[1] == nil {
		e.freqs[1] = list.New()
	}
	e.items[key] = e.freqs[1].PushFront(key)
	e.minFreq = 1
}

func (e *lfuEvictor[K]) evict() K {
	if len(e.keyFreq) == 0 {
		var zero K
		return zero
	}

	freqList := e.freqs[e.minFreq]
	elem := freqList.Back()
	key := elem.Value.(K)

	freqList.Remove(elem)
	if freqList.Len() == 0 {
		delete(e.freqs, e.minFreq)
	}
	delete(e.items, key)
	delete(e.keyFreq, key)

	return key
}

func (e *lfuEvictor[K]) remove(key K) {
	freq, ok := e.keyFreq[key]
	if !ok {
		return
	}

	elem := e.items[key]
	e.freqs[freq].Remove(elem)
	if e.freqs[freq].Len() == 0 {
		delete(e.freqs, freq)
	}
	delete(e.items, key)
	delete(e.keyFreq, key)
}

// fifoEvictor implements FIFO eviction using a queue.
type fifoEvictor[K comparable] struct {
	order *list.List
	items map[K]*list.Element
}

func newFIFOEvictor[K comparable]() *fifoEvictor[K] {
	return &fifoEvictor[K]{
		order: list.New(),
		items: make(map[K]*list.Element),
	}
}

func (e *fifoEvictor[K]) onAccess(key K) {
	// FIFO doesn't change order on access
}

func (e *fifoEvictor[K]) onInsert(key K) {
	if _, ok := e.items[key]; ok {
		return // FIFO doesn't move on update
	}
	elem := e.order.PushFront(key)
	e.items[key] = elem
}

func (e *fifoEvictor[K]) evict() K {
	elem := e.order.Back()
	if elem == nil {
		var zero K
		return zero
	}
	key := elem.Value.(K)
	e.order.Remove(elem)
	delete(e.items, key)
	return key
}

func (e *fifoEvictor[K]) remove(key K) {
	if elem, ok := e.items[key]; ok {
		e.order.Remove(elem)
		delete(e.items, key)
	}
}

func newEvictor[K comparable](p Policy) evictor[K] {
	switch p {
	case LFU:
		return newLFUEvictor[K]()
	case FIFO:
		return newFIFOEvictor[K]()
	default:
		return newLRUEvictor[K]()
	}
}
