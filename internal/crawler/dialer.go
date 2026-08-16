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
		ip.IsMulticast() ||
		isCGNAT(ip)
}

// isCGNAT reports whether ip falls in the shared address space
// 100.64.0.0/10 (RFC 6598), used for carrier-grade NAT. Some cloud
// providers (e.g. Alibaba Cloud's 100.100.100.200 metadata endpoint) and
// overlay networks (e.g. Tailscale) route internal services through this
// range, so it needs the same treatment as RFC 1918 private space.
func isCGNAT(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1]&0xc0 == 0x40 // 100.64.0.0 - 100.127.255.255
}
