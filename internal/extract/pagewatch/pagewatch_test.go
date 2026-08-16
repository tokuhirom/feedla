package pagewatch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/extract"
)

func extractOnce(t *testing.T, htm, cfg string, state json.RawMessage, now time.Time) *extract.Result {
	t.Helper()
	e := New()
	var cfgRaw json.RawMessage
	if cfg != "" {
		cfgRaw = json.RawMessage(cfg)
	}
	res, err := e.Extract(context.Background(), extract.Input{
		URL:    "https://example.com/diary/",
		HTML:   []byte(htm),
		Now:    now,
		Config: cfgRaw,
		State:  state,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return res
}

var baseTime = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

// sim simulates a caller that persists scrape_sources.state correctly: a
// nil Result.State means "leave the stored state as-is" (§7.3), not "no
// state exists" — so it must NOT be forwarded as-is into the next Extract
// call, or the next call would misread it as a true first run.
type sim struct {
	t     *testing.T
	state json.RawMessage
}

func (s *sim) extract(htm, cfg string, now time.Time) *extract.Result {
	s.t.Helper()
	res := extractOnce(s.t, htm, cfg, s.state, now)
	if res.State != nil {
		s.state = res.State
	}
	return res
}

func TestExtract_FirstRun(t *testing.T) {
	htm := `<html><head><title>日記</title></head><body><p>最初の投稿です。</p></body></html>`
	res := extractOnce(t, htm, "", nil, baseTime)
	if len(res.Feed.Items) != 1 {
		t.Fatalf("Items = %d, want 1 (initial 'monitoring started' entry)", len(res.Feed.Items))
	}
	if !strings.Contains(res.Feed.Items[0].Content, "監視を開始しました") {
		t.Errorf("first entry content = %q, want the monitoring-started notice", res.Feed.Items[0].Content)
	}
	if len(res.State) == 0 {
		t.Fatal("State must be saved on first run")
	}
}

func TestExtract_NoChangeLeavesStateNil(t *testing.T) {
	htm := `<html><body><p>変わらない内容です。</p></body></html>`
	first := extractOnce(t, htm, "", nil, baseTime)
	second := extractOnce(t, htm, "", first.State, baseTime.Add(time.Hour))
	if len(second.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0 (no change)", len(second.Feed.Items))
	}
	if second.State != nil {
		t.Errorf("State = %s, want nil (§7.3: don't rewrite unchanged state)", second.State)
	}
}

func TestExtract_TrackingParamChurnDoesNotFireADiff(t *testing.T) {
	page := func(clickID string) string {
		return `<html><body><p>本文の説明です <a href="/posts/1?utm_source=twitter&id=42&fbclid=` + clickID + `">続きを読む</a></p></body></html>`
	}
	first := extractOnce(t, page("aaa"), "", nil, baseTime)
	second := extractOnce(t, page("bbb"), "", first.State, baseTime.Add(time.Hour))
	if len(second.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0: only a tracking-param value changed", len(second.Feed.Items))
	}
}

func TestExtract_AdditionsOnly(t *testing.T) {
	first := extractOnce(t, `<html><body><p>1件目の記事です。</p></body></html>`, "", nil, baseTime)
	second := extractOnce(t,
		`<html><body><p>1件目の記事です。</p><p>2件目の新しい記事です。</p></body></html>`,
		"", first.State, baseTime.Add(time.Hour))

	if len(second.Feed.Items) != 1 {
		t.Fatalf("Items = %d, want 1", len(second.Feed.Items))
	}
	content := second.Feed.Items[0].Content
	if !strings.Contains(content, "2件目の新しい記事です") {
		t.Errorf("missing added text: %s", content)
	}
	if !strings.Contains(content, "1 ブロック追加 / 0 ブロック削除") {
		t.Errorf("summary line wrong: %s", content)
	}
}

func TestExtract_RollingRemovalNotNotifiedByDefault(t *testing.T) {
	first := extractOnce(t, `<html><body><p>古い記事の本文です。</p></body></html>`, "", nil, baseTime)
	second := extractOnce(t, `<html><body><p>新しい記事の本文です。</p></body></html>`, "", first.State, baseTime.Add(time.Hour))

	if len(second.Feed.Items) != 1 {
		t.Fatalf("Items = %d, want 1", len(second.Feed.Items))
	}
	content := second.Feed.Items[0].Content
	if strings.Contains(content, "<del>") {
		t.Errorf("additions mode (the default) must not render a <del> section: %s", content)
	}
	if !strings.Contains(content, "新しい記事の本文です") {
		t.Errorf("missing added text: %s", content)
	}
}

func TestExtract_ChangesModeIncludesRemoved(t *testing.T) {
	cfg := `{"watch_mode":"changes"}`
	first := extractOnce(t, `<html><body><p>削除される予定の記事です。</p></body></html>`, cfg, nil, baseTime)
	second := extractOnce(t, `<html><body><p>新しい記事です。</p></body></html>`, cfg, first.State, baseTime.Add(time.Hour))

	content := second.Feed.Items[0].Content
	if !strings.Contains(content, "<del>") || !strings.Contains(content, "削除される予定の記事です") {
		t.Errorf("watch_mode=changes must show removed content: %s", content)
	}
}

func TestExtract_ConfigChangeAloneDoesNotFireOnUnchangedPage(t *testing.T) {
	htm := `<html><body><p>本文は変わりません。</p><p>Document ID: ccc333</p></body></html>`
	s := &sim{t: t}
	s.extract(htm, "", baseTime)                // first run: baseline
	s.extract(htm, "", baseTime.Add(time.Hour)) // unchanged: state stays at the first-run baseline (§7.3)

	cfg := `{"ignore_patterns":["Document ID: [A-Za-z0-9]+"]}`
	third := s.extract(htm, cfg, baseTime.Add(2*time.Hour))
	if len(third.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0: adding an ignore_pattern on an otherwise-unchanged page must not itself create an entry (§6.6)", len(third.Feed.Items))
	}
}

func TestExtract_IgnorePatternsSuppressNoiseGoingForward(t *testing.T) {
	cfg := `{"ignore_patterns":["Document ID: [A-Za-z0-9]+"]}`
	page := func(id string) string {
		return `<html><body><p>本文は変わりません。</p><p>Document ID: ` + id + `</p></body></html>`
	}

	s := &sim{t: t}
	s.extract(page("aaa111"), cfg, baseTime)
	s.extract(page("aaa111"), cfg, baseTime.Add(time.Hour))
	third := s.extract(page("bbb222"), cfg, baseTime.Add(2*time.Hour)) // only the ignored ID changed
	if len(third.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0: only the ignore_patterns-masked text changed", len(third.Feed.Items))
	}

	fourth := s.extract(page("bbb222")+`<p>実際に新しい記事が増えました。</p>`, cfg, baseTime.Add(3*time.Hour))
	if len(fourth.Feed.Items) != 1 {
		t.Fatalf("Items = %d, want 1: a real content change must still be detected", len(fourth.Feed.Items))
	}
}

func TestExtract_MinChangeChars(t *testing.T) {
	cfg := `{"min_change_chars":50}`
	first := extractOnce(t, `<html><body><p>本文です。</p></body></html>`, cfg, nil, baseTime)
	second := extractOnce(t, `<html><body><p>本文です。</p><p>短い。</p></body></html>`, cfg, first.State, baseTime.Add(time.Hour))

	if len(second.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0: change is below min_change_chars", len(second.Feed.Items))
	}
	if second.State == nil {
		t.Error("State must still be saved so the small change isn't re-reported repeatedly")
	}
}

func TestExtract_RulesVersionMismatchResyncsSilently(t *testing.T) {
	htm := `<html><body><p>本文です。</p></body></html>`
	staleState := json.RawMessage(`{"version":1,"rules_version":999,"config_hash":"x","content_hash":"y","blocks":[]}`)
	res := extractOnce(t, htm, "", staleState, baseTime)

	if len(res.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0 (resync, no entry)", len(res.Feed.Items))
	}
	if len(res.State) == 0 {
		t.Fatal("resync must still save a fresh baseline state")
	}
}

func TestExtract_CorruptStateDoesNotPanic(t *testing.T) {
	htm := `<html><body><p>本文です。</p></body></html>`
	res := extractOnce(t, htm, "", json.RawMessage(`{not valid json`), baseTime)
	if len(res.Feed.Items) != 0 {
		t.Fatalf("Items = %d, want 0", len(res.Feed.Items))
	}
}

func TestExtract_NoBlocksIsError(t *testing.T) {
	e := New()
	_, err := e.Extract(context.Background(), extract.Input{
		URL:  "https://example.com/",
		HTML: []byte(`<html><body><nav>メニューだけです</nav></body></html>`),
		Now:  baseTime,
	})
	if err == nil {
		t.Fatal("want an error when noise removal leaves zero blocks")
	}
}

func TestExtract_GUIDStableAcrossFlapping(t *testing.T) {
	a := `<html><body><p>状態Aです。</p></body></html>`
	b := `<html><body><p>状態Bです。</p></body></html>`

	r1 := extractOnce(t, a, "", nil, baseTime)                       // initial: baseline = A
	r2 := extractOnce(t, b, "", r1.State, baseTime.Add(time.Hour))   // A -> B
	r3 := extractOnce(t, a, "", r2.State, baseTime.Add(2*time.Hour)) // B -> A again

	if len(r2.Feed.Items) != 1 || len(r3.Feed.Items) != 1 {
		t.Fatalf("expected an entry for each transition, got r2=%d r3=%d", len(r2.Feed.Items), len(r3.Feed.Items))
	}
	if r3.Feed.Items[0].GUID == r2.Feed.Items[0].GUID {
		t.Errorf("state A and state B must not share a GUID")
	}
	if r1.Feed.Items[0].GUID != r3.Feed.Items[0].GUID {
		t.Errorf("returning to the original content should reuse GUID %q, got %q (§5.3)", r1.Feed.Items[0].GUID, r3.Feed.Items[0].GUID)
	}
}
