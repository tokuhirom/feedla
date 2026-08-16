package pagewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestExtract_MVPGoldenSequence feeds the anonymized a-k-r.org fixture
// sequence through Extract in order and checks the entry counts documented
// in docs/feedless-site-subscription-pagewatch.md §14.3: "旧版 → 新記事1件
// 追加 → 同一 → lastmodだけ変更 → 古い記事が押し出された版" must yield
// 1, 1, 0, 0, 0 entries. See testdata/akr-v1.html's header comment for how
// the fixture was generated (tools/htmlskeleton from the real page, with no
// real text committed) and akr-v2..v5.html for how the later steps were
// hand-derived from it while staying inside the anonymized character set.
func TestExtract_MVPGoldenSequence(t *testing.T) {
	steps := []string{"akr-v1.html", "akr-v2.html", "akr-v3.html", "akr-v4.html", "akr-v5.html"}
	want := []int{1, 1, 0, 0, 0}

	s := &sim{t: t}
	for i, name := range steps {
		htm := readTestdata(t, name)
		now := baseTime.Add(time.Duration(i) * time.Hour)
		res := s.extract(htm, "", now)
		if got := len(res.Feed.Items); got != want[i] {
			t.Errorf("step %d (%s): Items = %d, want %d", i+1, name, got, want[i])
		}
	}
}

// TestExtract_TabesugiFixtureFirstRun is a lighter structural smoke test for
// the second MVP site (tabesugi.net, §14.2): its real page uses unclosed
// <p> tags and an HTML5-only charset declaration (<meta http-equiv>, no
// header charset), which a naive hand-written fixture would likely get
// wrong. Extracting it without error confirms the html.Parse-based pipeline
// handles that real structure.
func TestExtract_TabesugiFixtureFirstRun(t *testing.T) {
	htm := readTestdata(t, "tabesugi-v1.html")
	res := extractOnce(t, htm, "", nil, baseTime)
	if len(res.Feed.Items) != 1 {
		t.Fatalf("Items = %d, want 1 (first run)", len(res.Feed.Items))
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return string(b)
}
