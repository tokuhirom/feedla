import { expect, test } from '@playwright/test'

// Regression test for issue #33: within a カテゴリ/プライオリティ group, feeds
// sort unread-first, then newest-last-entry-first, and that order is a
// snapshot -- it doesn't reshuffle just because unread_count changes, only
// when subscriptions are explicitly reloaded (e.g. the 'r' refresh shortcut,
// which also reloads the whole subscription list -- see refreshCurrentFeed).
const OLD_FEED_URL = 'http://127.0.0.1:18098/sort-fixture-old'
const NEW_FEED_URL = 'http://127.0.0.1:18098/sort-fixture-new'

test('sidebar sorts unread-first/newest-first and freezes order until reload', async ({ page }) => {
  await page.goto('/')

  async function subscribe(url: string): Promise<void> {
    await page.getByTitle('購読を追加').click()
    await page.getByPlaceholder('https://example.com/feed.xml').fill(url)
    await page.getByRole('button', { name: '追加' }).click()
  }

  // Subscribe Old first, New second -- subscribe order is the opposite of
  // the expected display order, so a pass here can't be an accident of
  // insertion order.
  await subscribe(OLD_FEED_URL)
  await expect(page.locator('.subscription-row', { hasText: 'Sort Fixture Feed Zulu (old)' })).toBeVisible({
    timeout: 10_000,
  })
  await subscribe(NEW_FEED_URL)
  await expect(page.locator('.subscription-row', { hasText: 'Sort Fixture Feed Alpha (new)' })).toBeVisible({
    timeout: 10_000,
  })

  const rows = page.locator('.subscription-row', { hasText: /Sort Fixture Feed/ })
  await expect(rows).toHaveCount(2)

  // Both feeds are unread -- newest last-entry (New) sorts first, even
  // though it was subscribed second and doesn't sort first alphabetically.
  await expect(rows.nth(0)).toContainText('Sort Fixture Feed Alpha (new)')
  await expect(rows.nth(1)).toContainText('Sort Fixture Feed Zulu (old)')

  // Read New's only entry (j marks the current entry read even when there's
  // nowhere left to move focus to -- see moveFocus). Waits for the debounced
  // mark-read POST to actually land server-side (not just the optimistic
  // client-side unread_count update) before continuing -- otherwise the 'r'
  // refresh below reloads subscriptions from the server while the read is
  // still only pending client-side, and unread_count bounces back to 1.
  await page.locator('.subscription-row', { hasText: 'Sort Fixture Feed Alpha (new)' }).click()
  await expect(page.locator('.entry-item')).toHaveCount(1)
  const readFlush = page.waitForResponse(
    (resp) => resp.url().includes('/api/v1/entries/read') && resp.request().method() === 'POST',
    { timeout: 10_000 },
  )
  await page.keyboard.press('j')
  await expect(
    page.locator('.subscription-row', { hasText: 'Sort Fixture Feed Alpha (new)' }).locator('.unread-count'),
  ).toHaveText('', { timeout: 10_000 })
  await readFlush

  // New no longer has unread entries, but the sidebar order must not have
  // reshuffled yet -- it's frozen until the next explicit reload.
  await expect(rows.nth(0)).toContainText('Sort Fixture Feed Alpha (new)')
  await expect(rows.nth(1)).toContainText('Sort Fixture Feed Zulu (old)')

  // 'r' re-crawls the current feed and reloads the whole subscription list
  // (see refreshCurrentFeed) -- that's the explicit reload that re-snapshots
  // the sort. Old (still unread) now sorts before New (read through).
  await page.keyboard.press('r')
  await expect(rows.nth(0)).toContainText('Sort Fixture Feed Zulu (old)', { timeout: 10_000 })
  await expect(rows.nth(1)).toContainText('Sort Fixture Feed Alpha (new)')
})
