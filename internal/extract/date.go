package extract

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var jpDateRe = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日`)

// ParseFlexibleDate parses s as RFC3339, then the bare "2006-01-02" date
// form, then the Japanese "2006年1月2日" form (only the leading match is
// used; trailing text such as a time-of-day is ignored). A parsed date more
// than 1 hour in the future relative to now is rejected (a likely
// scheduled-post placeholder) — the same threshold crawler.normalizeItem
// uses for feed-supplied dates, so extractors don't need a second opinion.
//
// Shared by internal/extract/selector (date_selector, list-page dates) and
// internal/fulltext (ExtractPublished, individual article pages).
func ParseFlexibleDate(s string, now time.Time) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return checkNotFuture(t, now)
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return checkNotFuture(t, now)
	}
	if m := jpDateRe.FindStringSubmatch(s); m != nil {
		year, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		return checkNotFuture(t, now)
	}
	return time.Time{}, false
}

func checkNotFuture(t, now time.Time) (time.Time, bool) {
	if t.After(now.Add(time.Hour)) {
		return time.Time{}, false
	}
	return t, true
}
