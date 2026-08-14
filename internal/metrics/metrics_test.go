package metrics_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/metrics"
)

func TestWriteFetchMetrics(t *testing.T) {
	m := metrics.New()
	m.ObserveFetch("ok", 200*time.Millisecond)
	m.ObserveFetch("ok", 2*time.Second)
	m.ObserveFetch("error", 50*time.Millisecond)

	var buf bytes.Buffer
	if err := m.WriteFetchMetrics(&buf); err != nil {
		t.Fatalf("WriteFetchMetrics: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`fetch_total{status="ok"} 2`,
		`fetch_total{status="error"} 1`,
		`fetch_duration_seconds_bucket{le="0.1"} 1`,  // only the 50ms observation
		`fetch_duration_seconds_bucket{le="0.25"} 2`, // + the 200ms observation
		`fetch_duration_seconds_bucket{le="+Inf"} 3`,
		`fetch_duration_seconds_count 3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestWriteFetchMetricsEmpty(t *testing.T) {
	m := metrics.New()
	var buf bytes.Buffer
	if err := m.WriteFetchMetrics(&buf); err != nil {
		t.Fatalf("WriteFetchMetrics: %v", err)
	}
	if !strings.Contains(buf.String(), `fetch_duration_seconds_count 0`) {
		t.Fatalf("expected zeroed histogram output, got:\n%s", buf.String())
	}
}
