package pagewatch

import (
	"strings"
	"testing"
)

func TestFilterAttrs_TrackingParamsAndAbsolutize(t *testing.T) {
	raw := `<html><body><p><a href="/posts/1?utm_source=twitter&id=42" class="foo" onclick="x()">記事へのリンクです</a></p>
<img src="/img/1.png?fbclid=abc" alt="説明" width="100">
</body></html>`
	body := prepareBody(t, raw, "https://example.com/base/")
	rendered := renderNode(body)

	if strings.Contains(rendered, "utm_source") || strings.Contains(rendered, "fbclid") {
		t.Errorf("tracking params not stripped: %s", rendered)
	}
	if !strings.Contains(rendered, `href="https://example.com/posts/1?id=42"`) {
		t.Errorf("href not absolutized/normalized as expected: %s", rendered)
	}
	if !strings.Contains(rendered, `src="https://example.com/img/1.png"`) {
		t.Errorf("src not absolutized as expected: %s", rendered)
	}
	if strings.Contains(rendered, "class=") || strings.Contains(rendered, "onclick=") || strings.Contains(rendered, "width=") {
		t.Errorf("disallowed attrs leaked through: %s", rendered)
	}
	if !strings.Contains(rendered, `alt="説明"`) {
		t.Errorf("allowed img alt was dropped: %s", rendered)
	}
}
