// A tiny, dependency-free RSS server used as the subscription target in
// e2e tests, so tests exercise the real crawler/parser pipeline instead of
// mocking it. Port is passed as argv[2].
//
// Serves two distinct feeds by path so tests that run in the same suite
// (and thus share one feedla process/DB -- see playwright.config.ts) don't
// collide by both subscribing to the same feed_url.
import http from 'node:http'

const port = Number(process.argv[2] || 8092)

const feedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>E2E Fixture Feed</title>
<link>http://127.0.0.1:${port}/</link>
<item>
  <title>First Article</title>
  <link>http://127.0.0.1:${port}/1</link>
  <guid>e2e-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of first article</description>
</item>
<item>
  <title>Second Article</title>
  <link>http://127.0.0.1:${port}/2</link>
  <guid>e2e-guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of second article</description>
</item>
</channel></rss>`

// Dedicated feed for repro-read-reload.spec.ts -- it must not share
// dogfood-flow.spec.ts's feedXml (the bare "/" path), since both mark
// entries read and assert on unread counts against the same shared-suite
// DB; two specs racing over one feed's read state is exactly the kind of
// cross-test interference the per-path fixtures elsewhere in this file
// exist to avoid.
const readReloadFixtureFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Read Reload Fixture Feed</title>
<link>http://127.0.0.1:${port}/read-reload-fixture</link>
<item>
  <title>Read Reload First</title>
  <link>http://127.0.0.1:${port}/read-reload-fixture/1</link>
  <guid>read-reload-fixture-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of read reload first item</description>
</item>
<item>
  <title>Read Reload Second</title>
  <link>http://127.0.0.1:${port}/read-reload-fixture/2</link>
  <guid>read-reload-fixture-guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of read reload second item</description>
</item>
</channel></rss>`

const searchFixtureFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Search Fixture Feed</title>
<link>http://127.0.0.1:${port}/search-fixture</link>
<item>
  <title>Search Alpha Item</title>
  <link>http://127.0.0.1:${port}/search-fixture/1</link>
  <guid>search-fixture-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of alpha item</description>
</item>
<item>
  <title>Search Beta Item</title>
  <link>http://127.0.0.1:${port}/search-fixture/2</link>
  <guid>search-fixture-guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of beta item</description>
</item>
</channel></rss>`

// Long body (repeated paragraphs) so the first entry is taller than a
// phone viewport -- the mobile flow test needs to scroll past it to
// exercise auto-mark-read, which a one-line body can't guarantee.
const longBody = '<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>'.repeat(40)

const mobileFixtureFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Mobile Fixture Feed</title>
<link>http://127.0.0.1:${port}/mobile-fixture</link>
<item>
  <title>Mobile Tall Item</title>
  <link>http://127.0.0.1:${port}/mobile-fixture/1</link>
  <guid>mobile-fixture-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
<item>
  <title>Mobile Second Item</title>
  <link>http://127.0.0.1:${port}/mobile-fixture/2</link>
  <guid>mobile-fixture-guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of second mobile item</description>
</item>
</channel></rss>`

// A single short entry -- nothing to scroll past, so there's no 'scroll'
// event for useAutoMarkRead's tail fallback to hang off of. Dedicated
// mobile-flow.spec.ts case for the "lone short entry never gets marked
// read" bug: the fix listens for 'touchmove' as the equivalent signal.
const mobileSingleShortFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Mobile Single Short Feed</title>
<link>http://127.0.0.1:${port}/mobile-single-short</link>
<item>
  <title>Mobile Single Short Item</title>
  <link>http://127.0.0.1:${port}/mobile-single-short/1</link>
  <guid>mobile-single-short-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of the lone short item</description>
</item>
</channel></rss>`

// Two feeds for nav-order-flow.spec.ts. Titles are deliberately reversed
// from subscribe order (Two subscribed first, One second) so the flat
// subscribe/feed_id order disagrees with both alphabetical (プライオリティ)
// and display order -- exactly the mismatch adjacentFeedId (s/a) must not
// have. Both share the "Zzz Nav Feed" prefix, sorting after every other
// fixture feed's title used elsewhere in this suite (which share one DB --
// see playwright.config.ts) so nothing else can land between them
// alphabetically and break the adjacency this test relies on.
// Item titles/bodies deliberately avoid the word "Alpha" (in any case) --
// search-pin-opml-flow.spec.ts searches for that keyword against the shared
// suite DB, and an incidental match here would inflate its result count.
// Bodies use the tall longBody (see above) rather than a one-liner: s/a now
// skip feeds with zero unread (adjacentFeedId), and selectAndLoadFeed marks
// an entry read on leaving *only* if it fit entirely within the pane --
// a short one-liner would do that, so the round-trip s-then-a this test
// relies on would leave the first feed read and thus skipped.
const navZetaFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Zzz Nav Feed Two</title>
<link>http://127.0.0.1:${port}/nav-fixture-zeta</link>
<item>
  <title>Nav Fixture Item One</title>
  <link>http://127.0.0.1:${port}/nav-fixture-zeta/1</link>
  <guid>nav-fixture-zeta-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
</channel></rss>`

const navAlphaFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Zzz Nav Feed One</title>
<link>http://127.0.0.1:${port}/nav-fixture-alpha</link>
<item>
  <title>Nav Fixture Item Two</title>
  <link>http://127.0.0.1:${port}/nav-fixture-alpha/1</link>
  <guid>nav-fixture-alpha-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
</channel></rss>`

// Two feeds for shortcuts-flow.spec.ts's rating (+/-) and shift+j tests.
const shortcutFeedAXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Shortcut Fixture Feed A</title>
<link>http://127.0.0.1:${port}/shortcut-fixture-a</link>
<item>
  <title>Shortcut A First</title>
  <link>http://127.0.0.1:${port}/shortcut-fixture-a/1</link>
  <guid>shortcut-fixture-a-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of shortcut A first item</description>
</item>
<item>
  <title>Shortcut A Second</title>
  <link>http://127.0.0.1:${port}/shortcut-fixture-a/2</link>
  <guid>shortcut-fixture-a-guid-2</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of shortcut A second item</description>
</item>
</channel></rss>`

// Body uses the tall longBody, not a one-liner: AddSubscriptionDialog
// auto-selects B as soon as it's subscribed, and the shift+j test then
// clicks straight to feed A -- selectAndLoadFeed's mark-on-leave
// (markVisibleEntriesRead) would otherwise mark B's lone short entry read
// right there, well before shift+j is meant to reach it (and now that s/a
// (adjacentFeedId) skip zero-unread feeds, that would make it skip B).
const shortcutFeedBXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Shortcut Fixture Feed B</title>
<link>http://127.0.0.1:${port}/shortcut-fixture-b</link>
<item>
  <title>Shortcut B First</title>
  <link>http://127.0.0.1:${port}/shortcut-fixture-b/1</link>
  <guid>shortcut-fixture-b-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
</channel></rss>`

// Three feeds for shortcuts-flow.spec.ts's shift+j-skips-read-feeds test.
// All three share one pubDate -- a genuinely parseable past date (like every
// other fixture; the crawler clamps unparseable/implausible-future dates
// like a year-2099 pubDate to fetch time instead, which made an earlier
// version of this fixture order by fetch sequence rather than the intended
// date), unique among this suite's fixtures so these three tie with each
// other but nothing else and fall back to alphabetical title -- same trick
// as nav-fixture-*'s doc comment above, but on a dedicated date instead of
// reusing 2006-01-02. Titles are A/B/C (not One/Two/Three) so that
// alphabetical fallback order matches subscribe order directly. Bodies use
// the tall longBody (see above), not a one-liner: AddSubscriptionDialog
// auto-selects each feed as it's subscribed, and the test then clicks
// between rows -- selectAndLoadFeed's mark-on-leave (markVisibleEntriesRead)
// would otherwise mark a short, fully-visible entry read the moment the
// test clicks a different feed's row, well before the test ever means to
// mark anything read.
const unreadSkipFeedAXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Zzz Unread Skip Feed A</title>
<link>http://127.0.0.1:${port}/unread-skip-fixture-a</link>
<item>
  <title>Unread Skip A First</title>
  <link>http://127.0.0.1:${port}/unread-skip-fixture-a/1</link>
  <guid>unread-skip-fixture-a-guid-1</guid>
  <pubDate>Sun, 08 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
</channel></rss>`

const unreadSkipFeedBXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Zzz Unread Skip Feed B</title>
<link>http://127.0.0.1:${port}/unread-skip-fixture-b</link>
<item>
  <title>Unread Skip B First</title>
  <link>http://127.0.0.1:${port}/unread-skip-fixture-b/1</link>
  <guid>unread-skip-fixture-b-guid-1</guid>
  <pubDate>Sun, 08 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
</channel></rss>`

const unreadSkipFeedCXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Zzz Unread Skip Feed C</title>
<link>http://127.0.0.1:${port}/unread-skip-fixture-c</link>
<item>
  <title>Unread Skip C First</title>
  <link>http://127.0.0.1:${port}/unread-skip-fixture-c/1</link>
  <guid>unread-skip-fixture-c-guid-1</guid>
  <pubDate>Sun, 08 Jan 2006 15:04:05 GMT</pubDate>
  <description><![CDATA[${longBody}]]></description>
</item>
</channel></rss>`

// Two feeds for sort-order-flow.spec.ts (issue #33): distinct pubDates so
// "unread-first, then newest-entry-first" has something to sort by. Names
// deliberately don't sort alphabetically the same way as by date, so a test
// asserting date-order-not-alphabetical-order can't pass by accident.
const sortFixtureOldFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Sort Fixture Feed Zulu (old)</title>
<link>http://127.0.0.1:${port}/sort-fixture-old</link>
<item>
  <title>Sort Fixture Old Item</title>
  <link>http://127.0.0.1:${port}/sort-fixture-old/1</link>
  <guid>sort-fixture-old-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of sort fixture old item</description>
</item>
</channel></rss>`

const sortFixtureNewFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Sort Fixture Feed Alpha (new)</title>
<link>http://127.0.0.1:${port}/sort-fixture-new</link>
<item>
  <title>Sort Fixture New Item</title>
  <link>http://127.0.0.1:${port}/sort-fixture-new/1</link>
  <guid>sort-fixture-new-guid-1</guid>
  <pubDate>Wed, 04 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of sort fixture new item</description>
</item>
</channel></rss>`

// Ten short (non-tall) entries for scroll-follow-flow.spec.ts (issue #37) --
// short enough that j/k's old behavior (always trusting the remembered
// focusedIndex) can't be told apart from correct behavior when nothing
// scrolls independently, but with enough total entries that scrolling a few
// screens down via the mouse wheel lands mid-list, past several entries.
const scrollFollowFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Scroll Follow Feed</title>
<link>http://127.0.0.1:${port}/scroll-follow</link>
${Array.from(
  { length: 10 },
  (_, i) => `
<item>
  <title>Scroll Follow Item ${i + 1}</title>
  <link>http://127.0.0.1:${port}/scroll-follow/${i + 1}</link>
  <guid>scroll-follow-guid-${i + 1}</guid>
  <pubDate>Mon, 02 Jan 2006 15:0${i % 6}:05 GMT</pubDate>
  <description>Body of scroll follow item ${i + 1}</description>
</item>`,
).join('')}
</channel></rss>`

// Two single-short-entry feeds for no-scroll-mark-read-flow.spec.ts: an
// entry short enough to fit the pane without any scrolling never fires
// useAutoMarkRead's scroll/touchmove-driven paths, so switching feeds before
// ever scrolling is the only thing that can mark it read.
const noScrollFixtureFeedAXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>No Scroll Fixture Feed A</title>
<link>http://127.0.0.1:${port}/no-scroll-fixture-a</link>
<item>
  <title>No Scroll Fixture A Item</title>
  <link>http://127.0.0.1:${port}/no-scroll-fixture-a/1</link>
  <guid>no-scroll-fixture-a-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of no scroll fixture A item</description>
</item>
</channel></rss>`

const noScrollFixtureFeedBXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>No Scroll Fixture Feed B</title>
<link>http://127.0.0.1:${port}/no-scroll-fixture-b</link>
<item>
  <title>No Scroll Fixture B Item</title>
  <link>http://127.0.0.1:${port}/no-scroll-fixture-b/1</link>
  <guid>no-scroll-fixture-b-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of no scroll fixture B item</description>
</item>
</channel></rss>`

// Two feeds for group-header-current-feed-flow.spec.ts -- each single-entry,
// subscribed into a dedicated test-only folder so the group view's entry
// list contains exactly these two entries and nothing from the rest of the
// shared-suite DB.
const headerFixtureFeedAXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Header Fixture Feed A</title>
<link>http://127.0.0.1:${port}/header-fixture-a</link>
<item>
  <title>Header Fixture A Item</title>
  <link>http://127.0.0.1:${port}/header-fixture-a/1</link>
  <guid>header-fixture-a-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of header fixture A item</description>
</item>
</channel></rss>`

const headerFixtureFeedBXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Header Fixture Feed B</title>
<link>http://127.0.0.1:${port}/header-fixture-b</link>
<item>
  <title>Header Fixture B Item</title>
  <link>http://127.0.0.1:${port}/header-fixture-b/1</link>
  <guid>header-fixture-b-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of header fixture B item</description>
</item>
</channel></rss>`

// Single feed for feed-detail-move-folder-flow.spec.ts, which moves it
// between two test-only folders via the feed detail overlay's カテゴリ select.
const moveFolderFixtureFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Move Folder Fixture Feed</title>
<link>http://127.0.0.1:${port}/move-folder-fixture</link>
<item>
  <title>Move Folder Fixture Item</title>
  <link>http://127.0.0.1:${port}/move-folder-fixture/1</link>
  <guid>move-folder-fixture-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of move folder fixture item</description>
</item>
</channel></rss>`

// Two feeds for mark-all-read-flow.spec.ts, which exercises the Sidebar ⋮
// menu's "すべて既読にする" bulk action across feeds -- must be two distinct
// feeds (not one) so the test can confirm unread_count drops to 0 on both,
// not just the one currently selected.
const markAllReadFixtureFeedAXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Mark All Read Fixture Feed A</title>
<link>http://127.0.0.1:${port}/mark-all-read-fixture-a</link>
<item>
  <title>Mark All Read Fixture A Item</title>
  <link>http://127.0.0.1:${port}/mark-all-read-fixture-a/1</link>
  <guid>mark-all-read-fixture-a-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of mark all read fixture A item</description>
</item>
</channel></rss>`

const markAllReadFixtureFeedBXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Mark All Read Fixture Feed B</title>
<link>http://127.0.0.1:${port}/mark-all-read-fixture-b</link>
<item>
  <title>Mark All Read Fixture B Item</title>
  <link>http://127.0.0.1:${port}/mark-all-read-fixture-b/1</link>
  <guid>mark-all-read-fixture-b-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:05:05 GMT</pubDate>
  <description>Body of mark all read fixture B item</description>
</item>
</channel></rss>`

// Dedicated feed for multi-user-isolation.spec.ts's owner subscription --
// must not collide with any other spec's fixture path, since the owner's
// feed_id/entries there are used to probe a second user's IDOR boundaries
// against real content.
const idorFixtureOwnerFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>IDOR Fixture Owner Feed</title>
<link>http://127.0.0.1:${port}/idor-fixture-owner</link>
<item>
  <title>IDOR Fixture Owner Item</title>
  <link>http://127.0.0.1:${port}/idor-fixture-owner/1</link>
  <guid>idor-fixture-owner-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of idor fixture owner item</description>
</item>
</channel></rss>`

// A plain HTML page (no RSS/Atom, no <link rel=alternate>) for
// pagewatch-flow.spec.ts: feedla's normal subscribe flow can't find a feed
// here, which is exactly the case that offers "ページの更新を監視する"
// (design doc §9.1). Content starts at v1 and only moves to v2 when the
// test explicitly POSTs /pagewatch-fixture/advance -- unlike the flaky-N
// pattern's hit-count-based flip, this can't be tripped by the extra
// fetches DiscoverFeed/preview make along the way (see the shapes proven
// by internal/extract/pagewatch/pagewatch_test.go's TestExtract_AdditionsOnly:
// a second <p> appended to the body is picked up as one added block, none
// removed).
let pagewatchVersion = 1
const pagewatchHtmlV1 =
  '<html><head><title>Pagewatch Fixture Diary</title></head><body>' +
  '<p>Pagewatch Fixture First Post.</p>' +
  '</body></html>'
const pagewatchHtmlV2 =
  '<html><head><title>Pagewatch Fixture Diary</title></head><body>' +
  '<p>Pagewatch Fixture First Post.</p>' +
  '<p>Pagewatch Fixture Second Post Added.</p>' +
  '</body></html>'

// Paths under /flaky-N (any N) serve a valid feed on their first request
// and 404 on every request after that -- for tests needing a feed that
// subscribes successfully but then starts erroring (issue #38's overflowing
// "エラーのあるフィード" list, issue #39's 404 message). Subscribing hits a
// flaky path twice already (feed.DiscoverFeed's validation fetch, then the
// crawler's own fetch of the same URL right after) so the second of those
// two already fails and registers the error -- no extra manual recrawl
// needed to get error_count > 0.
const flakyHitCounts = new Map()
function flakyFeedXml(pathname) {
  const n = pathname.replace(/^\/flaky-/, '')
  return `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Flaky Feed ${n}</title>
<link>http://127.0.0.1:${port}${pathname}</link>
<item>
  <title>Flaky Feed ${n} Item</title>
  <link>http://127.0.0.1:${port}${pathname}/1</link>
  <guid>flaky-${n}-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of flaky feed ${n} item</description>
</item>
</channel></rss>`
}

http
  .createServer((req, res) => {
    res.setHeader('Content-Type', 'application/rss+xml')
    if (req.url === '/pagewatch-fixture/advance') {
      pagewatchVersion = 2
      res.statusCode = 204
      res.end()
    } else if (req.url === '/pagewatch-fixture') {
      res.setHeader('Content-Type', 'text/html; charset=utf-8')
      res.end(pagewatchVersion === 1 ? pagewatchHtmlV1 : pagewatchHtmlV2)
    } else if (req.url.startsWith('/flaky-')) {
      const hits = (flakyHitCounts.get(req.url) || 0) + 1
      flakyHitCounts.set(req.url, hits)
      if (hits > 1) {
        res.statusCode = 404
        res.end('not found')
        return
      }
      res.end(flakyFeedXml(req.url))
    } else if (req.url === '/search-fixture') {
      res.end(searchFixtureFeedXml)
    } else if (req.url === '/mobile-fixture') {
      res.end(mobileFixtureFeedXml)
    } else if (req.url === '/mobile-single-short') {
      res.end(mobileSingleShortFeedXml)
    } else if (req.url === '/nav-fixture-zeta') {
      res.end(navZetaFeedXml)
    } else if (req.url === '/nav-fixture-alpha') {
      res.end(navAlphaFeedXml)
    } else if (req.url === '/shortcut-fixture-a') {
      res.end(shortcutFeedAXml)
    } else if (req.url === '/shortcut-fixture-b') {
      res.end(shortcutFeedBXml)
    } else if (req.url === '/unread-skip-fixture-a') {
      res.end(unreadSkipFeedAXml)
    } else if (req.url === '/unread-skip-fixture-b') {
      res.end(unreadSkipFeedBXml)
    } else if (req.url === '/unread-skip-fixture-c') {
      res.end(unreadSkipFeedCXml)
    } else if (req.url === '/scroll-follow') {
      res.end(scrollFollowFeedXml)
    } else if (req.url === '/no-scroll-fixture-a') {
      res.end(noScrollFixtureFeedAXml)
    } else if (req.url === '/no-scroll-fixture-b') {
      res.end(noScrollFixtureFeedBXml)
    } else if (req.url === '/header-fixture-a') {
      res.end(headerFixtureFeedAXml)
    } else if (req.url === '/header-fixture-b') {
      res.end(headerFixtureFeedBXml)
    } else if (req.url === '/read-reload-fixture') {
      res.end(readReloadFixtureFeedXml)
    } else if (req.url === '/sort-fixture-old') {
      res.end(sortFixtureOldFeedXml)
    } else if (req.url === '/sort-fixture-new') {
      res.end(sortFixtureNewFeedXml)
    } else if (req.url === '/move-folder-fixture') {
      res.end(moveFolderFixtureFeedXml)
    } else if (req.url === '/mark-all-read-fixture-a') {
      res.end(markAllReadFixtureFeedAXml)
    } else if (req.url === '/mark-all-read-fixture-b') {
      res.end(markAllReadFixtureFeedBXml)
    } else if (req.url === '/idor-fixture-owner') {
      res.end(idorFixtureOwnerFeedXml)
    } else {
      res.end(feedXml)
    }
  })
  .listen(port, '127.0.0.1', () => {
    console.log(`fixture feed server listening on ${port}`)
  })
