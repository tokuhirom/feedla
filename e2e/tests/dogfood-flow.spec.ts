import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// This mirrors the README's Phase4 dogfooding completion criterion: subscribe
// to a feed, read its unread entries with j/k, and use the rest of the
// keyboard shortcuts -- all against the real feedla binary + a real (fixture)
// feed server, not mocks.
const FIXTURE_FEED_URL = 'http://127.0.0.1:18098/'

test('subscribe, read unread entries, and use keyboard shortcuts', async ({ page }) => {
  await page.goto('/')
  // Shared DB across specs (see playwright.config.ts) means unread entries
  // from earlier tests may already be present, prefixing the title with
  // "(N) - " (see subscriptions.ts's document.title updates).
  await expect(page).toHaveTitle(/feedla$/)

  await subscribeFeed(page, FIXTURE_FEED_URL)

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

  // p pins the focused entry (Phase5) and shows a confirmation toast.
  await page.keyboard.press('p')
  await expect(page.locator('.toast')).toContainText('pin')
  await expect(page.locator('.entry-item.focused .pin-star')).toBeVisible()

  // v opens the focused entry in a new tab.
  const [popup] = await Promise.all([page.waitForEvent('popup'), page.keyboard.press('v')])
  await popup.waitForLoadState()
  expect(popup.url()).toContain('127.0.0.1:18098')
  await popup.close()

  // r asks the server to re-crawl; with no new entries this should be a
  // no-op that doesn't error or change the unread count.
  await page.keyboard.press('r')
  await expect(subRow.locator('.unread-count')).toHaveText('1')

  // The "クロール状況" menu item (under the sidebar header's ⋮ menu)
  // surfaces GET /api/v1/stats -- confirm it reflects the feed just
  // subscribed to.
  await page.getByLabel('メニューを開く').click()
  await page.getByText('クロール状況').click()
  const statsPanel = page.locator('.help-panel', { hasText: 'クロール状況' })
  await expect(statsPanel).toContainText('購読フィード数')
  await expect(statsPanel.locator('dd').first()).toHaveText('1')
  await statsPanel.getByText('閉じる').click()

  // Clicking a header rating star sets it (PATCH /api/v1/subscriptions/{id}),
  // and clicking the same star again clears it back to unrated.
  const stars = page.locator('.rating-star')
  await stars.nth(2).click() // 3rd star
  await expect(stars).toHaveText(['★', '★', '★', '☆', '☆'])
  await stars.nth(2).click() // same star again -> clears
  await expect(stars).toHaveText(['☆', '☆', '☆', '☆', '☆'])
})
