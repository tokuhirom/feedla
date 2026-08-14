import { expect, test } from '@playwright/test'

// Regression test: s/a (adjacentFeedId) must step through feeds in the same
// order the sidebar actually displays them, not the flat API/subscribe
// order. Subscribing to "Two" before "One" makes those orders disagree
// under プライオリティ mode's alphabetical-within-rating sort (both feeds
// share the default ★評価なし rating here), which is exactly the mismatch
// that was reported. Titles share the "Zzz Nav Feed" prefix -- see
// fixtures/feed-server.mjs's doc comment for why that guarantees the two
// stay adjacent even with other specs' feeds mixed into the same shared DB.
const TWO_FEED_URL = 'http://127.0.0.1:18098/nav-fixture-zeta'
const ONE_FEED_URL = 'http://127.0.0.1:18098/nav-fixture-alpha'

test('s/a step through feeds in sidebar display order, not subscribe order', async ({ page }) => {
  await page.goto('/')

  async function subscribe(url: string): Promise<void> {
    await page.getByTitle('購読を追加').click()
    await page.getByPlaceholder('https://example.com/feed.xml').fill(url)
    await page.getByRole('button', { name: '追加' }).click()
  }

  await subscribe(TWO_FEED_URL)
  await expect(page.locator('.subscription-row', { hasText: 'Zzz Nav Feed Two' })).toBeVisible({ timeout: 10_000 })
  await subscribe(ONE_FEED_URL)
  await expect(page.locator('.subscription-row', { hasText: 'Zzz Nav Feed One' })).toBeVisible({ timeout: 10_000 })

  await page.getByText('プライオリティ').click()

  // Other specs in this shared-DB suite (see playwright.config.ts) may have
  // their own subscriptions mixed into the same list, so scope to just our
  // two "Zzz Nav Feed" rows and check their relative order rather than
  // assuming they're the only rows on the page.
  const rows = page.locator('.subscription-row', { hasText: /Zzz Nav Feed/ })
  await expect(rows).toHaveCount(2)

  // Displayed order within the shared ★評価なし group is alphabetical:
  // One, then Two -- the reverse of subscribe order.
  await expect(rows.nth(0)).toContainText('Zzz Nav Feed One')
  await expect(rows.nth(1)).toContainText('Zzz Nav Feed Two')

  await rows.nth(0).click() // select "One", the visually-first feed
  await expect(page.locator('.subscription-row.selected')).toContainText('Zzz Nav Feed One')

  // s from the first feed must move to the second as displayed ("Two"), not
  // do nothing (which is what happened when adjacentFeedId walked the flat
  // subscribe/feed_id order instead, where "One" was already last).
  await page.keyboard.press('s')
  await expect(page.locator('.subscription-row.selected')).toContainText('Zzz Nav Feed Two')

  // a moves back to "One".
  await page.keyboard.press('a')
  await expect(page.locator('.subscription-row.selected')).toContainText('Zzz Nav Feed One')

  // s/a must also follow whatever カテゴリ mode actually displays (both
  // feeds are unfiled, landing in a single "(未分類)" group in subscribe
  // order: Two, then One) rather than some other order -- guarding against
  // adjacentFeedId and SubscriptionTree drifting apart again, since both
  // now read from the same buildGroupsByFolder/buildGroupsByPriority.
  await page.getByText('カテゴリ').click()
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('Zzz Nav Feed Two')
  await expect(rows.nth(1)).toContainText('Zzz Nav Feed One')
  await rows.nth(0).click() // select "Two", the visually-first feed here
  await page.keyboard.press('s')
  await expect(page.locator('.subscription-row.selected')).toContainText('Zzz Nav Feed One')
})

test('sidebar view mode (カテゴリ/プライオリティ) persists across reload', async ({ page }) => {
  await page.goto('/')

  // Defaults to カテゴリ.
  await expect(page.getByText('カテゴリ')).toHaveClass(/active/)

  await page.getByText('プライオリティ').click()
  await expect(page.getByText('プライオリティ')).toHaveClass(/active/)

  await page.reload()
  await expect(page.getByText('プライオリティ')).toHaveClass(/active/)
  await expect(page.getByText('カテゴリ')).not.toHaveClass(/active/)

  await page.getByText('カテゴリ').click()
  await page.reload()
  await expect(page.getByText('カテゴリ')).toHaveClass(/active/)
})
