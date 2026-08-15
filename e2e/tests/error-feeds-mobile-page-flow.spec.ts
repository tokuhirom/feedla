import { expect, test } from '@playwright/test'

// Regression test for issue #47: on narrow (phone-width) viewports the
// erroring-feeds list (今はサイドバーの⚠バッジから開くフィード管理画面の
// エラー絞り込み状態) was rendered as the same small centered popup used on
// desktop, which is a poor fit for a list that can grow to hold an unbounded
// number of erroring feeds. It should instead behave as its own full-screen
// page, with the close button reachable from a fixed header rather than
// requiring a scroll to the bottom of a potentially long list.
test.use({ viewport: { width: 390, height: 600 } })

const FLAKY_COUNT = 8

test('スマホ幅ではエラー一覧が全画面ページになり、閉じるボタンがスクロールなしで押せる', async ({ page }) => {
  await page.goto('/')

  for (let i = 1; i <= FLAKY_COUNT; i++) {
    await page.getByTitle('購読を追加').click()
    await page.getByPlaceholder('https://example.com/feed.xml').fill(`http://127.0.0.1:18098/flaky-mobile-${i}`)
    await page.getByRole('button', { name: '追加' }).click()
    // Subscribing auto-selects the feed into the single-pane entry view (see
    // mobile-flow.spec.ts), hiding the sidebar -- back out to it so the
    // subscription row (and the next add's "購読を追加" button) are reachable.
    await expect(page.locator('.entry-pane')).toBeVisible({ timeout: 10_000 })
    await page.locator('.back-button').click()
    await expect(page.locator('.subscription-row', { hasText: `Flaky Feed mobile-${i}` })).toBeVisible({
      timeout: 10_000,
    })
  }

  const errorBadge = page.locator('.error-badge')
  await expect(errorBadge).toHaveText(/⚠ \d+/, { timeout: 10_000 })
  await errorBadge.click()

  const panel = page.locator('.error-feed-panel')
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
  // fix), yet the close button in the header stays visible without
  // scrolling.
  const list = page.locator('.error-feed-list')
  await expect(async () => {
    expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
  }).toPass({ timeout: 5_000 })

  const closeButton = page.locator('.error-feed-close')
  await expect(closeButton).toBeInViewport()
  await closeButton.click()
  await expect(panel).toBeHidden()
})
