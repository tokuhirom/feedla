package extract

import (
	"testing"
	"time"
)

func TestParseFlexibleDate(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{"rfc3339", "2026-08-18T09:30:00Z", time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC), true},
		{"bare date", "2026-08-18", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), true},
		{"japanese", "2026年8月18日", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), true},
		{"japanese with trailing text", "2026年8月18日 10:00更新", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), true},
		{"empty", "", time.Time{}, false},
		{"garbage", "not a date", time.Time{}, false},
		{"far future rejected", "2026-08-21T00:00:00Z", time.Time{}, false},
		{"just within future threshold", "2026-08-20T12:30:00Z", time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseFlexibleDate(c.in, now)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && !got.Equal(c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
