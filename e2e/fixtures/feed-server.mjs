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
const navZetaFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Zzz Nav Feed Two</title>
<link>http://127.0.0.1:${port}/nav-fixture-zeta</link>
<item>
  <title>Nav Fixture Item One</title>
  <link>http://127.0.0.1:${port}/nav-fixture-zeta/1</link>
  <guid>nav-fixture-zeta-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of nav fixture item one</description>
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
  <description>Body of nav fixture item two</description>
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

const shortcutFeedBXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Shortcut Fixture Feed B</title>
<link>http://127.0.0.1:${port}/shortcut-fixture-b</link>
<item>
  <title>Shortcut B First</title>
  <link>http://127.0.0.1:${port}/shortcut-fixture-b/1</link>
  <guid>shortcut-fixture-b-guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>Body of shortcut B first item</description>
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

// Three short entries for mobile-swipe-flow.spec.ts -- enough to swipe
// forward twice and back once without running off either end of the list.
const mobileSwipeFeedXml = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Mobile Swipe Fixture Feed</title>
<link>http://127.0.0.1:${port}/mobile-swipe-fixture</link>
${Array.from(
  { length: 3 },
  (_, i) => `
<item>
  <title>Mobile Swipe Item ${i + 1}</title>
  <link>http://127.0.0.1:${port}/mobile-swipe-fixture/${i + 1}</link>
  <guid>mobile-swipe-fixture-guid-${i + 1}</guid>
  <pubDate>Mon, 02 Jan 2006 15:0${i}:05 GMT</pubDate>
  <description>Body of mobile swipe item ${i + 1}</description>
</item>`,
).join('')}
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
    if (req.url.startsWith('/flaky-')) {
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
    } else if (req.url === '/scroll-follow') {
      res.end(scrollFollowFeedXml)
    } else if (req.url === '/mobile-swipe-fixture') {
      res.end(mobileSwipeFeedXml)
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
    } else {
      res.end(feedXml)
    }
  })
  .listen(port, '127.0.0.1', () => {
    console.log(`fixture feed server listening on ${port}`)
  })
