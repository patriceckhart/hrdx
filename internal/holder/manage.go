package holder

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// ErrNoHolder reports that nothing is listening on the holder socket.
var ErrNoHolder = errors.New("no session holder is running")

// ErrHolderBusy reports that a holder accepted the connection but did not
// complete the handshake in time. The holder serves one client at a time, so
// this normally means an attached hrdx terminal holds its client slot.
var ErrHolderBusy = errors.New("session holder is busy")

// Manager is a short-lived control connection used by the hrdx CLI to inspect
// and stop a running holder without starting the TUI. Unlike the TUI's long
// lived client, every call is bounded by a deadline so a holder that is busy
// with an attached terminal fails fast instead of hanging.
type Manager struct {
	client  *Client
	timeout time.Duration
	legacy  bool // connected to a holder speaking an incompatible protocol
}

// DialManager connects to the holder on socket. A socket that is missing or
// refuses the connection returns ErrNoHolder; a socket that accepts but never
// completes the handshake returns ErrHolderBusy. A holder speaking an
// incompatible protocol is still returned so it can be stopped, since the
// shutdown op needs no handshake.
func DialManager(socket string, timeout time.Duration) (*Manager, error) {
	conn, err := net.DialTimeout("unix", socket, timeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("%w: %v", ErrNoHolder, err)
		}
		return nil, fmt.Errorf("dial session holder: %w", err)
	}
	client := newClient(conn)
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := client.call(request{Op: "hello", Protocol: Protocol}); err != nil {
		if strings.Contains(err.Error(), "protocol mismatch") {
			return &Manager{client: client, timeout: timeout, legacy: true}, nil
		}
		client.Close()
		return nil, fmt.Errorf("%w: %v", ErrHolderBusy, err)
	}
	return &Manager{client: client, timeout: timeout}, nil
}

// Sessions lists the holder's sessions. A holder speaking an incompatible
// protocol cannot be listed reliably, so it reports an error instead.
func (m *Manager) Sessions() ([]SessionInfo, error) {
	if m.legacy {
		return nil, fmt.Errorf("holder speaks a different hrdx protocol")
	}
	_ = m.client.conn.SetDeadline(time.Now().Add(m.timeout))
	return m.client.List()
}

// Stop asks the holder to kill every session and exit.
func (m *Manager) Stop() error {
	_ = m.client.conn.SetDeadline(time.Now().Add(m.timeout))
	_, err := m.client.call(request{Op: "shutdown"})
	return err
}

// Close releases the management connection without stopping the holder.
func (m *Manager) Close() {
	m.client.Close()
}
