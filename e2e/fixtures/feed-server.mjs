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

http
  .createServer((req, res) => {
    res.setHeader('Content-Type', 'application/rss+xml')
    if (req.url === '/search-fixture') {
      res.end(searchFixtureFeedXml)
    } else if (req.url === '/mobile-fixture') {
      res.end(mobileFixtureFeedXml)
    } else {
      res.end(feedXml)
    }
  })
  .listen(port, '127.0.0.1', () => {
    console.log(`fixture feed server listening on ${port}`)
  })
