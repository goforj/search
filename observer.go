package search

import (
	"context"
	"time"
)

// EventPhase distinguishes submission latency from visibility and search latency.
type EventPhase string

const (
	// PhaseSubmission identifies accepted-work submission.
	PhaseSubmission EventPhase = "submission"
	// PhaseTerminal identifies terminal mutation observation.
	PhaseTerminal EventPhase = "terminal"
	// PhaseSearchComplete identifies a complete search call.
	PhaseSearchComplete EventPhase = "search_complete"
)

// Event contains bounded metadata without document, query, or backend prose.
type Event struct {
	Kind          string
	ReceiptID     string
	Phase         EventPhase
	Index         IndexName
	Duration      time.Duration
	DocumentCount int
	HitCount      int
	TotalRelation TotalRelation
	ErrorCode     string
	Outcome       string
}

// Observer receives synchronous events after driver locks have been released.
type Observer interface{ Observe(context.Context, Event) }

// ObserverFunc adapts an application callback to an Observer.
type ObserverFunc func(context.Context, Event)

// Observe invokes the application callback.
// @group Observability
func (f ObserverFunc) Observe(ctx context.Context, e Event) {
	f(ctx, e)
}

// MultiObserver delivers events in registration order with independent panic isolation.
type MultiObserver []Observer

// Observe ensures a failed observer does not suppress later observers.
// @group Observability
func (m MultiObserver) Observe(ctx context.Context, e Event) {
	for _, o := range m {
		notify(ctx, o, e)
	}
}

// notify contains observer panics so accepted work and successful reads stay successful.
func notify(ctx context.Context, o Observer, e Event) {
	defer func() {
		_ = recover()
	}()
	o.Observe(ctx, e)
}
