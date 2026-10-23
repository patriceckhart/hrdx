// Package continuous is hrdx's client for zot continuous hosts: the
// newline-delimited JSON host protocol, typed host state, and managed local
// host processes. It never touches UI state; callers run it off the Bubble
// Tea loop and deliver results as messages.
package continuous

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// maxFrame bounds one response line so a misbehaving host cannot grow
// client memory without limit.
const maxFrame = 8 << 20

// ErrUnsupported reports a capability the current platform or host lacks.
var ErrUnsupported = errors.New("unsupported")

// Error is a protocol-level failure returned by the host.
type Error struct {
	Method string
	Code   string
	Msg    string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("%s: %s", e.Method, e.Msg)
	}
	return fmt.Sprintf("%s: %s (%s)", e.Method, e.Msg, e.Code)
}

// Code returns the host error code of err, or "" when err is not a host error.
func Code(err error) string {
	var hostErr *Error
	if errors.As(err, &hostErr) {
		return hostErr.Code
	}
	return ""
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type response struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
	Code    string          `json:"code"`
}

// Client is one connection to a host. Calls are safe for concurrent use and
// matched to responses by ID. Closing the client never cancels host work.
type Client struct {
	conn   net.Conn
	mu     sync.Mutex
	next   int
	calls  map[string]chan response
	err    error
	closed chan struct{}
}

// Dialer opens a raw connection to a host address. Tests replace it.
type Dialer func(ctx context.Context, address string) (net.Conn, error)

// DialWith is Dial with an explicit dialer.
func DialWith(ctx context.Context, dial Dialer, address, token string) (*Client, error) {
	conn, err := dial(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("connect to continuous host: %w", err)
	}
	client := NewClient(conn)
	if token != "" {
		if _, err := client.Call(ctx, "hello", map[string]string{"token": token}); err != nil {
			client.Close()
			return nil, err
		}
	}
	return client, nil
}

// NewClient wraps an established connection and starts its read loop.
func NewClient(conn net.Conn) *Client {
	client := &Client{conn: conn, calls: map[string]chan response{}, closed: make(chan struct{})}
	go client.readLoop()
	return client
}

// Close ends the connection. Pending calls fail.
func (c *Client) Close() error { return c.conn.Close() }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.closed }

func (c *Client) readLoop() {
	defer func() {
		c.mu.Lock()
		if c.err == nil {
			c.err = io.EOF
		}
		for id, waiter := range c.calls {
			close(waiter)
			delete(c.calls, id)
		}
		c.mu.Unlock()
		close(c.closed)
	}()
	reader := bufio.NewReaderSize(c.conn, 64<<10)
	for {
		line, err := readLine(reader)
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			return
		}
		var frame response
		if json.Unmarshal(line, &frame) != nil || frame.Type != "response" {
			// Watch frames and malformed lines are not used by this client.
			continue
		}
		c.mu.Lock()
		if waiter, ok := c.calls[frame.ID]; ok {
			waiter <- frame
			delete(c.calls, frame.ID)
		}
		c.mu.Unlock()
	}
}

// readLine reads one newline-terminated frame, refusing oversized frames.
func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > maxFrame {
			return nil, fmt.Errorf("host frame exceeds %d bytes", maxFrame)
		}
		if !isPrefix {
			return line, nil
		}
	}
}

// Call performs one request and returns its raw data. A cancelled context
// abandons the wait without cancelling the host operation, whose outcome is
// then unknown to the caller.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.next++
	id := fmt.Sprintf("h%d", c.next)
	waiter := make(chan response, 1)
	c.calls[id] = waiter
	c.mu.Unlock()

	payload, err := json.Marshal(request{ID: id, Method: method, Params: params})
	if err != nil {
		c.forget(id)
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = c.conn.SetWriteDeadline(deadline)
	if _, err := c.conn.Write(append(payload, '\n')); err != nil {
		c.forget(id)
		return nil, err
	}
	select {
	case frame, ok := <-waiter:
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		if !frame.Success {
			return nil, &Error{Method: method, Code: frame.Code, Msg: frame.Error}
		}
		return frame.Data, nil
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	}
}

// CallInto performs one request and decodes its data into out.
func (c *Client) CallInto(ctx context.Context, method string, params, out any) error {
	data, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	return nil
}

func (c *Client) forget(id string) {
	c.mu.Lock()
	delete(c.calls, id)
	c.mu.Unlock()
}
