import { expect, test } from '@playwright/test'

const FIXTURE_FEED_URL = 'http://127.0.0.1:18098/'

test('reload while the debounced mark-read POST is still in flight loses the read state', async ({ page }) => {
  await page.goto('/')
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(FIXTURE_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const subRow = page.locator('.subscription-row', { hasText: 'E2E Fixture Feed' })
  await expect(subRow).toBeVisible({ timeout: 10_000 })
  await expect(subRow.locator('.unread-count')).toHaveText('2')

  // The e2e testserver holds POST /api/v1/entries/read open for 3s
  // (FEEDLA_E2E_DELAY_MARK_READ_MS in playwright.config.ts), so we can
  // reload while the debounced bulk-read POST is still genuinely in flight
  // over the wire -- mimicking a real user closing/reloading the tab just
  // after the 2s idle timer fires the request but before a slow connection
  // has answered it. Deliberately not using page.route() here: Playwright's
  // route interception ties the request to the page's frame and cancels it
  // on navigation regardless of fetch keepalive, so it can't be used to
  // test keepalive semantics faithfully.

  await subRow.click()
  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)

  await page.keyboard.press('j')
  await expect(entries.first()).toHaveClass(/read/)

  // Wait past the 2s idle-flush timer so flushPendingReads() has started
  // (and cleared pendingReadIds) but is still awaiting the delayed response.
  await page.waitForTimeout(2200)

  await page.reload()

  // The reload's own initial fetch races the still-in-flight (delayed)
  // mark-read POST, so it's expected to still see the pre-mark-read count
  // here regardless of keepalive -- keepalive only lets the request
  // complete in the background, it doesn't block navigation on it.
  const subRow2 = page.locator('.subscription-row', { hasText: 'E2E Fixture Feed' })
  await expect(subRow2.locator('.unread-count')).toHaveText('2')

  // Wait past the server's 3s delay so the (keepalive) request has had a
  // chance to actually complete server-side, then reload again for a fresh
  // fetch. Without keepalive, navigation aborted the request and
  // pendingReadIds was already cleared before it started, so the read never
  // reaches the server and the entry stays unread forever. With keepalive,
  // the request survives the earlier reload and completes in the
  // background, so this second, fresh fetch sees it.
  await page.waitForTimeout(3500)
  await page.reload()
  const subRow3 = page.locator('.subscription-row', { hasText: 'E2E Fixture Feed' })
  await expect(subRow3.locator('.unread-count')).toHaveText('1')
})
