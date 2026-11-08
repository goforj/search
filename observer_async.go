package search

import (
	"context"
	"sync"
	"sync/atomic"
)

// observedEvent retains only bounded pending callbacks, including their correlation context.
type observedEvent struct {
	ctx   context.Context
	event Event
}

// AsyncObserver delivers retained events in FIFO order from one bounded worker.
// Applications own Shutdown; an observer that blocks indefinitely also blocks its worker.
type AsyncObserver struct {
	mu       sync.RWMutex
	observer Observer
	queue    chan observedEvent
	done     chan struct{}
	closed   bool
	dropped  atomic.Uint64
}

// NewAsyncObserver starts a single delivery worker with capacity from 1 to 65536.
// @group Observability
func NewAsyncObserver(observer Observer, capacity int) (*AsyncObserver, error) {
	if capacity < 1 || capacity > 65536 {
		return nil, ErrInvalidDefinition
	}
	adapter := &AsyncObserver{observer: observer, queue: make(chan observedEvent, capacity), done: make(chan struct{})}
	go adapter.deliver()
	return adapter, nil
}

// Observe enqueues without blocking and counts full-queue or post-shutdown drops.
// @group Observability
func (a *AsyncObserver) Observe(ctx context.Context, event Event) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		a.dropped.Add(1)
		return
	}
	select {
	case a.queue <- observedEvent{ctx: ctx, event: event}:
	default:
		a.dropped.Add(1)
	}
}

// Dropped reports the cumulative number of events discarded by bounded delivery.
// @group Observability
func (a *AsyncObserver) Dropped() uint64 {
	return a.dropped.Load()
}

// Shutdown closes admission once and waits for retained callbacks within the caller's deadline.
// @group Observability
func (a *AsyncObserver) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.queue)
	}
	a.mu.Unlock()
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deliver isolates callback panics while keeping retained delivery sequential.
func (a *AsyncObserver) deliver() {
	defer close(a.done)
	for record := range a.queue {
		notify(record.ctx, a.observer, record.event)
	}
}
