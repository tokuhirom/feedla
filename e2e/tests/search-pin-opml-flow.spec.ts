import { expect, test } from '@playwright/test'

// Phase5 completion criterion: search, pin, and OPML export/import all work
// against the real feedla binary + a real (fixture) feed server. Uses its
// own fixture path (see feed-server.mjs) so it doesn't collide with
// dogfood-flow.spec.ts's subscription in the shared DB (see
// playwright.config.ts's "tests share one feedla server/DB" comment).
const FIXTURE_FEED_URL = 'http://127.0.0.1:18098/search-fixture'

test('search, pin via overlay, and export/import OPML', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveTitle('feedla')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(FIXTURE_FEED_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const subRow = page.locator('.subscription-row', { hasText: 'Search Fixture Feed' })
  await expect(subRow).toBeVisible({ timeout: 10_000 })
  await subRow.click()

  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)

  // / opens the search overlay; searching for "Alpha" should find exactly
  // the matching article, and pinning it from the overlay should stick.
  await page.keyboard.press('/')
  const searchPanel = page.locator('.search-panel')
  await expect(searchPanel).toBeVisible()
  await searchPanel.getByPlaceholder('キーワード').fill('Alpha')
  await searchPanel.getByRole('button', { name: '検索' }).click()

  const searchResults = page.locator('.search-results li')
  await expect(searchResults).toHaveCount(1)
  await expect(searchResults.first()).toContainText('Search Alpha Item')

  await searchResults.first().getByRole('button', { name: 'pin' }).click()
  await expect(searchResults.first().getByRole('button', { name: 'pin解除' })).toBeVisible()
  await searchPanel.getByRole('button', { name: '閉じる' }).click()
  await expect(page.locator('.search-panel')).toBeHidden()

  // The pinned entry should show a star in the entry pane too.
  await expect(
    page.locator('.entry-item', { hasText: 'Search Alpha Item' }).locator('.pin-star'),
  ).toBeVisible()

  // o opens the pin overlay listing every pinned entry (pins are global,
  // not scoped to one feed -- dogfood-flow.spec.ts's own pin from the
  // shared DB may still be listed here too). Unpinning ours clears the
  // star back in the entry pane.
  await page.keyboard.press('o')
  const alphaPin = page.locator('.pin-list li', { hasText: 'Search Alpha Item' })
  await expect(alphaPin).toHaveCount(1)
  await alphaPin.getByRole('button', { name: '解除' }).click()
  await expect(page.locator('.pin-list li', { hasText: 'Search Alpha Item' })).toHaveCount(0)
  await page.getByRole('button', { name: '閉じる' }).click()

  await expect(
    page.locator('.entry-item', { hasText: 'Search Alpha Item' }).locator('.pin-star'),
  ).toBeHidden()

  // OPML export downloads a document listing the subscribed feed. It lives
  // under the sidebar header's ⋮ menu alongside the other sidebar-wide
  // actions.
  await page.getByLabel('メニューを開く').click()
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('link', { name: 'OPML export' }).click(),
  ])
  const downloadPath = await download.path()
  expect(downloadPath).toBeTruthy()
  const fs = await import('node:fs/promises')
  const opml = await fs.readFile(downloadPath!, 'utf-8')
  expect(opml).toContain('Search Fixture Feed')
  expect(opml).toContain(FIXTURE_FEED_URL)

  // OPML import re-uploads that same document; re-subscribing to an
  // already-known feed must not create a duplicate subscription row.
  const fileInput = page.locator('input[type="file"]')
  await fileInput.setInputFiles(downloadPath!)
  await expect(page.locator('.toast')).toContainText('インポート')
  await expect(page.locator('.subscription-row', { hasText: 'Search Fixture Feed' })).toHaveCount(1)
})
