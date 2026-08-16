package pagewatch

import "testing"

func TestPreview_MaskedFlagReflectsIgnorePatterns(t *testing.T) {
	html := `<html><body>
<p>本文は変わりません。</p>
<p>Document ID: abc123</p>
</body></html>`
	cfg := Config{IgnorePatterns: []string{"Document ID: [A-Za-z0-9]+"}}

	blocks, err := Preview("https://example.com/diary/", []byte(html), cfg)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].Masked {
		t.Errorf("blocks[0] (%q) = masked, want unmasked", blocks[0].Text)
	}
	if !blocks[1].Masked {
		t.Errorf("blocks[1] (%q) = unmasked, want masked", blocks[1].Text)
	}
}

func TestPreview_NoConfigMasksNothing(t *testing.T) {
	html := `<html><body><p>ある記事の本文です。</p></body></html>`
	blocks, err := Preview("https://example.com/diary/", []byte(html), Config{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Masked {
		t.Fatalf("blocks = %+v, want one unmasked block", blocks)
	}
}

func TestPreview_InvalidIgnorePatternErrors(t *testing.T) {
	html := `<html><body><p>本文です。</p></body></html>`
	cfg := Config{IgnorePatterns: []string{"(unclosed"}}
	if _, err := Preview("https://example.com/diary/", []byte(html), cfg); err == nil {
		t.Fatal("Preview: want an error for an invalid regexp")
	}
}
