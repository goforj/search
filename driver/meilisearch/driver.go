// Package meilisearch implements asynchronous service-backed lexical search.
package meilisearch

import (
	"context"
	"net/http"
	"sync"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// MaxFacetValues bounds the distinct terms collected per requested facet before portable count-and-value ordering.
// A query exceeding this cardinality returns search.ErrUnsupported instead of an incorrectly selected subset.
const MaxFacetValues = 4096

// Config owns service credentials, physical namespace, and optional transport policy.
type Config struct {
	URL        string
	APIKey     string
	Prefix     string
	HTTPClient *http.Client
}

// Driver is a concurrent client for one provisioned logical index.
type Driver struct {
	mu          sync.RWMutex
	http        *driverutil.HTTP
	prefix      string
	name        string
	fingerprint string
	closed      bool
	pending     map[string]*pending
	order       []string
}

// pending retains bounded batch evidence for the process-local receipt lifetime.
type pending struct {
	uncertain bool
	items     []search.ItemResult
	def       search.Definition
	tasks     []uint64
}

// New validates transport configuration without contacting the server.
// @group Construction
func New(cfg Config) (*Driver, error) {
	h, e := driverutil.NewHTTP(cfg.URL, cfg.APIKey, "Authorization", cfg.HTTPClient)
	if e != nil {
		return nil, e
	}
	if !safePrefix(cfg.Prefix) {
		return nil, search.ErrInvalidDefinition
	}
	return &Driver{http: h, prefix: cfg.Prefix, pending: map[string]*pending{}}, nil
}

// safePrefix restricts physical deployment names independently of application schemas.
func safePrefix(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return len(s) <= 64
}

// target encodes logical names without allowing backend path delimiters.
func (d *Driver) target(def search.Definition) string {
	return d.prefix + "s_" + driverutil.FieldKey(search.FieldName(def.Name))
}

// ready admits only the exact provisioned definition and leaves unrelated construction inert.
func (d *Driver) ready(ctx context.Context, def search.Definition) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if d.closed {
		return search.ErrClosed
	}
	fp, e := def.Fingerprint()
	if e != nil {
		return e
	}
	if d.name == "" {
		return search.ErrIndexNotFound
	}
	if fp != d.fingerprint {
		return search.ErrSchemaDrift
	}
	return nil
}

// Close prevents new calls without closing application-owned HTTP transports.
// @group Backend
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Backend conformance is checked at compile time.
var _ search.Backend = (*Driver)(nil)
