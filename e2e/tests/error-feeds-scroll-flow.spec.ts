import { expect, test } from '@playwright/test'

// Regression test for issue #38: the "エラーのあるフィード" (error feeds)
// overlay's .help-panel had no max-height/overflow, so once enough erroring
// feeds accumulated to overflow the panel, the extras were simply clipped
// off-screen with no way to scroll to them. A short viewport here means
// fewer flaky feeds are needed to force that overflow.
test.use({ viewport: { width: 800, height: 400 } })

const FLAKY_COUNT = 5

test('エラーのあるフィード一覧がパネルからあふれてもスクロールできる', async ({ page }) => {
  await page.goto('/')

  for (let i = 1; i <= FLAKY_COUNT; i++) {
    await page.getByTitle('購読を追加').click()
    await page.getByPlaceholder('https://example.com/feed.xml').fill(`http://127.0.0.1:18098/flaky-${i}`)
    await page.getByRole('button', { name: '追加' }).click()
    // The flaky fixture path 404s on its second request -- DiscoverFeed's
    // validation fetch (the subscribe's first hit) succeeds, but the
    // crawler's own fetch right after (still inside the same subscribe
    // request) is the second hit and fails, so error_count is already > 0
    // by the time this row appears.
    await expect(page.locator('.subscription-row', { hasText: `Flaky Feed ${i}` })).toBeVisible({ timeout: 10_000 })
  }

  const errorBadge = page.locator('.error-badge')
  await expect(errorBadge).toContainText(`⚠ ${FLAKY_COUNT}`, { timeout: 10_000 })
  await errorBadge.click()

  const items = page.locator('.error-feed-list li')
  await expect(items).toHaveCount(FLAKY_COUNT)

  const panel = page.locator('.help-panel')
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
