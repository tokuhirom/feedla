package inspect

import (
	"strconv"
	"strings"
	"testing"
)

func mustSanitize(t *testing.T, input string) (string, []Element) {
	t.Helper()
	out, elements := Sanitize([]byte(input))
	if out == nil {
		t.Fatalf("Sanitize(%q) returned nil output", input)
	}
	return string(out), elements
}

func TestSanitizeKeepsAllowedTagsAndDropsOthers(t *testing.T) {
	out, elements := mustSanitize(t, `<html><body>
		<article class="post"><h2>Title</h2><p>Body text</p></article>
		<script>alert(1)</script>
		<iframe src="https://evil.example/"></iframe>
		<svg onload="alert(2)"></svg>
	</body></html>`)

	if !strings.Contains(out, "<article") || !strings.Contains(out, "<h2") || !strings.Contains(out, "<p") {
		t.Fatalf("expected allowed tags to survive, got: %s", out)
	}
	if strings.Contains(out, "alert(1)") || strings.Contains(out, "<iframe") || strings.Contains(out, "<svg") || strings.Contains(out, "onload") {
		t.Fatalf("expected disallowed elements to be dropped entirely, got: %s", out)
	}

	// article, h2, p should all be indexed; script/iframe/svg must not be.
	if len(elements) != 3 {
		t.Fatalf("expected 3 indexed elements, got %d: %+v", len(elements), elements)
	}
	if elements[0].Tag != "article" || elements[0].Classes[0] != "post" {
		t.Fatalf("unexpected first element: %+v", elements[0])
	}
}

func TestSanitizeDropsDisallowedAttributesButKeepsAllowList(t *testing.T) {
	out, elements := mustSanitize(t, `<html><body>
		<div id="main" class="wrap" onclick="evil()" data-feedla-id="999" style="color:red">
			<a href="https://evil.example/" onmouseover="evil()">link text</a>
		</div>
	</body></html>`)

	if strings.Contains(out, "onclick") || strings.Contains(out, "onmouseover") || strings.Contains(out, "evil()") {
		t.Fatalf("expected event handler attributes to be stripped, got: %s", out)
	}
	if strings.Contains(out, "href") {
		t.Fatalf("expected href to be stripped, got: %s", out)
	}
	if !strings.Contains(out, `id="main"`) || !strings.Contains(out, `class="wrap"`) || !strings.Contains(out, `style="color:red"`) {
		t.Fatalf("expected allow-listed attributes to survive, got: %s", out)
	}

	// The spoofed data-feedla-id="999" on the source <div> must not survive:
	// Sanitize assigns its own sequential id instead.
	if elements[0].ID == 999 {
		t.Fatalf("spoofed data-feedla-id was trusted from input: %+v", elements[0])
	}
	if !strings.Contains(out, `data-feedla-id="`+strconv.Itoa(elements[0].ID)+`"`) {
		t.Fatalf("expected element to carry its assigned data-feedla-id, got: %s", out)
	}
}

func TestSanitizeStripsStyleURLsAndImports(t *testing.T) {
	out, _ := mustSanitize(t, `<html><body>
		<style>body { background: url(https://evil.example/track.gif); } @import "https://evil.example/x.css";</style>
		<div style="background: url('https://evil.example/track2.gif')">x</div>
	</body></html>`)

	if strings.Contains(out, "evil.example") {
		t.Fatalf("expected url()/@import references to be stripped, got: %s", out)
	}
}

func TestSanitizeReplacesImgWithAltPlaceholder(t *testing.T) {
	out, elements := mustSanitize(t, `<html><body>
		<figure><img src="https://evil.example/track.gif" alt="a photo"></figure>
	</body></html>`)

	if strings.Contains(out, "<img") || strings.Contains(out, "evil.example") {
		t.Fatalf("expected <img> to be replaced entirely, got: %s", out)
	}
	if !strings.Contains(out, "a photo") {
		t.Fatalf("expected alt text to survive as placeholder content, got: %s", out)
	}

	var imgEl *Element
	for i := range elements {
		if elements[i].Tag == "img" {
			imgEl = &elements[i]
		}
	}
	if imgEl == nil {
		t.Fatalf("expected an indexed element with Tag \"img\", got: %+v", elements)
	}
}

func TestSanitizeDropsUnknownTagSubtreeEntirely(t *testing.T) {
	out, elements := mustSanitize(t, `<html><body>
		<custom-widget class="post"><h2>Title</h2></custom-widget>
	</body></html>`)

	if strings.Contains(out, "Title") {
		t.Fatalf("expected the whole subtree under an unknown tag to be dropped, got: %s", out)
	}
	if len(elements) != 0 {
		t.Fatalf("expected no indexed elements, got: %+v", elements)
	}
}

func TestSanitizeAssignsParentIDsMatchingDocumentStructure(t *testing.T) {
	_, elements := mustSanitize(t, `<html><body>
		<ul><li>a</li><li>b</li></ul>
	</body></html>`)

	if len(elements) != 3 {
		t.Fatalf("expected 3 elements (ul, li, li), got %d: %+v", len(elements), elements)
	}
	ul, li1, li2 := elements[0], elements[1], elements[2]
	if ul.Tag != "ul" || ul.ParentID != 0 {
		t.Fatalf("expected ul to be top-level: %+v", ul)
	}
	if li1.Tag != "li" || li1.ParentID != ul.ID {
		t.Fatalf("expected first li to be a child of ul: %+v", li1)
	}
	if li2.Tag != "li" || li2.ParentID != ul.ID {
		t.Fatalf("expected second li to be a child of ul: %+v", li2)
	}
}

func TestSanitizeMalformedHRDoesNotBreakRendering(t *testing.T) {
	// A lenient HTML parser can nest content under <hr> for malformed
	// input; Sanitize must not hand html.Render a void element with
	// children.
	out, _ := mustSanitize(t, `<html><body><hr><div>after</div></body></html>`)
	if !strings.Contains(out, "after") {
		t.Fatalf("expected content after <hr> to survive, got: %s", out)
	}
}

func TestSanitizeEmbedsPickerScriptByteForByte(t *testing.T) {
	out, _ := mustSanitize(t, `<html><body><p>hi</p></body></html>`)

	start := strings.Index(out, "<script>")
	end := strings.Index(out, "</script>")
	if start == -1 || end == -1 || end < start {
		t.Fatalf("expected exactly one <script>...</script> in output, got: %s", out)
	}
	got := out[start+len("<script>") : end]
	if got != pickerScript {
		t.Fatalf("served script content does not byte-match pickerScript:\n got: %q\nwant: %q", got, pickerScript)
	}

	// The CSP hash must match exactly what was served, computed
	// independently here rather than reusing PickerScriptSHA256, so a bug
	// in computeScriptHash itself would still be caught.
	independentHash := computeScriptHash(got)
	if independentHash != PickerScriptSHA256 {
		t.Fatalf("PickerScriptSHA256 (%s) does not match hash of served script (%s)", PickerScriptSHA256, independentHash)
	}
}
