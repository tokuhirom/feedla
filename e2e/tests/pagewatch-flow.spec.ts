import { expect, request, test } from '@playwright/test'

// End-to-end coverage for the feedless-page subscription feature
// (docs/feedless-site-subscription-pagewatch.md), driven entirely through
// the UI added in PR #121: registration via AddSubscriptionDialog's 502
// fallback, the sidebar 👁 indicator, the 監視設定 panel (watch mode,
// preview, ignore patterns), a real added-block diff surfacing from a
// second crawl, and the §9.4 "このブロックを無視する" recovery button.
const PAGEWATCH_FIXTURE_URL = 'http://127.0.0.1:18098/pagewatch-fixture'
const PAGEWATCH_ADVANCE_URL = 'http://127.0.0.1:18098/pagewatch-fixture/advance'

test('pagewatch: register a feedless page, watch it change, unsubscribe', async ({ page, baseURL }) => {
  const api = await request.newContext({ baseURL })

  await page.goto('/')

  // 1. The normal subscribe path 502s (no feed, no <link rel=alternate> at
  // this URL) and the dialog offers page-watch as a fallback (§9.1).
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(PAGEWATCH_FIXTURE_URL)
  await page.getByRole('button', { name: '追加' }).click()
  await expect(page.getByText('このページにフィードが見つかりませんでした。')).toBeVisible({
    timeout: 10_000,
  })

  // 2. Registering runs an immediate first crawl, establishing the
  // baseline -- the sidebar row appears with the 👁 indicator (§9.2) and
  // its title comes from the page's <title>, and a "監視を開始しました"
  // entry is created (not a diff, since there's nothing to diff against
  // yet).
  await page.getByRole('button', { name: 'ページの更新を監視する' }).click()
  const row = page.locator('.subscription-row', { hasText: 'Pagewatch Fixture Diary' })
  await expect(row).toBeVisible({ timeout: 10_000 })
  await expect(row.locator('.pagewatch-icon')).toBeVisible()
  await expect(page.locator('.entry-item', { hasText: '監視を開始しました' })).toBeVisible()

  // 3. 監視設定 (§9.3): defaults are "additions only" and no ignore patterns.
  await row.click()
  await page.getByTitle('フィード詳細').click()
  await expect(page.locator('.pagewatch-settings')).toBeVisible()
  await expect(page.getByRole('radio', { name: '新しく増えた内容だけ通知' })).toBeChecked()
  await expect(page.locator('.pagewatch-ignore-patterns .empty-state')).toBeVisible()

  // 4. Preview fetches the page right now, read-only, and shows its
  // current blocks -- v1 content at this point (still not advanced).
  await page.getByRole('button', { name: 'いま取得して確認' }).click()
  await expect(page.locator('.pagewatch-preview-blocks li').first()).toContainText(
    'Pagewatch Fixture First Post.',
  )

  // 5. Watch mode toggle persists server-side, not just optimistically:
  // survives a reload.
  await page.getByRole('radio', { name: '消えた内容も通知' }).check()
  await page.reload()
  await page.locator('.subscription-row', { hasText: 'Pagewatch Fixture Diary' }).click()
  await page.getByTitle('フィード詳細').click()
  await expect(page.getByRole('radio', { name: '消えた内容も通知' })).toBeChecked()
  // Switch back to the MVP default (additions only, §14.1) for the rest of
  // this flow.
  await page.getByRole('radio', { name: '新しく増えた内容だけ通知' }).check()
  await expect(page.getByRole('radio', { name: '新しく増えた内容だけ通知' })).toBeChecked()

  // 6. Mutate the watched page out-of-band, then force a re-crawl (the
  // overlay's 再クロール button, not waiting for the 1h scheduler) --
  // this must surface a diff entry with the newly-added block in <ins>.
  const advanceRes = await api.post(PAGEWATCH_ADVANCE_URL)
  expect(advanceRes.status()).toBe(204)
  await page.getByRole('button', { name: '再クロール' }).click()
  await expect(page.locator('.toast')).toContainText('新着')
  await page.getByRole('button', { name: '閉じる' }).click()

  const diffEntry = page.locator('.entry-item', { hasText: 'ブロック追加' })
  await expect(diffEntry).toBeVisible()
  await expect(diffEntry.locator('ins')).toContainText('Pagewatch Fixture Second Post Added.')

  // 7. §9.4's recovery button: ignoring the newly-added block adds a
  // regexp derived from its text to ignore_patterns.
  await diffEntry.locator('.pagewatch-ignore-btn').click()
  await expect(page.locator('.toast')).toContainText('このブロックを無視するように設定しました')

  await page.getByTitle('フィード詳細').click()
  await expect(page.locator('.pagewatch-ignore-patterns .pin-list code')).toContainText(
    'Pagewatch Fixture Second Post Added',
  )

  // 8. Unsubscribe: this suite shares one DB/sidebar across every spec
  // (see playwright.config.ts) -- leaving this behind would shift sidebar
  // order/counts for whichever spec runs next.
  page.once('dialog', (d) => void d.accept())
  await page.locator('.unsubscribe-button').click()
  await expect(page.locator('.subscription-row', { hasText: 'Pagewatch Fixture Diary' })).toHaveCount(0)
})
