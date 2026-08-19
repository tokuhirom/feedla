import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// Regression test for issue #47: on narrow (phone-width) viewports the
// erroring-feeds list (今はサイドバーの⚠バッジから開くフィード管理画面の
// エラー絞り込み状態) used to be rendered as the same small centered popup
// used on desktop, a poor fit for a list that can grow to hold an unbounded
// number of erroring feeds. FeedManagerPane now renders inline in
// .entry-pane like any feed/group/search view (see EntryPane's
// feedManagerMode branch), which gets the full-screen-on-mobile behavior
// and a sticky header for free from the same mechanism every other view
// uses -- this asserts that mechanism actually delivers it here too.
test.use({ viewport: { width: 390, height: 600 } })

const FLAKY_COUNT = 8

test('スマホ幅ではエラー一覧が全画面ページになり、戻るボタンがスクロールなしで押せる', async ({ page }) => {
  // fixtures/feed-server.mjs's host is throttled to ~1 request/sec (see
  // internal/crawler's per-host semaphore), and bumping each of the 8 feeds
  // to the ERRORING_THRESHOLD (3) below needs 3 serialized requests per feed
  // (24 total) -- comfortably over the default 30s test timeout on its own,
  // before any UI overhead.
  test.setTimeout(90_000)

  await page.goto('/')

  for (let i = 1; i <= FLAKY_COUNT; i++) {
    await subscribeFeed(page, `http://127.0.0.1:18098/flaky-mobile-${i}`)
    // Subscribing auto-selects the feed into the single-pane entry view (see
    // mobile-flow.spec.ts), hiding the sidebar -- back out to it so the
    // subscription row (and the next add's "購読を追加" button) are reachable.
    await expect(page.locator('.entry-pane')).toBeVisible({ timeout: 10_000 })

    // Subscribing only fails once (error_count=1); the sidebar/フィード管理
    // only treat a feed as "erroring" once it fails ERRORING_THRESHOLD (3)
    // times in a row, so force two more failed re-crawls via this feed's
    // entry header before backing out.
    const refreshButton = page.getByTitle('再クロール (r)')
    for (let j = 0; j < 2; j++) {
      await Promise.all([
        page.waitForResponse(
          (resp) => resp.url().includes('/refresh') && resp.request().method() === 'POST',
        ),
        refreshButton.click(),
      ])
    }

    await page.locator('.back-button').click()
    await expect(page.locator('.subscription-row', { hasText: `Flaky Feed mobile-${i}` })).toBeVisible({
      timeout: 10_000,
    })
  }

  const errorBadge = page.locator('.error-badge')
  await expect(errorBadge).toHaveText(/⚠ \d+/, { timeout: 10_000 })
  await errorBadge.click()

  const panel = page.locator('.entry-pane')
  await expect(panel).toBeVisible()

  // Full-page, not a small centered popup: the panel fills the viewport
  // rather than floating in the middle of it with a visible dimmed backdrop
  // around its edges.
  const panelBox = (await panel.boundingBox())!
  const viewport = page.viewportSize()!
  expect(panelBox.width).toBeCloseTo(viewport.width, 0)
  expect(panelBox.height).toBeCloseTo(viewport.height, 0)

  const items = page.locator('.error-feed-list li').filter({ hasText: /^Flaky Feed mobile-\d+/ })
  await expect(items).toHaveCount(FLAKY_COUNT)

  // The list overflows the viewport (that's the whole premise of this
  // fix), yet the back button in the sticky header stays visible without
  // scrolling.
  await expect(async () => {
    expect(await panel.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
  }).toPass({ timeout: 5_000 })

  const backButton = page.locator('.back-button')
  await expect(backButton).toBeInViewport()
  await backButton.click()
  await expect(panel).toBeHidden()
})
