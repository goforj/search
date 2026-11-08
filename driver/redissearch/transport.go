package redissearch

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/goforj/search"
)

// maxResponseBytes bounds all aggregate RESP allocations before backend data reaches application memory.
const maxResponseBytes = 32 << 20

// nativeError is inspected only inside the transport and never crosses the driver boundary.
type nativeError string

// Error retains native prose only long enough to classify a stable allowlisted failure.
func (e nativeError) Error() string { return string(e) }

// reader owns one bounded RESP2 response, including nested aggregate allocations.
type reader struct {
	input     *bufio.Reader
	remaining int
}

// value rejects unbounded lengths, malformed frames, and excessive nesting before allocating.
func (r *reader) value(depth int) (any, error) {
	if depth > 16 {
		return nil, search.ErrUnavailable
	}
	line, err := r.input.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	r.remaining -= len(line)
	if r.remaining < 0 || len(line) < 3 || line[len(line)-2] != '\r' {
		return nil, search.ErrUnavailable
	}
	content := string(line[1 : len(line)-2])
	switch line[0] {
	case '+':
		return content, nil
	case '-':
		return nil, nativeError(content)
	case ':':
		return strconv.ParseInt(content, 10, 64)
	case '$', '*':
		count, err := strconv.Atoi(content)
		if err != nil || count < -1 {
			return nil, search.ErrUnavailable
		}
		if count == -1 {
			return nil, nil
		}
		if count > r.remaining {
			return nil, search.ErrUnavailable
		}
		if line[0] == '$' {
			r.remaining -= count + 2
			if r.remaining < 0 {
				return nil, search.ErrUnavailable
			}
			data := make([]byte, count+2)
			if _, err := io.ReadFull(r.input, data); err != nil {
				return nil, err
			}
			if data[count] != '\r' || data[count+1] != '\n' {
				return nil, search.ErrUnavailable
			}
			return string(data[:count]), nil
		}
		if count > 1000000 {
			return nil, search.ErrUnavailable
		}
		values := make([]any, count)
		for n := range values {
			value, err := r.value(depth + 1)
			if err != nil {
				return nil, err
			}
			values[n] = value
		}
		return values, nil
	default:
		return nil, search.ErrUnavailable
	}
}

// exchange writes one complete RESP command and consumes exactly one bounded response.
func exchange(connection net.Conn, args []string) (any, error) {
	var command bytes.Buffer
	fmt.Fprintf(&command, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&command, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if command.Len() > maxResponseBytes {
		return nil, search.ErrUnsupported
	}
	if _, err := io.Copy(connection, &command); err != nil {
		return nil, err
	}
	reader := reader{input: bufio.NewReaderSize(connection, 64<<10), remaining: maxResponseBytes}
	return reader.value(0)
}

// command owns a short-lived connection so cancellation and shutdown cannot corrupt another caller's protocol stream.
func (d *Driver) command(ctx context.Context, mutation bool, args ...string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()
	dialer := net.Dialer{Timeout: d.config.Timeout}
	var connection net.Conn
	var err error
	if d.config.TLSConfig != nil {
		tlsDialer := tls.Dialer{NetDialer: &dialer, Config: d.config.TLSConfig}
		connection, err = tlsDialer.DialContext(ctx, "tcp", d.config.Addr)
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", d.config.Addr)
	}
	if err != nil {
		return nil, transportError(ctx, err, false)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, transportError(ctx, err, false)
	}
	if d.config.Password != "" {
		auth := []string{"AUTH", d.config.Password}
		if d.config.Username != "" {
			auth = []string{"AUTH", d.config.Username, d.config.Password}
		}
		if _, err := exchange(connection, auth); err != nil {
			return nil, transportError(ctx, err, false)
		}
	}
	if d.config.DB != 0 {
		if _, err := exchange(connection, []string{"SELECT", strconv.Itoa(d.config.DB)}); err != nil {
			return nil, transportError(ctx, err, false)
		}
	}
	value, err := exchange(connection, args)
	if err != nil {
		return nil, transportError(ctx, err, mutation)
	}
	return value, nil
}

// transportError strips native prose, credentials, endpoints, and commands from every public error path.
func transportError(ctx context.Context, err error, mutation bool) error {
	var native nativeError
	if errors.As(err, &native) {
		message := strings.ToLower(string(native))
		switch {
		case strings.Contains(message, "wrongpass"), strings.Contains(message, "noauth"), strings.Contains(message, "noperm"):
			if mutation {
				return errors.Join(search.ErrUnauthorized, search.ErrOutcomeUnknown)
			}
			return search.ErrUnauthorized
		case strings.Contains(message, "unknown index"), strings.Contains(message, "index not found"), strings.Contains(message, "search_index_not_found"), strings.Contains(message, "index_missing"):
			return search.ErrIndexNotFound
		case strings.Contains(message, "index already exists"):
			return search.ErrConflict
		case strings.Contains(message, "unknown command"):
			return search.ErrUnsupported
		case strings.Contains(message, "timeout"):
			if mutation {
				return search.ErrOutcomeUnknown
			}
			return search.ErrUnavailable
		default:
			if mutation {
				return search.ErrOutcomeUnknown
			}
			return search.ErrUnavailable
		}
	}
	if errors.Is(err, search.ErrUnsupported) {
		return err
	}
	if mutation {
		if ctx.Err() != nil {
			return errors.Join(search.ErrOutcomeUnknown, ctx.Err())
		}
		return search.ErrOutcomeUnknown
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return search.ErrUnavailable
}

// pairs rejects malformed RESP field dictionaries instead of silently accepting missing protocol evidence.
func pairs(value any) (map[string]any, error) {
	values, ok := value.([]any)
	if !ok || len(values)%2 != 0 {
		return nil, search.ErrUnavailable
	}
	result := make(map[string]any, len(values)/2)
	for n := 0; n < len(values); n += 2 {
		key, ok := values[n].(string)
		if !ok {
			return nil, search.ErrUnavailable
		}
		if _, exists := result[key]; exists {
			return nil, search.ErrUnavailable
		}
		result[key] = values[n+1]
	}
	return result, nil
}

// timeoutDefault keeps client deadlines below the explicitly requested native search timeout.
const timeoutDefault = 10 * time.Second
