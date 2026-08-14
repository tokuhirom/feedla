import { expect, test } from '@playwright/test'

// This mirrors the README's Phase4 dogfooding completion criterion: subscribe
// to a feed, read its unread entries with j/k, and use the rest of the
// keyboard shortcuts -- all against the real feedla binary + a real (fixture)
// feed server, not mocks.
const FIXTURE_FEED_URL = 'http://127.0.0.1:18098/'

test('subscribe, read unread entries, and use keyboard shortcuts', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveTitle('feedla')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(FIXTURE_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const subRow = page.locator('.subscription-row', { hasText: 'E2E Fixture Feed' })
  await expect(subRow).toBeVisible({ timeout: 10_000 })
  await expect(subRow.locator('.unread-count')).toHaveText('2')

  await subRow.click()
  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)
  await expect(entries.first()).toHaveClass(/focused/)
  await expect(entries.first()).not.toHaveClass(/read/)

  // j marks the entry being left behind as read and moves focus forward.
  await page.keyboard.press('j')
  await expect(entries.first()).toHaveClass(/read/)
  await expect(entries.nth(1)).toHaveClass(/focused/)

  // The debounced bulk read POST should land within its max-flush window.
  await expect(subRow.locator('.unread-count')).toHaveText('1', { timeout: 10_000 })

  // k moves focus back without affecting read state further.
  await page.keyboard.press('k')
  await expect(entries.first()).toHaveClass(/focused/)

  // ? toggles the help overlay.
  await page.keyboard.press('?')
  await expect(page.locator('.help-overlay')).toBeVisible()
  await page.keyboard.press('?')
  await expect(page.locator('.help-overlay')).toBeHidden()

  // p has no backend yet (Phase5): pressing it should surface a toast, not
  // silently do nothing.
  await page.keyboard.press('p')
  await expect(page.locator('.toast')).toContainText('Phase 5')

  // v opens the focused entry in a new tab.
  const [popup] = await Promise.all([page.waitForEvent('popup'), page.keyboard.press('v')])
  await popup.waitForLoadState()
  expect(popup.url()).toContain('127.0.0.1:18098')
  await popup.close()

  // r asks the server to re-crawl; with no new entries this should be a
  // no-op that doesn't error or change the unread count.
  await page.keyboard.press('r')
  await expect(subRow.locator('.unread-count')).toHaveText('1')
})
