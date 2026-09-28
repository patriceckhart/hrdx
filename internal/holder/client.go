package holder

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func unmarshal(data []byte, into any) error { return json.Unmarshal(data, into) }

// Client is the TUI side of the holder connection. It multiplexes
// control requests and per-session I/O over one socket.
type Client struct {
	conn net.Conn

	writeMu sync.Mutex // serializes frame writes

	mu       sync.Mutex
	nextReq  int64
	pending  map[int64]chan response
	outputs  map[int64]func([]byte) // session id -> output sink
	onExited func(int64)
	closed   bool
}

// Connect dials an existing holder socket and validates the protocol.
func Connect(socket string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, err
	}
	client := newClient(conn)
	if _, err := client.call(request{Op: "hello", Protocol: Protocol}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// newClient wraps an established connection with a running read loop. It is
// the shared constructor for TUI attaches and CLI management connections.
func newClient(conn net.Conn) *Client {
	client := &Client{
		conn:    conn,
		nextReq: 1,
		pending: map[int64]chan response{},
		outputs: map[int64]func([]byte){},
	}
	go client.readLoop()
	return client
}

// Spawn starts a holder process (the current binary with the holder
// flag) detached from the TUI's lifetime and waits for its socket.
func Spawn(socket string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "--holder", "--holder-socket", socket)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	// A fresh session detaches the holder from the TUI's controlling
	// terminal, so closing the terminal never signals it.
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap if it exits while we live

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("holder did not come up on %s", socket)
}

// ConnectOrSpawn attaches to a running holder or starts a new one. A
// stale socket file (nothing listening) is removed first; a holder
// speaking an incompatible protocol (left over from an old binary) is
// shut down and replaced, losing its sessions.
func ConnectOrSpawn(socket, version string) (*Client, error) {
	client, err := Connect(socket)
	if err == nil {
		return client, nil
	}
	if strings.Contains(err.Error(), "protocol mismatch") {
		// The shutdown op works without a successful hello.
		if old, dialErr := dialRaw(socket); dialErr == nil {
			old.Shutdown()
			old.Close()
		}
	}
	_ = os.Remove(socket)
	if err := Spawn(socket); err != nil {
		return nil, err
	}
	return Connect(socket)
}

// dialRaw connects without the hello handshake.
func dialRaw(socket string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return newClient(conn), nil
}

// readLoop dispatches incoming frames until the connection dies.
func (c *Client) readLoop() {
	for {
		f, err := readFrame(c.conn)
		if err != nil {
			c.mu.Lock()
			c.closed = true
			for _, waiter := range c.pending {
				close(waiter)
			}
			c.pending = map[int64]chan response{}
			c.mu.Unlock()
			return
		}
		switch f.Type {
		case TOut:
			c.mu.Lock()
			sink := c.outputs[f.ID]
			c.mu.Unlock()
			if sink != nil {
				sink(f.Payload)
			}
		case TResp:
			var resp response
			if unmarshal(f.Payload, &resp) != nil {
				continue
			}
			c.mu.Lock()
			waiter := c.pending[resp.Req]
			delete(c.pending, resp.Req)
			c.mu.Unlock()
			if waiter != nil {
				waiter <- resp
			}
		case TEvt:
			var evt event
			if unmarshal(f.Payload, &evt) != nil {
				continue
			}
			c.mu.Lock()
			handler := c.onExited
			c.mu.Unlock()
			if evt.Event == "exited" && handler != nil {
				// Never block the read loop on the handler: it may wait on
				// the UI loop, which in turn may wait on a holder response
				// only this loop can deliver (deadlock otherwise).
				go handler(evt.Session)
			}
		}
	}
}

// call sends a control request and waits for its response.
func (c *Client) call(req request) (response, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return response{}, fmt.Errorf("holder connection closed")
	}
	req.Req = c.nextReq
	c.nextReq++
	waiter := make(chan response, 1)
	c.pending[req.Req] = waiter
	c.mu.Unlock()

	c.writeMu.Lock()
	err := writeFrame(c.conn, frame{Type: TCtrl, Payload: marshal(req)})
	c.writeMu.Unlock()
	if err != nil {
		return response{}, err
	}

	select {
	case resp, ok := <-waiter:
		if !ok {
			return response{}, fmt.Errorf("holder connection closed")
		}
		if resp.Err != "" {
			return resp, fmt.Errorf("%s", resp.Err)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		c.mu.Lock()
		delete(c.pending, req.Req)
		c.mu.Unlock()
		return response{}, fmt.Errorf("holder did not answer")
	}
}

// SetExitHandler registers the callback for session-exited events.
func (c *Client) SetExitHandler(handler func(session int64)) {
	c.mu.Lock()
	c.onExited = handler
	c.mu.Unlock()
}

// Start launches a new session in the holder.
func (c *Client) Start(command string, args []string, cwd string, env []string, cols, rows int) (int64, error) {
	resp, err := c.call(request{
		Op: "start", Command: command, Args: args, CWD: cwd, Env: env, Cols: cols, Rows: rows,
	})
	if err != nil {
		return 0, err
	}
	return resp.Session, nil
}

// Attach subscribes to a session's output; buffered output is replayed
// through sink. Returns whether the session is still running.
func (c *Client) Attach(session int64, cols, rows int, sink func([]byte)) (bool, error) {
	c.mu.Lock()
	c.outputs[session] = sink
	c.mu.Unlock()
	resp, err := c.call(request{Op: "attach", Session: session, Cols: cols, Rows: rows})
	if err != nil {
		c.mu.Lock()
		delete(c.outputs, session)
		c.mu.Unlock()
		return false, err
	}
	return resp.Running, nil
}

// Write sends input bytes to a session's PTY.
func (c *Client) Write(session int64, data []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = writeFrame(c.conn, frame{Type: TIn, ID: session, Payload: data})
}

// Resize changes a session's PTY size.
func (c *Client) Resize(session int64, cols, rows int) {
	go func() { _, _ = c.call(request{Op: "resize", Session: session, Cols: cols, Rows: rows}) }()
}

// Kill terminates a session's process and forgets it.
func (c *Client) Kill(session int64) {
	go func() { _, _ = c.call(request{Op: "kill", Session: session}) }()
}

// Foreground returns the session's foreground process name.
func (c *Client) Foreground(session int64) string {
	resp, err := c.call(request{Op: "fg", Session: session})
	if err != nil {
		return ""
	}
	return resp.Name
}

// List returns the holder's sessions.
func (c *Client) List() ([]SessionInfo, error) {
	resp, err := c.call(request{Op: "list"})
	if err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

// Shutdown asks the holder to kill all sessions and exit.
func (c *Client) Shutdown() {
	_, _ = c.call(request{Op: "shutdown"})
}

// Close detaches from the holder, leaving sessions running.
func (c *Client) Close() {
	_ = c.conn.Close()
}
