import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// A single short entry fits the entry pane without any scrolling, so
// useAutoMarkRead's scroll/touchmove-driven paths never fire for it. Before
// this fix, switching straight to another feed (no scroll, no j/k) left such
// an entry unread forever even though the reader plainly saw all of it.
// selectAndLoadFeed/selectGroup now mark it read as part of the switch --
// see markVisibleEntriesRead in state/entries.ts.
const NO_SCROLL_FEED_A_URL = 'http://127.0.0.1:18098/no-scroll-fixture-a'
const NO_SCROLL_FEED_B_URL = 'http://127.0.0.1:18098/no-scroll-fixture-b'

test('switching feeds marks a fully-visible entry read even without any scrolling', async ({ page }) => {
  await page.goto('/')

  await subscribeFeed(page, NO_SCROLL_FEED_A_URL)

  await subscribeFeed(page, NO_SCROLL_FEED_B_URL)

  const subRowA = page.locator('.subscription-row', { hasText: 'No Scroll Fixture Feed A' })
  const subRowB = page.locator('.subscription-row', { hasText: 'No Scroll Fixture Feed B' })
  await expect(subRowA.locator('.unread-count')).toHaveText('1')
  await expect(subRowB.locator('.unread-count')).toHaveText('1')

  // Select feed A. Its lone entry fits on screen -- no scroll, no touch, no
  // j/k happens here.
  await subRowA.click()
  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(1)
  await expect(entries.first()).not.toHaveClass(/read/)

  // Switching away is the only thing that touches this entry.
  await subRowB.click()
  await expect(page.locator('.entry-item')).toHaveCount(1)

  await expect(subRowA.locator('.unread-count')).toHaveText('')

  await subRowA.click()
  await expect(page.locator('.entry-item').first()).toHaveClass(/read/)

  // This suite shares one DB/sidebar across every spec (see
  // playwright.config.ts), and other tests assume a fixed set of feeds for
  // sidebar-adjacency ordering -- leaving these behind would shift that
  // order for whichever test runs next. Currently on feed A's single-feed
  // view already.
  async function unsubscribe(): Promise<void> {
    page.once('dialog', (d) => void d.accept())
    await page.getByTitle('フィード詳細').click()
    await page.locator('.unsubscribe-button').click()
  }
  await unsubscribe()
  await subRowB.click()
  await unsubscribe()
})
