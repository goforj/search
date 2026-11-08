package driverutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goforj/search"
)

// MaxRequestTimeout keeps the client deadline below the service driver's validated native cutoff.
const MaxRequestTimeout = 30 * time.Second

// HTTP owns a restricted, bounded transport without redirect credential forwarding.
type HTTP struct {
	Authorization string
	owned         bool
	Base          string
	Key           string
	Header        string
	Client        *http.Client
	MaxBytes      int64
}

// NewHTTP validates an origin and copies client policy without changing shared transports.
func NewHTTP(base, key, header string, client *http.Client) (*HTTP, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, search.ErrInvalidDefinition
	}
	c := http.Client{Timeout: MaxRequestTimeout}
	if client != nil {
		c = *client
	}
	if c.Timeout <= 0 || c.Timeout > MaxRequestTimeout {
		c.Timeout = MaxRequestTimeout
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	owned := c.Transport == nil
	if owned {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.MaxResponseHeaderBytes = 64 << 10
		t.ResponseHeaderTimeout = 15 * time.Second
		t.DisableCompression = true
		c.Transport = t
	}
	return &HTTP{Base: strings.TrimRight(base, "/"), Key: key, Header: header, Client: &c, MaxBytes: 16 << 20, owned: owned}, nil
}

// Do marshals JSON and treats malformed accepted-mutation responses as unknown outcomes.
func (h *HTTP) Do(ctx context.Context, method, path string, body any, out any, mutation bool) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return search.ErrInvalidDocument
		}
	}
	response, err := h.DoBytes(ctx, method, path, "application/json", data, mutation)
	if err != nil {
		return err
	}
	if out != nil {
		if err = json.Unmarshal(response, out); err != nil {
			if mutation {
				return search.ErrOutcomeUnknown
			}
			return search.ErrUnavailable
		}
	}
	return nil
}

// DoBytes bounds decoded responses and never exposes backend prose or redirect targets.
func (h *HTTP) DoBytes(ctx context.Context, method, path, contentType string, body []byte, mutation bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, h.Base+path, bytes.NewReader(body))
	if err != nil {
		return nil, search.ErrUnavailable
	}
	req.Header.Set("Content-Type", contentType)
	if h.Authorization != "" {
		req.Header.Set("Authorization", h.Authorization)
	} else if h.Key != "" {
		value := h.Key
		if h.Header == "Authorization" {
			value = "Bearer " + value
		}
		req.Header.Set(h.Header, value)
	}
	response, err := h.Client.Do(req)
	if err != nil {
		if mutation {
			return nil, unknownMutationError(err)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, search.ErrUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, h.MaxBytes+1))
	if err != nil || int64(len(data)) > h.MaxBytes {
		if mutation {
			return nil, unknownMutationError(err)
		}
		return nil, search.ErrUnavailable
	}
	switch response.StatusCode {
	case 401, 403:
		return nil, search.ErrUnauthorized
	case 404:
		return nil, search.ErrIndexNotFound
	case 409:
		return nil, search.ErrConflict
	case 429:
		return nil, search.ErrRateLimited
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if mutation && response.StatusCode >= 500 {
			return nil, unknownMutationError(err)
		}
		return nil, &StatusError{Code: response.StatusCode, classification: search.ErrUnavailable}
	}
	return data, nil
}

// unknownMutationError retains only context classifications when transport loss follows possible acceptance.
func unknownMutationError(err error) error {
	if errors.Is(err, context.Canceled) {
		return errors.Join(search.ErrOutcomeUnknown, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(search.ErrOutcomeUnknown, context.DeadlineExceeded)
	}
	return search.ErrOutcomeUnknown
}

// Close releases only idle connections owned by this client, preserving application-supplied transports.
func (h *HTTP) Close() {
	if h.owned {
		h.Client.CloseIdleConnections()
	}
}

// StatusError retains only an HTTP status and an allowlisted classification for concrete driver translation.
type StatusError struct {
	Code           int
	classification error
}

// Error omits server prose, URLs, and response bodies.
func (e *StatusError) Error() string { return e.classification.Error() }

// Unwrap exposes only the stable library classification.
func (e *StatusError) Unwrap() error { return e.classification }
