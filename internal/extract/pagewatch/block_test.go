package pagewatch

import "testing"

func TestSplitBlocks_LeafOnly(t *testing.T) {
	raw := `<html><body><div class="outer"><p>最初の段落です。</p><p>二番目の段落です。</p></div></body></html>`
	body := prepareBody(t, raw, "https://example.com/")
	blocks := splitBlocks(body, nil)
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2 (the wrapping div must not itself become a block)", len(blocks))
	}
}

func TestSplitBlocks_AnchorAndHead(t *testing.T) {
	raw := `<html><body>
<h2><a id="entry-1">最初の見出し</a></h2>
<p>見出し1の本文です。</p>
<h2><a id="entry-2">二番目の見出し</a></h2>
<p>見出し2の本文です。</p>
</body></html>`
	body, anchors := prepareBodyWithAnchors(t, raw, "https://example.com/")
	blocks := splitBlocks(body, anchors)
	if len(blocks) != 4 {
		t.Fatalf("len(blocks) = %d, want 4", len(blocks))
	}
	if blocks[1].Anchor != "entry-1" || blocks[1].Head != "最初の見出し" {
		t.Errorf("blocks[1] anchor/head = %q/%q, want entry-1/最初の見出し", blocks[1].Anchor, blocks[1].Head)
	}
	if blocks[3].Anchor != "entry-2" || blocks[3].Head != "二番目の見出し" {
		t.Errorf("blocks[3] anchor/head = %q/%q, want entry-2/二番目の見出し", blocks[3].Anchor, blocks[3].Head)
	}
}

func TestSplitBlocks_ImageOnlyKeptTextOnlyWhitespaceDropped(t *testing.T) {
	raw := `<html><body>
<p>   </p>
<p><img src="/a.png" alt="a"></p>
</body></html>`
	body := prepareBody(t, raw, "https://example.com/")
	blocks := splitBlocks(body, nil)
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1 (whitespace-only <p> must be dropped, image-only <p> kept)", len(blocks))
	}
	if blocks[0].Text != "" {
		t.Errorf("blocks[0].Text = %q, want empty (image-only block)", blocks[0].Text)
	}
}

func TestNormalizeText_WhitespaceAndFullwidth(t *testing.T) {
	got := normalizeText("  全角　スペース\t と\n改行  ")
	want := "全角 スペース と 改行"
	if got != want {
		t.Errorf("normalizeText = %q, want %q", got, want)
	}
}
