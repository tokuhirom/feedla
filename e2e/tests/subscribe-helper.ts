import type { Page } from '@playwright/test'

// Subscribes to url via the normal AddSubscriptionDialog flow. Submitting
// the URL always comes back as a candidate list now (even for a single
// discovered feed -- see internal/api's handleCreateSubscription and
// AddSubscriptionDialog.tsx), with a synthetic "(本文抽出あり)" variant
// appended alongside the real candidate; this picks the plain one, matching
// what every e2e fixture feed expects (entries as delivered, no
// internal/fulltext extraction). Shared by every spec that just wants "a
// feed subscribed" -- pagewatch-flow.spec.ts drives the dialog manually
// instead, since it's specifically testing the no-feed-found (502) path.
export async function subscribeFeed(page: Page, url: string): Promise<void> {
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(url)
  await page.getByRole('button', { name: '追加' }).click()
  await page
    .locator('.candidate-list button')
    .filter({ hasNotText: '本文抽出あり' })
    .click()
}
