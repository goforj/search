// Package typesensesearch implements service-backed search with Typesense.
package typesensesearch

import (
	"context"
	"net/http"
	"sync"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Config supplies server-owned credentials and an optional physical namespace.
type Config struct {
	URL        string
	APIKey     string
	Prefix     string
	HTTPClient *http.Client
}

// Driver owns one validated collection and bounded process-local mutation evidence.
type Driver struct {
	mu          sync.RWMutex
	http        *driverutil.HTTP
	prefix      string
	name        string
	fingerprint string
	closed      bool
	ledger      driverutil.Ledger
}

// New validates configuration without provisioning or contacting Typesense.
// @group Construction
func New(cfg Config) (*Driver, error) {
	h, err := driverutil.NewHTTP(cfg.URL, cfg.APIKey, "X-TYPESENSE-API-KEY", cfg.HTTPClient)
	if err != nil {
		return nil, err
	}
	if len(cfg.Prefix) > 64 {
		return nil, search.ErrInvalidDefinition
	}
	for _, r := range cfg.Prefix {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return nil, search.ErrInvalidDefinition
		}
	}
	return &Driver{http: h, prefix: cfg.Prefix}, nil
}

// target keeps logical names out of URL and backend query syntax.
func (d *Driver) target(def search.Definition) string {
	return d.prefix + "s_" + driverutil.FieldKey(search.FieldName(def.Name))
}

// ready admits calls only for the provisioned definition.
func (d *Driver) ready(ctx context.Context, def search.Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed {
		return search.ErrClosed
	}
	if d.name == "" {
		return search.ErrIndexNotFound
	}
	fp, err := def.Fingerprint()
	if err != nil {
		return err
	}
	if fp != d.fingerprint {
		return search.ErrSchemaDrift
	}
	return nil
}

// Close prevents new operations after admitted calls have completed.
// @group Backend
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.http.Close()
	return nil
}

// Backend conformance is checked at compile time.
var _ search.Backend = (*Driver)(nil)
