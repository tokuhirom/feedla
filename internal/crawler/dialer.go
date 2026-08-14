package crawler

import (
	"context"
	"errors"
	"net"
	"syscall"
)

// errBlockedAddress is returned when a fetch target resolves to an address
// feedla refuses to connect to (see safeDialContext).
var errBlockedAddress = errors.New("crawler: blocked address (loopback/private/link-local)")

// safeDialContext wraps a net.Dialer with a Control hook that inspects the
// actual IP being connected to, not just the hostname. This defeats DNS
// rebinding: even if a feed URL's hostname resolves to a public IP at
// request-build time, Control runs against the address the runtime is about
// to dial, after Go's own resolution.
func safeDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || isBlockedIP(ip) {
				return errBlockedAddress
			}
			return nil
		},
	}
	return dialer.DialContext
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}
