package driverutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"

	"github.com/goforj/search"
)

// Ledger bounds process-local terminal evidence to the most recent 4096 receipts.
// Expired receipts return an honest terminal unknown outcome.
type Ledger struct {
	mu      sync.Mutex
	entries map[string]search.Outcome
	order   []string
}

// Complete retains a terminal outcome under a random content-free identity.
func (l *Ledger) Complete(o search.Outcome) (search.Receipt, error) {
	var key [16]byte
	rand.Read(key[:])
	token := hex.EncodeToString(key[:])
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]search.Outcome{}
	}
	if len(l.order) >= 4096 {
		delete(l.entries, l.order[0])
		l.order = l.order[1:]
	}
	l.order = append(l.order, token)
	if o.Bulk != nil {
		bulk := *o.Bulk
		bulk.Items = slices.Clone(bulk.Items)
		o.Bulk = &bulk
	}
	l.entries[token] = o
	return search.NewReceipt([]byte(token))
}

// Wait returns detached terminal evidence or classified expiration.
func (l *Ledger) Wait(ctx context.Context, r search.Receipt) (search.Outcome, error) {
	if err := ctx.Err(); err != nil {
		return search.Outcome{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	o, ok := l.entries[string(r.Token())]
	if !ok {
		return search.Outcome{State: search.OutcomeUnknown, Error: search.ErrReceiptExpired}, nil
	}
	if o.Bulk != nil {
		b := *o.Bulk
		b.Items = slices.Clone(b.Items)
		o.Bulk = &b
	}
	return o, nil
}
