import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// Regression test for issue #38: the erroring-feeds list (now the sidebar's
// ⚠ badge opening フィード管理 pre-filtered to errors, rendered inline in
// .entry-pane rather than a modal) had no max-height/overflow, so once
// enough erroring feeds accumulated to overflow the panel, the extras were
// simply clipped off-screen with no way to scroll to them. A short viewport
// here means fewer flaky feeds are needed to force that overflow.
test.use({ viewport: { width: 800, height: 400 } })

const FLAKY_COUNT = 5

test('エラーのあるフィード一覧がパネルからあふれてもスクロールできる', async ({ page }) => {
  await page.goto('/')

  for (let i = 1; i <= FLAKY_COUNT; i++) {
    await subscribeFeed(page, `http://127.0.0.1:18098/flaky-${i}`)
    // The flaky fixture path 404s on its second request -- DiscoverFeed's
    // validation fetch (the subscribe's first hit) succeeds, but the
    // crawler's own fetch right after (still inside the same subscribe
    // request) is the second hit and fails, so error_count is already 1
    // by the time this row appears.
    await expect(page.locator('.subscription-row', { hasText: `Flaky Feed ${i}` })).toBeVisible({ timeout: 10_000 })

    // The sidebar/フィード管理 only surface a feed as "erroring" once it
    // fails ERRORING_THRESHOLD (3) times in a row, so subscribing's single
    // failure isn't enough -- force two more failed re-crawls via the
    // just-subscribed feed's entry header (subscribing auto-selects it).
    const refreshButton = page.getByTitle('再クロール (r)')
    for (let j = 0; j < 2; j++) {
      await Promise.all([
        page.waitForResponse(
          (resp) => resp.url().includes('/refresh') && resp.request().method() === 'POST',
        ),
        refreshButton.click(),
      ])
    }
  }

  // The badge counts every erroring subscription in the shared-suite DB
  // (see playwright.config.ts), not just this test's -- other specs' own
  // flaky feeds can already be in there, so only wait for it to reflect at
  // least this test's contribution rather than an exact count.
  const errorBadge = page.locator('.error-badge')
  await expect(errorBadge).toHaveText(/⚠ \d+/, { timeout: 10_000 })
  await errorBadge.click()

  // Scope to this test's own rows (titled "Flaky Feed 1".."Flaky Feed 5")
  // so other specs' erroring feeds mixed into the same list don't affect
  // the overflow math below.
  const items = page.locator('.error-feed-list li').filter({ hasText: /^Flaky Feed \d+/ })
  await expect(items).toHaveCount(FLAKY_COUNT)

  const panel = page.locator('.entry-pane')
  await expect(async () => {
    expect(await panel.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
  }).toPass({ timeout: 5_000 })

  // Before scrolling, the last row overflows past the panel's clipped
  // bottom edge.
  const panelBox = (await panel.boundingBox())!
  const lastItem = items.last()
  const beforeBox = (await lastItem.boundingBox())!
  expect(beforeBox.y + beforeBox.height).toBeGreaterThan(panelBox.y + panelBox.height)

  // Scrolling the panel to its end brings the last row within its bounds.
  await panel.evaluate((el) => {
    el.scrollTop = el.scrollHeight
  })
  const afterBox = (await lastItem.boundingBox())!
  expect(afterBox.y + afterBox.height).toBeLessThanOrEqual(panelBox.y + panelBox.height + 1)
})
