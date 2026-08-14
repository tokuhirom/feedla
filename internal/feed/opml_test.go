package feed_test

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/tokuhirom/feedla/internal/feed"
)

// Many Japanese feed readers export OPML declared as Shift_JIS rather than
// UTF-8. encoding/xml has no built-in support for that; ParseOPML must wire
// up a CharsetReader so such documents still parse instead of erroring out.
func TestParseOPMLShiftJIS(t *testing.T) {
	const utf8Doc = `<?xml version="1.0" encoding="Shift_JIS"?>
<opml version="1.0">
  <head><title>購読リスト</title></head>
  <body>
    <outline text="ニュース">
      <outline text="サンプルフィード" title="サンプルフィード" type="rss"
        xmlUrl="https://a.example.com/feed" htmlUrl="https://a.example.com/"/>
    </outline>
  </body>
</opml>`

	sjisDoc, _, err := transform.Bytes(japanese.ShiftJIS.NewEncoder(), []byte(utf8Doc))
	if err != nil {
		t.Fatalf("encode Shift_JIS fixture: %v", err)
	}

	feeds, err := feed.ParseOPML(bytes.NewReader(sjisDoc))
	if err != nil {
		t.Fatalf("ParseOPML: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("len(feeds) = %d, want 1", len(feeds))
	}
	if feeds[0].Title != "サンプルフィード" {
		t.Fatalf("Title = %q, want サンプルフィード", feeds[0].Title)
	}
	if feeds[0].FolderName != "ニュース" {
		t.Fatalf("FolderName = %q, want ニュース", feeds[0].FolderName)
	}
}
