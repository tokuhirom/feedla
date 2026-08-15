import { expect, test } from '@playwright/test'

// Exercises useSwipeNavigation: on a narrow viewport, a horizontal swipe
// across the entry pane should step focus forward/back exactly like j/k
// (see keyboard/useKeyboardShortcuts.ts), since a touch device has no
// keyboard to drive that flow directly and scrolling to a long entry's end
// just to reach the next one is exactly what this feature avoids.
test.use({ viewport: { width: 390, height: 844 } })

const MOBILE_SWIPE_FIXTURE_FEED_URL = 'http://127.0.0.1:18098/mobile-swipe-fixture'

// Dispatches a synthetic touchstart/touchend pair on .entry-pane, the same
// way mobile-flow.spec.ts fakes 'touchmove' -- real TouchEvent/Touch
// construction requires a touch-capable browser context (hasTouch), which
// this suite's Desktop Chrome project doesn't set up, but the handler only
// reads touches[0]/changedTouches[0].clientX/clientY, so a plain Event with
// those arrays attached exercises it identically.
async function swipe(page: import('@playwright/test').Page, fromX: number, toX: number, y = 400) {
  await page.evaluate(
    ({ fromX, toX, y }) => {
      const pane = document.querySelector('.entry-pane')!
      const start = new Event('touchstart', { bubbles: true })
      Object.defineProperty(start, 'touches', { value: [{ clientX: fromX, clientY: y }] })
      pane.dispatchEvent(start)

      const end = new Event('touchend', { bubbles: true })
      Object.defineProperty(end, 'changedTouches', { value: [{ clientX: toX, clientY: y }] })
      pane.dispatchEvent(end)
    },
    { fromX, toX, y },
  )
}

test('narrow viewport: horizontal swipe steps focus like j/k', async ({ page }) => {
  await page.goto('/')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(MOBILE_SWIPE_FIXTURE_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  await expect(page.locator('.entry-pane')).toBeVisible({ timeout: 10_000 })
  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(3)
  await expect(entries.nth(0)).toHaveClass(/focused/)

  // A swipe starting within the edge-exclusion zone (see EDGE_EXCLUSION_PX
  // in useSwipeNavigation) is left alone for the OS back-gesture -- focus
  // must stay put.
  await swipe(page, 15, 200)
  await expect(entries.nth(0)).toHaveClass(/focused/)

  // Left swipe (positive-x start further than EDGE_EXCLUSION_PX from the
  // edge, moving left past MIN_SWIPE_PX) advances focus, same as 'j', and
  // marks the entry being left behind read.
  await swipe(page, 300, 150)
  await expect(entries.nth(0)).toHaveClass(/read/)
  await expect(entries.nth(1)).toHaveClass(/focused/)

  await swipe(page, 300, 150)
  await expect(entries.nth(2)).toHaveClass(/focused/)

  // Right swipe moves focus back, same as 'k'.
  await swipe(page, 150, 300)
  await expect(entries.nth(1)).toHaveClass(/focused/)

  // Unsubscribe: this suite shares one DB/sidebar across every spec (see
  // playwright.config.ts), and other tests assume a fixed set of feeds for
  // sidebar-adjacency ordering -- leaving this one-off subscription behind
  // would shift that order for whichever test runs next.
  page.once('dialog', (d) => void d.accept())
  await page.getByTitle('フィード詳細').click()
  await page.locator('.unsubscribe-button').click()
})
