// Package redissearch implements bounded, exact lexical search with Redis Search on a standalone Redis service.
package redissearch

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// MaxMatches bounds complete match collection needed for portable multi-sort and exact array facets.
// Queries above this limit return ErrUnsupported rather than silently returning incomplete results.
const MaxMatches = 4096

// Config owns standalone Redis connection policy without changing persistent client authentication.
// Addr is a host:port pair. Timeout defaults to ten seconds and cannot exceed thirty seconds.
// TLSConfig is cloned; certificate verification remains enabled unless the caller explicitly changes it.
type Config struct {
	Addr      string
	Username  string
	Password  string
	DB        int
	Prefix    string
	Timeout   time.Duration
	TLSConfig *tls.Config
}

// Driver owns one explicitly provisioned Redis Search index and process-local receipt state.
type Driver struct {
	mu          sync.RWMutex
	config      Config
	name        string
	fingerprint string
	closed      bool
	ledger      driverutil.Ledger
}

// New validates configuration without connecting or provisioning native state.
// @group Construction
func New(config Config) (*Driver, error) {
	if config.Timeout == 0 {
		config.Timeout = timeoutDefault
	}
	if _, _, err := net.SplitHostPort(config.Addr); err != nil {
		return nil, search.ErrInvalidDefinition
	}
	if config.DB < 0 || config.DB > 15 || config.Timeout < time.Millisecond || config.Timeout > 30*time.Second || len(config.Prefix) > 64 || config.Username != "" && config.Password == "" {
		return nil, search.ErrInvalidDefinition
	}
	if strings.TrimSpace(config.Addr) != config.Addr {
		return nil, search.ErrInvalidDefinition
	}
	for _, r := range config.Prefix {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return nil, search.ErrInvalidDefinition
		}
	}
	if config.TLSConfig != nil {
		config.TLSConfig = config.TLSConfig.Clone()
	}
	return &Driver{config: config}, nil
}

// target makes physical keys inert to native query syntax and logical-name length.
func (d *Driver) target(def search.Definition) string {
	hash := sha256.Sum256([]byte(def.Name))
	return d.config.Prefix + "s_" + hex.EncodeToString(hash[:16])
}

// ready requires an exact validated schema before constructing native request targets.
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
	fingerprint, err := def.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != d.fingerprint {
		return search.ErrSchemaDrift
	}
	return nil
}

// Close drains admitted calls; every connection is already scoped to a single operation.
// @group Backend
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Backend conformance is checked at compile time.
var _ search.Backend = (*Driver)(nil)
