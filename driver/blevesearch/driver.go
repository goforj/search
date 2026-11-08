// Package blevesearch implements embedded lexical search with Bleve.
// Persistent indexes have one process owner; use a service driver across processes.
package blevesearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
	"github.com/gofrs/flock"
)

// Config selects a single-index persistent path or an explicit in-memory index.
type Config struct {
	Path   string
	Memory bool
}

// Driver owns one embedded index and its process lock. Close releases both.
type Driver struct {
	mu          sync.RWMutex
	cfg         Config
	index       bleve.Index
	lock        *flock.Flock
	closed      bool
	fingerprint string
	ledger      driverutil.Ledger
}

// Open acquires ownership without creating or modifying an index schema.
// @group Construction
func Open(ctx context.Context, cfg Config) (*Driver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Memory && cfg.Path != "" || !cfg.Memory && cfg.Path == "" {
		return nil, search.ErrInvalidDefinition
	}
	d := &Driver{cfg: cfg}
	if cfg.Memory {
		return d, nil
	}
	abs, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, search.ErrUnavailable
	}
	d.cfg.Path = abs
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, search.ErrUnavailable
	}
	d.lock = flock.New(abs + ".owner.lock")
	ok, err := d.lock.TryLock()
	if err != nil {
		return nil, search.ErrUnavailable
	}
	if !ok {
		return nil, search.ErrConflict
	}
	if _, err = os.Stat(abs); errors.Is(err, os.ErrNotExist) {
		return d, nil
	} else if err != nil {
		_ = d.lock.Unlock()
		return nil, search.ErrUnavailable
	}
	d.index, err = bleve.Open(abs)
	if err != nil {
		_ = d.lock.Unlock()
		return nil, search.ErrUnavailable
	}
	return d, nil
}

// check keeps cancellation and closure visible at every entry point.
func (d *Driver) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed {
		return search.ErrClosed
	}
	return nil
}

// ready refuses request-path provisioning or a different definition.
func (d *Driver) ready(ctx context.Context, def search.Definition) error {
	if err := d.check(ctx); err != nil {
		return err
	}
	if d.index == nil {
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

// Close waits for admitted calls before releasing the embedded index and ownership lock.
// @group Backend
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	var err error
	if d.index != nil {
		err = search.SafeError(d.index.Close())
	}
	if d.lock != nil {
		err = errors.Join(err, search.SafeError(d.lock.Unlock()))
	}
	return err
}

// Ensure external import changes cannot accidentally remove the request-path contract.
var _ search.Backend = (*Driver)(nil)
