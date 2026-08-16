package crawler

import (
	"bytes"
	"fmt"
	"io"

	"golang.org/x/net/html/charset"
)

// DecodeUTF8 converts body to UTF-8 using contentType's charset parameter,
// falling back to <meta charset>/<meta http-equiv> sniffing and then
// BOM/content-based detection (see golang.org/x/net/html/charset).
//
// Feed parsing doesn't need this — gofeed/goxpp decode XML charsets
// internally — but HTML scraping (pagewatch) does its own parsing, so a
// misdecoded page must not be allowed to masquerade as a content diff (see
// docs/feedless-site-subscription-pagewatch.md §7.2).
func DecodeUTF8(body []byte, contentType string) ([]byte, error) {
	r, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, fmt.Errorf("crawler: detect charset: %w", err)
	}
	decoded, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("crawler: decode charset: %w", err)
	}
	return decoded, nil
}
