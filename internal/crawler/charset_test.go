package crawler_test

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/tokuhirom/feedla/internal/crawler"
)

func encodeShiftJIS(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := transform.NewWriter(&buf, japanese.ShiftJIS.NewEncoder())
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("encode Shift_JIS: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("encode Shift_JIS: %v", err)
	}
	return buf.Bytes()
}

func TestDecodeUTF8_ContentTypeHeaderCharset(t *testing.T) {
	body := encodeShiftJIS(t, "<html><body>日本語のページです</body></html>")
	got, err := crawler.DecodeUTF8(body, "text/html; charset=Shift_JIS")
	if err != nil {
		t.Fatalf("DecodeUTF8: %v", err)
	}
	if !bytes.Contains(got, []byte("日本語のページです")) {
		t.Errorf("decoded body = %q, want it to contain the Japanese text", got)
	}
}

func TestDecodeUTF8_MetaHTTPEquivCharsetWithNoHeaderCharset(t *testing.T) {
	// The Content-Type header carries no charset param at all (only the
	// <meta http-equiv> in the body does) — the tabesugi.net MVP fixture's
	// exact situation (§14.2 of docs/feedless-site-subscription-pagewatch.md).
	html := `<html><head><meta http-equiv="Content-Type" content="text/html; charset=Shift_JIS"></head><body>日本語のページです</body></html>`
	body := encodeShiftJIS(t, html)
	got, err := crawler.DecodeUTF8(body, "text/html")
	if err != nil {
		t.Fatalf("DecodeUTF8: %v", err)
	}
	if !bytes.Contains(got, []byte("日本語のページです")) {
		t.Errorf("decoded body = %q, want it to contain the Japanese text", got)
	}
}

func TestDecodeUTF8_AlreadyUTF8IsUnchanged(t *testing.T) {
	html := []byte(`<html><body>すでに UTF-8 です</body></html>`)
	got, err := crawler.DecodeUTF8(html, "text/html; charset=utf-8")
	if err != nil {
		t.Fatalf("DecodeUTF8: %v", err)
	}
	if !bytes.Equal(got, html) {
		t.Errorf("decoded body = %q, want it unchanged", got)
	}
}
