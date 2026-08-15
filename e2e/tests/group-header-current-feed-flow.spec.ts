import { expect, request, test } from '@playwright/test'

// Regression coverage for the group-view header's "current feed" indicator:
// a folder/priority group mixes entries from many feeds, and once a long
// entry's body scrolls the per-entry feed link (see EntryItem) out of view,
// nothing on screen says whose article it is. Header.tsx now mirrors the
// focused entry's feed in the sticky header, so it stays visible for as
// long as that entry does.
const HEADER_FIXTURE_A_URL = 'http://127.0.0.1:18098/header-fixture-a'
const HEADER_FIXTURE_B_URL = 'http://127.0.0.1:18098/header-fixture-b'

test('group view header shows which feed the focused entry belongs to', async ({ page, baseURL }) => {
  // Isolate this test's two entries from the rest of the shared-suite DB
  // (see playwright.config.ts) by putting both fixture feeds in a
  // dedicated folder -- there's no UI for creating folders, so this goes
  // straight through the API the same way AddSubscriptionDialog does for
  // subscribing.
  const api = await request.newContext({ baseURL })
  const folderRes = await api.post('/api/v1/folders', {
    data: { name: 'Header Fixture Folder' },
  })
  const folder = await folderRes.json()

  await page.goto('/')

  async function subscribeInFolder(url: string): Promise<void> {
    await page.getByTitle('購読を追加').click()
    await page.getByPlaceholder('https://example.com/feed.xml').fill(url)
    await page.getByRole('button', { name: '追加' }).click()
  }

  await subscribeInFolder(HEADER_FIXTURE_A_URL)
  await expect(page.locator('.subscription-row', { hasText: 'Header Fixture Feed A' })).toBeVisible({
    timeout: 10_000,
  })
  await subscribeInFolder(HEADER_FIXTURE_B_URL)
  await expect(page.locator('.subscription-row', { hasText: 'Header Fixture Feed B' })).toBeVisible({
    timeout: 10_000,
  })

  const subsRes = await api.get('/api/v1/subscriptions')
  const { subscriptions } = (await subsRes.json()) as {
    subscriptions: { feed_id: number; title: string }[]
  }
  const feedA = subscriptions.find((s) => s.title === 'Header Fixture Feed A')!
  const feedB = subscriptions.find((s) => s.title === 'Header Fixture Feed B')!
  await api.patch(`/api/v1/subscriptions/${feedA.feed_id}`, {
    data: { folder_id: folder.id },
  })
  await api.patch(`/api/v1/subscriptions/${feedB.feed_id}`, {
    data: { folder_id: folder.id },
  })

  // Reload so the sidebar picks up the folder assignment made via the API.
  await page.reload()
  // getByRole, not getByText: the feed detail overlay's カテゴリ select also
  // lists every folder name as an <option>, so a bare text match is
  // ambiguous once that folder has any subscription to open the detail
  // overlay for (see feed-detail-move-folder-flow.spec.ts).
  await page.getByRole('button', { name: 'Header Fixture Folder' }).click()

  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(2)
  await expect(entries.nth(0)).toHaveClass(/focused/)

  // loadGroupEntries orders unread entries newest-first, so Feed B's entry
  // (later pubDate) is focused first.
  await expect(page.locator('.entry-header-current-feed')).toHaveText('Header Fixture Feed B')

  await page.keyboard.press('j')
  await expect(entries.nth(1)).toHaveClass(/focused/)
  await expect(page.locator('.entry-header-current-feed')).toHaveText('Header Fixture Feed A')

  // Clicking the header's current-feed link jumps straight to that feed's
  // single-feed view (same action as clicking the per-entry feed link).
  await page.locator('.entry-header-current-feed').click()
  await expect(page.locator('.entry-header-title')).toHaveText('Header Fixture Feed A')

  // Unsubscribe both: this suite shares one DB/sidebar across every spec
  // (see playwright.config.ts), and other tests assume a fixed set of feeds
  // for sidebar-adjacency ordering -- leaving these behind would shift that
  // order for whichever test runs next. The now-empty folder itself is left
  // in place (no delete-folder API exists), but SubscriptionTree only
  // renders folders that still have subscriptions, so it won't show up or
  // affect other tests. "フィード詳細" only appears in the single-feed
  // header (see Header.tsx), so switch there first via the entry list's
  // per-entry feed link -- still on Feed A's single-feed view from above.
  page.once('dialog', (d) => void d.accept())
  await page.getByTitle('フィード詳細').click()
  await page.locator('.unsubscribe-button').click()

  await page.getByRole('button', { name: 'Header Fixture Folder' }).click()
  await expect(page.locator('.entry-item')).toHaveCount(1)
  await page.locator('.entry-header-current-feed').click()
  await expect(page.locator('.entry-header-title')).toHaveText('Header Fixture Feed B')
  page.once('dialog', (d) => void d.accept())
  await page.getByTitle('フィード詳細').click()
  await page.locator('.unsubscribe-button').click()
})
