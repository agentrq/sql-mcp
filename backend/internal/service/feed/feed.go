// Package feed fans events out to live subscribers, grouped by key. It is
// in-process: a subscriber sees what this server publishes, nothing else.
package feed

import "sync"

// Hub delivers each event published under a key to that key's subscribers.
// A subscriber that falls behind loses events rather than holding up the
// publisher, which is a query that has already run.
type Hub[T any] struct {
	mu   sync.Mutex
	subs map[int64]map[chan T]struct{}
}

// New is an empty hub.
func New[T any]() *Hub[T] {
	return &Hub[T]{subs: map[int64]map[chan T]struct{}{}}
}

// Subscribe returns key's events, buffering up to buffer of them, and the
// function that ends the subscription. The channel is closed when it ends,
// by that function or by Close.
func (h *Hub[T]) Subscribe(key int64, buffer int) (<-chan T, func()) {
	ch := make(chan T, buffer)
	h.mu.Lock()
	if h.subs[key] == nil {
		h.subs[key] = map[chan T]struct{}{}
	}
	h.subs[key][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		// Close may have ended it already; a channel is closed only by
		// whoever removes it, so never twice.
		if _, ok := h.subs[key][ch]; ok {
			delete(h.subs[key], ch)
			if len(h.subs[key]) == 0 {
				delete(h.subs, key)
			}
			close(ch)
		}
	}
}

// Publish sends v to key's subscribers, skipping any whose buffer is full.
func (h *Hub[T]) Publish(key int64, v T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[key] {
		select {
		case ch <- v:
		default:
		}
	}
}

// Close ends every subscription to key.
func (h *Hub[T]) Close(key int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[key] {
		close(ch)
	}
	delete(h.subs, key)
}
