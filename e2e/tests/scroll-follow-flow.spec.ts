import { expect, test } from '@playwright/test'

// Regression test for issue #37: j/k used to always step from the last
// index moveFocus() itself set (focusedIndex), ignoring any scrolling the
// reader did independently (mouse wheel, or useAutoMarkRead's own
// scroll-driven marking). After scrolling a few entries past the
// remembered focus with the wheel, j should act on whatever's now actually
// at the top of the pane, not the stale entry.
const SCROLL_FOLLOW_FEED_URL = 'http://127.0.0.1:18098/scroll-follow'

test('j follows the wheel-scrolled reading position, not a stale focusedIndex', async ({ page }) => {
  await page.goto('/')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(SCROLL_FOLLOW_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(10)
  await expect(entries.first()).toHaveClass(/focused/)

  // Scroll (independently of j/k) so the pane's reading position sits
  // exactly at the top of the 4th entry (index 3) -- offset by the sticky
  // header's height, the same way moveFocus itself aligns entries.
  await page.evaluate(() => {
    const pane = document.querySelector('.entry-pane')!
    const header = document.querySelector('.entry-header')!
    const target = document.querySelectorAll('.entry-item')[3] as HTMLElement
    const targetTop =
      target.getBoundingClientRect().top - pane.getBoundingClientRect().top + pane.scrollTop
    // +5px past the exact boundary so entry 2's bottom edge is unambiguously
    // above the reading position, not a rounding-distance tie with it.
    pane.scrollTo(0, targetTop - header.getBoundingClientRect().height + 5)
  })

  // focusedIndex itself hasn't moved -- entry 0 is still the CSS-focused
  // one, since nothing pressed j/k yet.
  await expect(entries.first()).toHaveClass(/focused/)

  // j should act on the 4th entry (now at the top of the pane), not
  // increment from the stale focusedIndex(0) to entry 1. (Entries 0-2 are
  // separately marked read by the scroll itself -- useAutoMarkRead's own
  // IntersectionObserver, unrelated to j -- so this doesn't check those.)
  await page.keyboard.press('j')
  await expect(entries.nth(3)).toHaveClass(/read/)
  await expect(entries.nth(4)).toHaveClass(/focused/)
  await expect(entries.nth(1)).not.toHaveClass(/focused/)
})
