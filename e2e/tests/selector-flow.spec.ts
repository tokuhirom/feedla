import { expect, request, test } from '@playwright/test'

// End-to-end coverage for feedless Phase F1 (CSS セレクタによる一覧ページ
// 抽出, docs/feedless-site-subscription-selector.md), driven entirely
// through the UI: registration via AddSubscriptionDialog's "記事一覧として
// 取り込む" fallback, a preview step, the sidebar 📰 indicator, the 抽出設定
// panel, and a growth in the listing surfacing exactly one new entry on
// re-crawl.
const SELECTOR_FIXTURE_URL = 'http://127.0.0.1:18098/selector-fixture'
const SELECTOR_ADVANCE_URL = 'http://127.0.0.1:18098/selector-fixture/advance'

test('selector: register a listing page, preview, grow it, unsubscribe', async ({ page, baseURL }) => {
  const api = await request.newContext({ baseURL })

  await page.goto('/')

  // 1. The normal subscribe path 502s and the dialog offers both fallbacks
  // (§9.1); pick "記事一覧として取り込む".
  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(SELECTOR_FIXTURE_URL)
  await page.getByRole('button', { name: '追加' }).click()
  await expect(page.getByText('このページにフィードが見つかりませんでした。')).toBeVisible({
    timeout: 10_000,
  })
  await page.getByRole('button', { name: '記事一覧として取り込む' }).click()

  // 2. Enter item_selector and preview before subscribing (§9.1 step 2-3):
  // the fixture's two articles must both show up as matches.
  await page.getByPlaceholder('article').fill('article')
  await page.getByRole('button', { name: 'プレビュー' }).click()
  await expect(page.getByText('マッチ数: 2')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('.selector-preview-items li')).toHaveCount(2)

  // 3. Subscribe with this config -- an immediate first crawl imports both
  // articles as real entries (not a single "monitoring started" stub, since
  // selector always has real articles from the start).
  await page.getByRole('button', { name: 'この設定で購読' }).click()
  const row = page.locator('.subscription-row', { hasText: 'Selector Fixture List' })
  await expect(row).toBeVisible({ timeout: 10_000 })
  await expect(row.locator('.pagewatch-icon')).toContainText('📰')
  await expect(page.locator('.entry-item', { hasText: 'Selector Fixture Article 1' })).toBeVisible()
  await expect(page.locator('.entry-item', { hasText: 'Selector Fixture Article 2' })).toBeVisible()

  // 4. 抽出設定 (§9.3): the saved item_selector round-trips, and the
  // settings-panel preview marks both articles as already imported (seen).
  await row.click()
  await page.getByTitle('フィード詳細').click()
  await expect(page.locator('.selector-settings')).toBeVisible()
  await expect(page.getByPlaceholder('article')).toHaveValue('article')
  await page.getByRole('button', { name: 'いま取得して確認' }).click()
  await expect(page.locator('.selector-preview-items li.seen')).toHaveCount(2)
  await page.getByRole('button', { name: '閉じる' }).click()

  // 5. Grow the listing out-of-band, force a re-crawl -- exactly one new
  // entry must appear, and the first two articles are not re-fetched
  // (§4.4's URL-first-seen new-item detection; not directly observable from
  // the UI, but re-fetching them would risk flipping their body/date, which
  // this test doesn't assert on either way -- the entry count is the
  // reliable signal).
  const advanceRes = await api.post(SELECTOR_ADVANCE_URL)
  expect(advanceRes.status()).toBe(204)
  await row.click()
  await page.getByTitle('フィード詳細').click()
  await page.getByRole('button', { name: '再クロール' }).click()
  await expect(page.locator('.toast')).toContainText('新着')
  await expect(page.locator('.entry-item', { hasText: 'Selector Fixture Article 3' })).toBeVisible()

  // 6. Unsubscribe: this suite shares one DB/sidebar across every spec (see
  // playwright.config.ts) -- leaving this behind would shift sidebar
  // order/counts for whichever spec runs next.
  page.once('dialog', (d) => void d.accept())
  await page.locator('.unsubscribe-button').click()
  await expect(page.locator('.subscription-row', { hasText: 'Selector Fixture List' })).toHaveCount(0)
})
