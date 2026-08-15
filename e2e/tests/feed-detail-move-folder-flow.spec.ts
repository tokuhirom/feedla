import { expect, request, test } from '@playwright/test'

// Regression coverage for issue #79: move a feed to a different folder from
// the feed detail overlay's カテゴリ select.
const MOVE_FOLDER_FIXTURE_URL = 'http://127.0.0.1:18098/move-folder-fixture'

test('feed detail overlay moves a feed between folders', async ({ page, baseURL }) => {
  // Isolate this test from the rest of the shared-suite DB (see
  // playwright.config.ts) with two dedicated test-only folders -- there's
  // no UI for creating folders, so this goes straight through the API the
  // same way group-header-current-feed-flow.spec.ts does.
  const api = await request.newContext({ baseURL })
  const folderARes = await api.post('/api/v1/folders', {
    data: { name: 'Move Folder Fixture A' },
  })
  const folderA = await folderARes.json()
  const folderBRes = await api.post('/api/v1/folders', {
    data: { name: 'Move Folder Fixture B' },
  })
  const folderB = await folderBRes.json()

  await page.goto('/')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(MOVE_FOLDER_FIXTURE_URL)
  await page.getByRole('button', { name: '追加' }).click()
  await expect(page.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' })).toBeVisible({
    timeout: 10_000,
  })

  const subsRes = await api.get('/api/v1/subscriptions')
  const { subscriptions } = (await subsRes.json()) as {
    subscriptions: { feed_id: number; title: string }[]
  }
  const feed = subscriptions.find((s) => s.title === 'Move Folder Fixture Feed')!
  await api.patch(`/api/v1/subscriptions/${feed.feed_id}`, {
    data: { folder_id: folderA.id },
  })

  // Reload so the sidebar picks up the folder assignment made via the API.
  await page.reload()
  const folderALi = page.locator('.subscription-tree > li').filter({ hasText: 'Move Folder Fixture A' })
  const folderBLi = page.locator('.subscription-tree > li').filter({ hasText: 'Move Folder Fixture B' })
  await expect(folderALi.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' })).toBeVisible()

  await folderALi.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' }).click()
  await page.getByTitle('フィード詳細').click()
  await expect(page.locator('.feed-detail-list')).toBeVisible()
  await expect(page.locator('.feed-detail-list select')).toHaveValue(String(folderA.id))

  // Moving to folder B via the カテゴリ select applies optimistically --
  // the row disappears from A and reappears under B without a reload.
  await page.locator('.feed-detail-list select').selectOption({ label: 'Move Folder Fixture B' })
  await expect(folderALi.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' })).toHaveCount(0)
  await expect(folderBLi.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' })).toBeVisible()

  // The change also survives a reload (persisted server-side, not just
  // reflected optimistically in the client's own state).
  await page.reload()
  await expect(folderBLi.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' })).toBeVisible()

  // Unsubscribe: this suite shares one DB/sidebar across every spec (see
  // playwright.config.ts), and other tests assume a fixed set of feeds for
  // sidebar-adjacency ordering -- leaving this behind would shift that
  // order for whichever test runs next. The now-empty folders are left in
  // place (no delete-folder API exists), but SubscriptionTree only renders
  // folders that still have subscriptions, so they won't show up or affect
  // other tests.
  await folderBLi.locator('.subscription-row', { hasText: 'Move Folder Fixture Feed' }).click()
  await page.getByTitle('フィード詳細').click()
  page.once('dialog', (d) => void d.accept())
  await page.locator('.unsubscribe-button').click()
})
