import { expect, test } from '@playwright/test'

// Triaging a pile of dead feeds one 購読解除 confirm at a time doesn't scale
// (feature request after issue #99). The ⚠エラーのみ view in フィード管理
// gained URL/エラーメッセージ/エラー回数 filters, per-row checkboxes, and a
// single-confirm bulk unsubscribe action -- this covers all three filters
// narrowing the selection and the bulk action actually unsubscribing.
test('エラーフィードをフィルタで絞り込んで一括購読解除できる', async ({ page }) => {
  await page.goto('/')

  // /flaky-<suffix> 404s on its second request (see feed-server.mjs), so
  // error_count is already 1 right after subscribing. The "bulk-unsub-"
  // prefix keeps this test's rows distinguishable from other specs' own
  // flaky feeds sharing the same DB (see playwright.config.ts).
  const suffixes = ['bulk-unsub-a', 'bulk-unsub-b', 'bulk-unsub-c']
  for (const suffix of suffixes) {
    await page.getByTitle('購読を追加').click()
    await page
      .getByPlaceholder('https://example.com/feed.xml')
      .fill(`http://127.0.0.1:18098/flaky-${suffix}`)
    await page.getByRole('button', { name: '追加' }).click()
    await expect(
      page.locator('.subscription-row', { hasText: `Flaky Feed ${suffix}` }),
    ).toBeVisible({ timeout: 10_000 })

    // The sidebar/フィード管理 only treat a feed as "erroring" once it fails
    // ERRORING_THRESHOLD (3) times in a row, so force two more failed
    // re-crawls via this feed's entry header (subscribing auto-selects it).
    const refreshButton = page.getByTitle('再クロール (r)')
    for (let j = 0; j < 2; j++) {
      await Promise.all([
        page.waitForResponse(
          (resp) => resp.url().includes('/refresh') && resp.request().method() === 'POST',
        ),
        refreshButton.click(),
      ])
    }
  }

  const errorBadge = page.locator('.error-badge')
  await expect(errorBadge).toHaveText(/⚠ \d+/, { timeout: 10_000 })
  await errorBadge.click()

  const list = page.locator('.error-feed-list li')
  const urlFilter = page.locator('input[placeholder="URL部分一致"]')
  const errorFilter = page.locator('input[placeholder="エラーメッセージ部分一致"]')
  const countFilter = page.locator('input[placeholder="エラー回数以上"]')

  // URL partial match scopes down to just this test's three rows.
  await urlFilter.fill('bulk-unsub-')
  await expect(list).toHaveCount(3)

  // Error message partial match: every /flaky-* 404 shares the same crawler
  // message, so filtering for it keeps all three; an unrelated needle
  // filters all three out.
  await errorFilter.fill('404')
  await expect(list).toHaveCount(3)
  await errorFilter.fill('no such message')
  await expect(list).toHaveCount(0)
  await errorFilter.fill('')

  // Error-count threshold: all three are already at error_count 3 (the
  // erroring-list threshold itself); recrawl one feed an extra time (still
  // 404s) to bump it past the other two, then isolate it with a >=4 filter.
  const rowA = list.filter({ hasText: 'Flaky Feed bulk-unsub-a' })
  await rowA.getByRole('button', { name: '再クロール' }).click()
  await expect(rowA).toContainText('4 回連続失敗', { timeout: 10_000 })

  await countFilter.fill('4')
  await expect(list).toHaveCount(1)
  await expect(list).toContainText('Flaky Feed bulk-unsub-a')
  await countFilter.fill('')
  await expect(list).toHaveCount(3)

  // Selecting all currently-filtered rows and confirming once unsubscribes
  // every one of them, not just the first.
  await page.locator('.feed-manager-select-all input').click()
  await expect(page.locator('.feed-manager-bulk-bar')).toContainText(
    '3 件選択中',
  )

  page.once('dialog', (d) => d.accept())
  await page.getByRole('button', { name: /一括購読解除/ }).click()
  await expect(page.locator('.toast')).toContainText('3 件を購読解除しました')
  await expect(list).toHaveCount(0)

  // Fully gone from the sidebar too, not just filtered out of view.
  await urlFilter.fill('')
  await expect(
    page.locator('.subscription-row', { hasText: 'bulk-unsub-' }),
  ).toHaveCount(0)
})
