import { expect, test } from '@playwright/test'

// Regression test for issue #39: the erroring-feeds list (now the sidebar's
// ⚠ badge opening フィード管理 pre-filtered to errors) showed only a title
// and error message, with no URL and no way to reach the feed's detail page
// from there.
const FLAKY_URL = 'http://127.0.0.1:18098/flaky-detail-link'

test('エラーのあるフィード一覧にURLが出て、詳細ページへ遷移できる', async ({ page }) => {
  await page.goto('/')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(FLAKY_URL)
  await page.getByRole('button', { name: '追加' }).click()
  await expect(page.locator('.subscription-row', { hasText: 'Flaky Feed detail-link' })).toBeVisible({
    timeout: 10_000,
  })

  const errorBadge = page.locator('.error-badge')
  await expect(errorBadge).toContainText('⚠ 1', { timeout: 10_000 })
  await errorBadge.click()

  const row = page.locator('.error-feed-list li')
  await expect(row).toHaveCount(1)
  await expect(row.locator('.error-feed-url')).toHaveText(FLAKY_URL)
  await expect(row.locator('.error-feed-message')).toContainText('unexpected status 404')

  // 再クロール forces an immediate re-crawl instead of waiting for the
  // scheduler's own retry interval; this fixture 404s every time, so it
  // surfaces the failure via toast rather than clearing the row.
  await row.getByRole('button', { name: '再クロール' }).click()
  await expect(page.locator('.toast')).toContainText('unexpected status 404')
  await expect(row).toHaveCount(1)

  await row.getByRole('button', { name: '詳細' }).click()

  // The error overlay is replaced by the feed detail screen for that feed.
  await expect(page.locator('.error-feed-list')).toHaveCount(0)
  await expect(page.locator('.feed-detail-list')).toBeVisible()
  await expect(page.locator('.help-panel h2')).toContainText('Flaky Feed detail-link')
  await expect(page.locator('.feed-detail-list')).toContainText(FLAKY_URL)
  await expect(page.locator('.feed-detail-list')).toContainText('unexpected status 404')
})
