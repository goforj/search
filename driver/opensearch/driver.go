// Package opensearch implements service-backed lexical search with OpenSearch.
package opensearch

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// MaxKeywordBytes keeps the physical hexadecimal term below Lucene's term-byte limit.
const MaxKeywordBytes = 16382

// Config owns connection policy and materialized index settings.
// Shards defaults to one and accepts 1 through 16. Replicas accepts 0 through 3.
// RefreshInterval defaults to one second and accepts 100 milliseconds through five seconds.
// Basic credentials are optional; custom signing transports belong in HTTPClient.
type Config struct {
	URL             string
	Username        string
	Password        string
	Prefix          string
	Shards          int
	Replicas        int
	RefreshInterval time.Duration
	HTTPClient      *http.Client
}

// Driver owns a concurrent client for one explicitly provisioned logical index.
type Driver struct {
	mu          sync.RWMutex
	http        *driverutil.HTTP
	config      Config
	name        string
	fingerprint string
	closed      bool
	ledger      driverutil.Ledger
}

// New validates configuration without contacting the cluster or changing index state.
// @group Construction
func New(config Config) (*Driver, error) {
	if config.Shards == 0 {
		config.Shards = 1
	}
	if config.RefreshInterval == 0 {
		config.RefreshInterval = time.Second
	}
	if config.Shards < 1 || config.Shards > 16 || config.Replicas < 0 || config.Replicas > 3 || config.RefreshInterval < 100*time.Millisecond || config.RefreshInterval > 5*time.Second {
		return nil, search.ErrInvalidDefinition
	}
	if len(config.Prefix) > 64 || strings.Contains(config.Username, ":") || (config.Username == "") != (config.Password == "") {
		return nil, search.ErrInvalidDefinition
	}
	for n, r := range config.Prefix {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || n > 0 && (r == '_' || r == '-')) {
			return nil, search.ErrInvalidDefinition
		}
	}
	client, err := driverutil.NewHTTP(config.URL, "", "", config.HTTPClient)
	if err != nil {
		return nil, err
	}
	if config.Username != "" {
		client.Authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte(config.Username+":"+config.Password))
	}
	return &Driver{http: client, config: config}, nil
}

// target bounds physical names independently of logical UTF-8 length while retaining collision resistance.
func (d *Driver) target(definition search.Definition) string {
	hash := sha256.Sum256([]byte(definition.Name))
	return d.config.Prefix + "s_" + hex.EncodeToString(hash[:16])
}

// ready keeps request paths inert until an exact definition has been validated.
func (d *Driver) ready(ctx context.Context, definition search.Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed {
		return search.ErrClosed
	}
	if d.name == "" {
		return search.ErrIndexNotFound
	}
	fingerprint, err := definition.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != d.fingerprint {
		return search.ErrSchemaDrift
	}
	return nil
}

// Close drains admitted calls and releases only connection pools owned by this driver.
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
