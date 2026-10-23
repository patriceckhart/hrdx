//go:build !windows

package continuous

import (
	"context"
	"net"
)

// LocalSupported reports whether this platform can reach local hosts.
const LocalSupported = true

func dialLocal(ctx context.Context, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", address)
}
