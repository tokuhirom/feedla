import { type Page, expect, test } from '@playwright/test'

// Waits for the currently-displayed entry's (deliberately tall, see
// feed-server.mjs's longBody) body to actually finish laying out before the
// caller navigates away from it. .entry-item uses content-visibility: auto
// (see EntryItem.tsx), so its true height isn't known until the browser has
// laid it out -- racing that would let selectAndLoadFeed's mark-on-leave
// (markVisibleEntriesRead) see a not-yet-measured, seemingly-small entry
// and wrongly mark it read as "fully visible", which -- now that s/a
// (adjacentFeedId) skip zero-unread feeds -- would make a subsequent s/a/
// shift+j wrongly skip past it.
async function waitForTallEntryLaidOut(page: Page): Promise<void> {
  await expect(async () => {
    const bodyBox = await page.locator('.entry-body').first().boundingBox()
    const paneBox = await page.locator('.entry-pane').boundingBox()
    expect(bodyBox).not.toBeNull()
    expect(paneBox).not.toBeNull()
    expect(bodyBox!.height).toBeGreaterThan(paneBox!.height)
  }).toPass({ timeout: 5_000 })
}

const FEED_A_URL = 'http://127.0.0.1:18098/shortcut-fixture-a'
const FEED_B_URL = 'http://127.0.0.1:18098/shortcut-fixture-b'
const MOBILE_FIXTURE_FEED_URL = 'http://127.0.0.1:18098/mobile-fixture'

test('+/- adjust the current feed rating, clamped to 0..5', async ({ page }) => {
  await page.goto('/')
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(FEED_A_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const subRow = page.locator('.subscription-row', { hasText: 'Shortcut Fixture Feed A' })
  await expect(subRow).toBeVisible({ timeout: 10_000 })
  await subRow.click()

  const stars = page.locator('.rating-star')
  await expect(stars).toHaveText(['☆', '☆', '☆', '☆', '☆'])

  for (let i = 0; i < 5; i++) await page.keyboard.press('+')
  await expect(stars).toHaveText(['★', '★', '★', '★', '★'])

  // Clamped at 5 -- one more + must not error or overflow the display.
  await page.keyboard.press('+')
  await expect(stars).toHaveText(['★', '★', '★', '★', '★'])

  await page.keyboard.press('-')
  await expect(stars).toHaveText(['★', '★', '★', '★', '☆'])

  for (let i = 0; i < 5; i++) await page.keyboard.press('-')
  // Clamped at 0.
  await expect(stars).toHaveText(['☆', '☆', '☆', '☆', '☆'])
})

test('shift+j behaves like j until the last entry, then moves to the next feed like s', async ({ page }) => {
  await page.goto('/')

  // Feed A is already subscribed by the previous test in this shared-DB
  // suite (see playwright.config.ts) -- only B needs subscribing here.
  // Re-submitting A's URL would add a second client-side row instead of
  // reusing the existing one (a separate, pre-existing quirk of the "add
  // subscription" dialog, unrelated to what this test covers).
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(FEED_B_URL)
  await page.getByRole('button', { name: '追加' }).click()
  await expect(page.locator('.subscription-row', { hasText: 'Shortcut Fixture Feed B' })).toBeVisible({
    timeout: 10_000,
  })

  await page.locator('.subscription-row', { hasText: 'Shortcut Fixture Feed A' }).click()
  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)
  await expect(entries.first()).toHaveClass(/focused/)

  // Not at the last entry yet -- shift+j behaves like plain j.
  await page.keyboard.press('J')
  await expect(entries.first()).toHaveClass(/read/)
  await expect(entries.nth(1)).toHaveClass(/focused/)
  await expect(page.locator('.entry-header-title')).toContainText('Shortcut Fixture Feed A')

  // Now focused on the last entry -- shift+j acts like s, moving to the
  // next feed as displayed instead of being a no-op.
  await page.keyboard.press('J')
  await expect(page.locator('.entry-header-title')).toContainText('Shortcut Fixture Feed B')
})

test('s/a and shift+j skip over feeds with no unread entries left', async ({ page }) => {
  await page.goto('/')

  async function subscribe(url: string): Promise<void> {
    await page.getByTitle('購読を追加').click()
    await page.getByPlaceholder('https://example.com/feed.xml').fill(url)
    await page.getByRole('button', { name: '追加' }).click()
  }

  await subscribe('http://127.0.0.1:18098/unread-skip-fixture-a')
  await expect(page.locator('.subscription-row', { hasText: 'Zzz Unread Skip Feed A' })).toBeVisible({
    timeout: 10_000,
  })
  await subscribe('http://127.0.0.1:18098/unread-skip-fixture-b')
  await expect(page.locator('.subscription-row', { hasText: 'Zzz Unread Skip Feed B' })).toBeVisible({
    timeout: 10_000,
  })
  await subscribe('http://127.0.0.1:18098/unread-skip-fixture-c')
  await expect(page.locator('.subscription-row', { hasText: 'Zzz Unread Skip Feed C' })).toBeVisible({
    timeout: 10_000,
  })

  await page.getByText('カテゴリ').click()

  // Confirm display order (A, B, C -- see feed-server.mjs's doc comment on
  // their shared pubDate/alphabetical-title trick for why this is
  // guaranteed even with other specs' feeds sharing this suite's DB) before
  // relying on it.
  const rows = page.locator('.subscription-row', { hasText: /Zzz Unread Skip Feed/ })
  await expect(rows).toHaveCount(3)
  await expect(rows.nth(0)).toContainText('Zzz Unread Skip Feed A')
  await expect(rows.nth(1)).toContainText('Zzz Unread Skip Feed B')
  await expect(rows.nth(2)).toContainText('Zzz Unread Skip Feed C')

  // Read B's only entry so it drops to zero unread, without touching A or
  // C.
  await rows.nth(1).click()
  await expect(page.locator('.entry-header-title')).toContainText('Zzz Unread Skip Feed B')
  await waitForTallEntryLaidOut(page) // ensures the entry has actually loaded, not just the header
  await page.keyboard.press('j')
  await expect(page.locator('.entry-item').first()).toHaveClass(/read/)

  // From A, s must skip the now-read B and land straight on C -- not on B,
  // which is what adjacentFeedId did before it became unread-aware.
  await rows.nth(0).click()
  await expect(page.locator('.entry-header-title')).toContainText('Zzz Unread Skip Feed A')
  await waitForTallEntryLaidOut(page) // see helper doc comment above
  await page.keyboard.press('s')
  await expect(page.locator('.entry-header-title')).toContainText('Zzz Unread Skip Feed C')
  await waitForTallEntryLaidOut(page)

  // Symmetrically, a from C must skip back over B and land on A.
  await page.keyboard.press('a')
  await expect(page.locator('.entry-header-title')).toContainText('Zzz Unread Skip Feed A')

  // shift+j at the last (only) entry of A follows the same "next feed" path
  // as s, so it must skip B too.
  await page.keyboard.press('J')
  await expect(page.locator('.entry-header-title')).toContainText('Zzz Unread Skip Feed C')
})

test('j scrolls the next entry title into view below the sticky header', async ({ page }) => {
  await page.goto('/')

  // Reuses mobile-flow.spec.ts's fixture for its deliberately tall first
  // item -- moving from entry 1 to entry 2 via j requires a real scroll,
  // which is what exposes the sticky .entry-header covering the target
  // entry's title (moveFocus previously used scrollIntoView({block:
  // 'start'}) with no offset for the header's height).
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(MOBILE_FIXTURE_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)
  await expect(entries.first()).toHaveClass(/focused/)

  await page.keyboard.press('j')
  await expect(entries.nth(1)).toHaveClass(/focused/)

  const secondTitle = entries.nth(1).locator('.entry-title')
  const header = page.locator('.entry-header')
  await expect(async () => {
    const titleBox = await secondTitle.boundingBox()
    const headerBox = await header.boundingBox()
    expect(titleBox).not.toBeNull()
    expect(headerBox).not.toBeNull()
    // The title's top must be at or below the header's bottom edge, not
    // hidden behind it -- waits out the smooth scroll's settling time.
    expect(titleBox!.y).toBeGreaterThanOrEqual(headerBox!.y + headerBox!.height - 1)
  }).toPass({ timeout: 5_000 })
})
