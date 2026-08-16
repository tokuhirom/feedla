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
		{"224.0.0.1", true},       // multicast
		{"100.64.0.0", true},      // CGNAT range start
		{"100.100.100.200", true}, // Alibaba Cloud metadata endpoint (CGNAT)
		{"100.127.255.255", true}, // CGNAT range end
		{"100.63.255.255", false}, // just below CGNAT range
		{"100.128.0.0", false},    // just above CGNAT range
		{"8.8.8.8", false},
		{"93.184.216.34", false},
		{"2606:4700:4700::1111", false},
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
