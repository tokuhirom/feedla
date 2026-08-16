package crawler

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.5", true},
		{"172.16.0.5", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // cloud metadata endpoint
		{"0.0.0.0", true},
		{"0.0.0.1", true},         // rest of 0.0.0.0/8 ("this network"), not just 0.0.0.0 itself
		{"0.255.255.255", true},   // 0.0.0.0/8 range end
		{"1.0.0.0", false},        // just above 0.0.0.0/8
		{"224.0.0.1", true},       // multicast
		{"100.64.0.0", true},      // CGNAT range start
		{"100.100.100.200", true}, // Alibaba Cloud metadata endpoint (CGNAT)
		{"100.127.255.255", true}, // CGNAT range end
		{"100.63.255.255", false}, // just below CGNAT range
		{"100.128.0.0", false},    // just above CGNAT range
		{"8.8.8.8", false},
		{"93.184.216.34", false},
		{"2606:4700:4700::1111", false},

		// IPv4-mapped IPv6 (::ffff:a.b.c.d) is unwrapped by net.IP.To4(),
		// so the plain IPv4 checks above already apply transparently.
		{"::ffff:192.168.1.1", true},
		{"::ffff:100.100.100.200", true}, // CGNAT via IPv4-mapped

		// Encodings net.IP.To4() does NOT unwrap: embeddedIPv4 must catch
		// these explicitly, recursing into the same IPv4 checks.
		{"64:ff9b::c0a8:101", true},  // NAT64: embeds 192.168.1.1
		{"64:ff9b::808:808", false},  // NAT64: embeds 8.8.8.8 (public, allowed)
		{"2002:c0a8:101::", true},    // 6to4: embeds 192.168.1.1
		{"2002:808:808::", false},    // 6to4: embeds 8.8.8.8 (public, allowed)
		{"::192.168.1.1", true},      // deprecated IPv4-compatible form
		{"::8.8.8.8", false},         // deprecated IPv4-compatible, public
		{"64:ff9c::c0a8:101", false}, // just outside the NAT64 prefix
		{"2003:c0a8:101::", false},   // just outside the 6to4 prefix
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("net.ParseIP(%q) failed", c.ip)
		}
		if got := isBlockedIP(ip); got != c.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}
