package continuous

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Pool caches one connection per host profile for background commands. It
// is safe for concurrent use. A closed connection is redialed on the next
// Get; callers decide whether a failed operation may be retried.
type Pool struct {
	mu     sync.Mutex
	conns  map[string]*Client
	dial   Dialer // test override; nil uses the profile's transport
	custom bool
}

// NewPool returns a pool using dial for every profile, or each profile's
// own transport (local endpoint or TLS) when dial is nil.
func NewPool(dial Dialer) *Pool {
	return &Pool{conns: map[string]*Client{}, dial: dial, custom: dial != nil}
}

// DialerFor returns the transport for a profile: the platform local
// endpoint, plaintext loopback TCP, or TLS 1.3 with the profile's CA.
func DialerFor(profile Profile) (Dialer, error) {
	if !IsRemoteAddress(profile.Address) {
		return dialLocal, nil
	}
	if profile.TLSCA == "" {
		if !IsLoopbackAddress(profile.Address) {
			return nil, fmt.Errorf("profile %s: remote hosts require tls_ca", profile.ID)
		}
		return func(ctx context.Context, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", address)
		}, nil
	}
	pem, err := os.ReadFile(profile.TLSCA)
	if err != nil {
		return nil, fmt.Errorf("profile %s: read tls_ca: %w", profile.ID, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("profile %s: no certificates in tls_ca", profile.ID)
	}
	config := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}
	return func(ctx context.Context, address string) (net.Conn, error) {
		dialer := &tls.Dialer{Config: config}
		return dialer.DialContext(ctx, "tcp", address)
	}, nil
}

// IsLoopbackAddress reports a host:port whose host is a loopback name.
func IsLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Get returns a live host connection for profile.
func (p *Pool) Get(ctx context.Context, profile Profile) (Host, error) {
	p.mu.Lock()
	client := p.conns[profile.ID]
	p.mu.Unlock()
	if client != nil {
		select {
		case <-client.Done():
		default:
			return Host{client}, nil
		}
	}
	token, err := profile.Token()
	if err != nil {
		return Host{}, err
	}
	dial := p.dial
	if !p.custom {
		if dial, err = DialerFor(profile); err != nil {
			return Host{}, err
		}
	}
	fresh, err := DialWith(ctx, dial, profile.Address, token)
	if err != nil {
		return Host{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if current := p.conns[profile.ID]; current != nil && current != client {
		select {
		case <-current.Done():
		default:
			// Another command connected first; keep one connection.
			fresh.Close()
			return Host{current}, nil
		}
	}
	p.conns[profile.ID] = fresh
	return Host{fresh}, nil
}

// Drop closes and forgets a profile's connection, for example after an
// explicit disconnect or host stop.
func (p *Pool) Drop(id string) {
	p.mu.Lock()
	client := p.conns[id]
	delete(p.conns, id)
	p.mu.Unlock()
	if client != nil {
		client.Close()
	}
}

// Close closes every connection. Host work is never cancelled by closing.
func (p *Pool) Close() {
	p.mu.Lock()
	conns := p.conns
	p.conns = map[string]*Client{}
	p.mu.Unlock()
	for _, client := range conns {
		client.Close()
	}
}

// IsRemoteAddress reports a host:port address rather than a local endpoint.
func IsRemoteAddress(address string) bool {
	// A rooted path ("/x" or "\x") is local on every platform; Windows
	// does not treat it as absolute without a drive letter.
	if filepath.IsAbs(address) || strings.HasPrefix(address, "/") || strings.HasPrefix(address, `\`) ||
		strings.HasPrefix(address, ".") {
		return false
	}
	return strings.Contains(address, ":")
}
