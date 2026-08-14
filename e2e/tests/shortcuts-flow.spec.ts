import { expect, test } from '@playwright/test'

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
