package crawler

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
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
		// A per-dial Timeout bounds a single stuck connection attempt; the
		// http.Client-level Timeout (see Fetcher) already bounds the whole
		// request but only fires after everything, including a hung dial,
		// finishes or times out together.
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
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
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		isCGNAT(ip) ||
		isThisNetwork(ip) {
		return true
	}
	// net.IP.To4() only unwraps the standard IPv4-mapped form
	// (::ffff:a.b.c.d). Other IPv6 encodings that embed an IPv4 address
	// (NAT64, 6to4, the deprecated ::a.b.c.d form) would otherwise sail
	// through the checks above even when the embedded address is private.
	if v4 := embeddedIPv4(ip); v4 != nil {
		return isBlockedIP(v4)
	}
	return false
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

// isThisNetwork reports whether ip falls in 0.0.0.0/8 (RFC 791 "this
// network"), reserved as a source-only broadcast range. ip.IsUnspecified()
// only catches the single address 0.0.0.0, leaving the rest of the /8
// (0.0.0.1-0.255.255.255) unblocked; some platforms treat any address in
// this range as "this host", so it gets the same treatment as loopback.
func isThisNetwork(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 0
}

var (
	// nat64Prefix is the NAT64/DNS64 "Well-Known Prefix" (RFC 6052) used to
	// synthesize AAAA records that embed an IPv4 address in the low 32 bits.
	nat64Prefix = &net.IPNet{IP: net.ParseIP("64:ff9b::"), Mask: net.CIDRMask(96, 128)}
	// sixToFourPrefix is the 6to4 (RFC 3056) range, which embeds an IPv4
	// address in bits 16-47.
	sixToFourPrefix = &net.IPNet{IP: net.ParseIP("2002::"), Mask: net.CIDRMask(16, 128)}
)

// embeddedIPv4 extracts the IPv4 address encoded in ip, for the IPv6
// encodings net.IP.To4() does not unwrap on its own: NAT64-synthesized
// addresses, 6to4, and the deprecated "IPv4-compatible" form (::a.b.c.d,
// RFC 4291, obsoleted by RFC 5156). Returns nil if ip doesn't match any of
// these schemes.
func embeddedIPv4(ip net.IP) net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip16.To4() != nil {
		return nil
	}
	switch {
	case nat64Prefix.Contains(ip16):
		return ip16[12:16]
	case sixToFourPrefix.Contains(ip16):
		return ip16[2:6]
	case isDeprecatedIPv4Compatible(ip16):
		return ip16[12:16]
	}
	return nil
}

// isDeprecatedIPv4Compatible reports whether ip16 (a 16-byte net.IP) is in
// the deprecated "IPv4-compatible IPv6 address" form ::a.b.c.d: the first 96
// bits are zero. ::1 and :: are caught by IsLoopback/IsUnspecified before
// this ever runs, so no special-casing is needed here.
func isDeprecatedIPv4Compatible(ip16 net.IP) bool {
	for _, b := range ip16[:12] {
		if b != 0 {
			return false
		}
	}
	return true
}
