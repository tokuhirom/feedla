import { expect, test } from '@playwright/test'

// End-to-end coverage for feedless Phase F2's safe-display foundation
// (docs/feedless-site-subscription-selector.md §10.3/§10.5): the
// inspect/inspect-view endpoints and the sandboxed-iframe + postMessage
// mechanism they exist to support. There is no UI for this yet (the
// click-to-selector GUI itself is a later PR, §10.6) -- this spec drives
// the raw contract directly via fetch()/an injected iframe, which is
// exactly what §10.5 flags as needing real-browser verification before any
// UI gets built on top of it:
//   - a sandboxed iframe (no allow-same-origin) can navigate to
//     GET .../inspect/view and receive the sanitized page
//   - a click inside it reaches the outer page via postMessage, with
//     event.origin === "null" (an opaque origin), not the app's own origin
//   - the token is genuinely single-use
// internal/api/scrape_sources_inspect_test.go covers the same contract at
// the Go httptest level (including the no-cookie and cross-user cases);
// this spec is the one place a real sandboxed browsing context is
// involved.
const SELECTOR_FIXTURE_URL = 'http://127.0.0.1:18098/selector-fixture'

test('inspect: sandboxed iframe loads the sanitized page and a click reaches the parent via postMessage', async ({
  page,
}) => {
  await page.goto('/')

  // Listen before the iframe exists so nothing sent early is missed.
  await page.evaluate(() => {
    ;(window as unknown as { __inspectMessages: unknown[] }).__inspectMessages = []
    window.addEventListener('message', (ev) => {
      const data = ev.data as { type?: string; id?: number } | undefined
      if (data && data.type === 'feedla-inspect-click') {
        ;(window as unknown as { __inspectMessages: unknown[] }).__inspectMessages.push({
          origin: ev.origin,
          id: data.id,
        })
      }
    })
  })

  const inspectRes = await page.evaluate(async (url) => {
    const res = await fetch('/api/v1/scrape_sources/inspect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url }),
    })
    return { status: res.status, body: await res.json() }
  }, SELECTOR_FIXTURE_URL)
  expect(inspectRes.status).toBe(200)
  const viewUrl = inspectRes.body.view_url as string
  const elements = inspectRes.body.elements as { id: number; tag: string }[]
  expect(elements.length).toBeGreaterThan(0)
  expect(elements.some((e) => e.tag === 'article')).toBe(true)

  await page.evaluate((src) => {
    const iframe = document.createElement('iframe')
    iframe.id = 'inspect-test-iframe'
    iframe.sandbox.add('allow-scripts')
    iframe.src = src
    document.body.appendChild(iframe)
  }, viewUrl)

  const frame = page.frameLocator('#inspect-test-iframe')
  const article = frame.locator('article').first()
  await expect(article).toBeVisible({ timeout: 10_000 })
  await article.click()

  await expect
    .poll(
      () =>
        page.evaluate(
          () => (window as unknown as { __inspectMessages: unknown[] }).__inspectMessages.length,
        ),
      { timeout: 10_000 },
    )
    .toBeGreaterThan(0)

  const messages = (await page.evaluate(
    () => (window as unknown as { __inspectMessages: { origin: string; id: number }[] }).__inspectMessages,
  )) as { origin: string; id: number }[]
  expect(messages[0].origin).toBe('null')
  expect(elements.some((e) => e.id === messages[0].id)).toBe(true)

  // Single use: the same view_url must fail the second time (the iframe
  // navigation above already consumed it).
  const secondStatus = await page.evaluate(async (src) => (await fetch(src)).status, viewUrl)
  expect(secondStatus).toBe(404)
})
