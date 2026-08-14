import { expect, test } from '@playwright/test'

// Exercises the mobile (narrow-viewport) reading flow: single-pane
// navigation with a back button instead of the two-column desktop grid,
// and marking an entry read by scrolling past it -- there's no keyboard on
// a phone to drive the j/k flow the desktop dogfood-flow test covers.
test.use({ viewport: { width: 390, height: 844 } })

const MOBILE_FIXTURE_FEED_URL = 'http://127.0.0.1:18098/mobile-fixture'

test('narrow viewport: single-pane navigation and scroll-to-read', async ({ page }) => {
  await page.goto('/')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(MOBILE_FIXTURE_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  // Subscribing auto-selects the new feed (see AddSubscriptionDialog), so
  // by the time it's added we're already in the entry pane's single-pane
  // view -- below the mobile breakpoint the sidebar and entry pane are
  // mutually exclusive, switched by the back button.
  await expect(page.locator('.entry-pane')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('.sidebar')).toBeHidden()

  const subRow = page.locator('.subscription-row', { hasText: 'Mobile Fixture Feed' })
  await expect(subRow.locator('.unread-count')).toHaveText('2')

  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)
  await expect(entries.first()).not.toHaveClass(/read/)
  await expect(entries.last()).not.toHaveClass(/read/)

  // Scroll just past the (deliberately tall) first entry, stopping short of
  // the pane's true bottom -- measured from the DOM rather than a fixed
  // pixel guess, since the fixture's tall body doesn't render to a fixed
  // height. The IntersectionObserver in useAutoMarkRead should mark the
  // first entry read once its bottom edge has scrolled above the pane's
  // top edge, and leave the second (not yet reached) alone.
  await page.evaluate(() => {
    const el = document.querySelector('.entry-pane')!
    const first = document.querySelector<HTMLElement>('.entry-item')!
    // getBoundingClientRect() here is relative to the pane's current
    // (unscrolled) viewport, so combine it with the current scrollTop to
    // get the entry's absolute position within the scrollable content --
    // it doesn't start at 0 because the sticky header above it still
    // occupies real flow height.
    el.scrollTo(0, el.scrollTop + first.getBoundingClientRect().bottom + 50)
  })
  await expect(entries.first()).toHaveClass(/read/, { timeout: 5_000 })
  await expect(entries.last()).not.toHaveClass(/read/)

  // The debounced bulk read POST should land within its max-flush window.
  await expect(subRow.locator('.unread-count')).toHaveText('1', { timeout: 10_000 })

  // The last entry in the list can never scroll entirely past the pane's
  // top edge (there's nothing left below it to push it there), so the
  // IntersectionObserver above can't catch it -- reaching the actual
  // bottom of the pane is a separate signal useAutoMarkRead falls back to.
  await page.evaluate(() => {
    const el = document.querySelector('.entry-pane')!
    el.scrollTo(0, el.scrollHeight)
  })
  await expect(entries.last()).toHaveClass(/read/, { timeout: 5_000 })
  // unread-count renders blank (not "0") once a subscription has no
  // unread entries -- see SubscriptionTree.tsx.
  await expect(subRow.locator('.unread-count')).toHaveText('', { timeout: 10_000 })

  // The back button returns to the single-pane subscription list, showing
  // the unread count the scroll-triggered reads already brought down to 0.
  await page.locator('.back-button').click()
  await expect(page.locator('.sidebar')).toBeVisible()
  await expect(page.locator('.entry-pane')).toBeHidden()
  await expect(subRow).toBeVisible()
  await expect(subRow.locator('.unread-count')).toHaveText('')
})
