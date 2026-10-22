package geolocation

import (
	"context"
	"net"
	"time"
)

// netDialer applies Guzzle's connect_timeout option.
type netDialer struct{ timeout time.Duration }

func (d *netDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{Timeout: d.timeout, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
}
