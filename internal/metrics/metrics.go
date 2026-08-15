// Package metrics collects feed-fetch counters/histograms exposed via
// /metrics (see docs/DESIGN.md's "観測" section: fetch_total{status},
// fetch_duration_seconds). Gauges backed directly by the store
// (feeds_total, entries_unread, ...) are rendered by internal/api, which
// queries the store live on every scrape instead of caching state here.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// fetchDurationBuckets are the histogram's `le` boundaries, in seconds.
var fetchDurationBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// Metrics accumulates process-lifetime counters for feed fetches. The zero
// value is not usable; construct with New. Safe for concurrent use.
type Metrics struct {
	mu sync.Mutex

	fetchTotal map[string]int64 // status -> count

	durationBucketCounts []int64 // cumulative, parallel to fetchDurationBuckets
	durationSum          float64
	durationCount        int64
}

// New returns a ready-to-use Metrics.
func New() *Metrics {
	return &Metrics{
		fetchTotal:           make(map[string]int64),
		durationBucketCounts: make([]int64, len(fetchDurationBuckets)),
	}
}

// ObserveFetch records the outcome of one feed fetch attempt. status is a
// short label such as "ok", "not_modified", "gone", or "error".
func (m *Metrics) ObserveFetch(status string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fetchTotal[status]++

	sec := d.Seconds()
	for i, le := range fetchDurationBuckets {
		if sec <= le {
			m.durationBucketCounts[i]++
		}
	}
	m.durationSum += sec
	m.durationCount++
}

// WriteFetchMetrics renders fetch_total and fetch_duration_seconds in
// Prometheus text exposition format.
func (m *Metrics) WriteFetchMetrics(w io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	statuses := make([]string, 0, len(m.fetchTotal))
	for status := range m.fetchTotal {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)

	if _, err := fmt.Fprintf(w, "# HELP fetch_total Total feed fetch attempts by outcome.\n# TYPE fetch_total counter\n"); err != nil {
		return err
	}
	for _, status := range statuses {
		if _, err := fmt.Fprintf(w, "fetch_total{status=%q} %d\n", status, m.fetchTotal[status]); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "# HELP fetch_duration_seconds Feed fetch+parse+write duration in seconds.\n# TYPE fetch_duration_seconds histogram\n"); err != nil {
		return err
	}
	for i, le := range fetchDurationBuckets {
		if _, err := fmt.Fprintf(w, "fetch_duration_seconds_bucket{le=%q} %d\n", formatFloat(le), m.durationBucketCounts[i]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "fetch_duration_seconds_bucket{le=\"+Inf\"} %d\n", m.durationCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "fetch_duration_seconds_sum %s\n", formatFloat(m.durationSum)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "fetch_duration_seconds_count %d\n", m.durationCount); err != nil {
		return err
	}
	return nil
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}
