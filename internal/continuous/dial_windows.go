//go:build windows

package continuous

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// LocalSupported reports whether this platform can reach local hosts. On
// native Windows zot continuous serves a private named pipe derived from
// the socket path; this client derives the same name.
const LocalSupported = true

// LocalEndpointName mirrors zot's derivation of the pipe name for a store
// socket path, so a profile address works unchanged on every platform.
func LocalEndpointName(path string) string {
	if strings.HasPrefix(path, `\\.\pipe\`) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	h := fnv.New64a()
	h.Write([]byte(strings.ToLower(abs)))
	base := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, filepath.Base(abs))
	return fmt.Sprintf(`\\.\pipe\zot-continuous-%x-%s`, h.Sum64(), base)
}

func dialLocal(ctx context.Context, address string) (net.Conn, error) {
	name := LocalEndpointName(address)
	name16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	for {
		handle, err := windows.CreateFile(name16, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			return newPipeConn(handle, name), nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, fmt.Errorf("connect to pipe: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// pipeConn is a client pipe handle with overlapped I/O, so reads and
// writes run concurrently and Close cancels them.
type pipeConn struct {
	handle   windows.Handle
	name     string
	readEv   windows.Handle
	writeEv  windows.Handle
	readMu   sync.Mutex
	writeMu  sync.Mutex
	closeMu  sync.Mutex
	closed   bool
	deadline struct {
		mu    sync.Mutex
		read  time.Time
		write time.Time
	}
}

func newPipeConn(handle windows.Handle, name string) *pipeConn {
	readEv, _ := windows.CreateEvent(nil, 1, 0, nil)
	writeEv, _ := windows.CreateEvent(nil, 1, 0, nil)
	return &pipeConn{handle: handle, name: name, readEv: readEv, writeEv: writeEv}
}

func (c *pipeConn) wait(ov *windows.Overlapped, ev windows.Handle, deadline time.Time) error {
	finished := make(chan error, 1)
	go func() {
		_, err := windows.WaitForSingleObject(ev, windows.INFINITE)
		finished <- err
	}()
	var timer <-chan time.Time
	if !deadline.IsZero() {
		timer = time.After(time.Until(deadline))
	}
	select {
	case err := <-finished:
		return err
	case <-timer:
		windows.CancelIoEx(c.handle, ov)
		<-finished
		return os.ErrDeadlineExceeded
	}
}

func (c *pipeConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return 0, net.ErrClosed
	}
	if len(p) == 0 {
		c.closeMu.Unlock()
		return 0, nil
	}
	windows.ResetEvent(c.readEv)
	ov := &windows.Overlapped{HEvent: c.readEv}
	var pinned runtime.Pinner
	pinned.Pin(ov)
	pinned.Pin(&p[0])
	defer pinned.Unpin()
	var n uint32
	err := windows.ReadFile(c.handle, p, &n, ov)
	c.closeMu.Unlock()
	if err != nil && errors.Is(err, windows.ERROR_IO_PENDING) {
		c.deadline.mu.Lock()
		deadline := c.deadline.read
		c.deadline.mu.Unlock()
		err = c.wait(ov, c.readEv, deadline)
	}
	if err == nil {
		err = windows.GetOverlappedResult(c.handle, ov, &n, false)
	}
	switch {
	case err == nil:
		if n == 0 {
			return 0, io.EOF
		}
		return int(n), nil
	case errors.Is(err, windows.ERROR_BROKEN_PIPE), errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED), errors.Is(err, windows.ERROR_NO_DATA):
		return int(n), io.EOF
	case errors.Is(err, windows.ERROR_OPERATION_ABORTED):
		return int(n), net.ErrClosed
	}
	return int(n), err
}

func (c *pipeConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var pinned runtime.Pinner
	defer pinned.Unpin()
	if len(p) > 0 {
		pinned.Pin(&p[0])
	}
	total := 0
	for total < len(p) {
		windows.ResetEvent(c.writeEv)
		ov := &windows.Overlapped{HEvent: c.writeEv}
		pinned.Pin(ov)
		var n uint32
		c.closeMu.Lock()
		if c.closed {
			c.closeMu.Unlock()
			return total, net.ErrClosed
		}
		err := windows.WriteFile(c.handle, p[total:], &n, ov)
		c.closeMu.Unlock()
		if err != nil && errors.Is(err, windows.ERROR_IO_PENDING) {
			c.deadline.mu.Lock()
			deadline := c.deadline.write
			c.deadline.mu.Unlock()
			err = c.wait(ov, c.writeEv, deadline)
		}
		if err == nil {
			err = windows.GetOverlappedResult(c.handle, ov, &n, false)
		}
		total += int(n)
		if err != nil {
			if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA) {
				return total, io.ErrClosedPipe
			}
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (c *pipeConn) Close() error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closed = true
	windows.CancelIoEx(c.handle, nil)
	c.closeMu.Unlock()
	// Keep the handle and events valid until cancelled operations release
	// their pinned memory.
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	err := windows.CloseHandle(c.handle)
	windows.CloseHandle(c.readEv)
	windows.CloseHandle(c.writeEv)
	return err
}

type pipeAddr string

func (pipeAddr) Network() string  { return "pipe" }
func (a pipeAddr) String() string { return string(a) }

func (c *pipeConn) LocalAddr() net.Addr  { return pipeAddr(c.name) }
func (c *pipeConn) RemoteAddr() net.Addr { return pipeAddr(c.name) }

func (c *pipeConn) SetDeadline(t time.Time) error {
	c.deadline.mu.Lock()
	c.deadline.read, c.deadline.write = t, t
	c.deadline.mu.Unlock()
	return nil
}

func (c *pipeConn) SetReadDeadline(t time.Time) error {
	c.deadline.mu.Lock()
	c.deadline.read = t
	c.deadline.mu.Unlock()
	return nil
}

func (c *pipeConn) SetWriteDeadline(t time.Time) error {
	c.deadline.mu.Lock()
	c.deadline.write = t
	c.deadline.mu.Unlock()
	return nil
}
