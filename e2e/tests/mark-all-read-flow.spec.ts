import { type Locator, expect, test } from '@playwright/test'

const MARK_ALL_READ_FEED_A_URL = 'http://127.0.0.1:18098/mark-all-read-fixture-a'
const MARK_ALL_READ_FEED_B_URL = 'http://127.0.0.1:18098/mark-all-read-fixture-b'

// The Sidebar ⋮ menu's "すべて既読にする" bulk action -- unlike
// FeedDetailOverlay's 全て既読にする button (which only touches the currently
// open feed), this must clear unread_count across every subscribed feed at
// once via a single confirm.
test('サイドバーメニューからすべての未読を一括で既読にできる', async ({ page }) => {
  await page.goto('/')

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(MARK_ALL_READ_FEED_A_URL)
  await page.getByRole('button', { name: '追加' }).click()

  await page.getByTitle('購読を追加').click()
  await page.getByPlaceholder('https://example.com/feed.xml').fill(MARK_ALL_READ_FEED_B_URL)
  await page.getByRole('button', { name: '追加' }).click()

  const subRowA = page.locator('.subscription-row', { hasText: 'Mark All Read Fixture Feed A' })
  const subRowB = page.locator('.subscription-row', { hasText: 'Mark All Read Fixture Feed B' })
  await expect(subRowA.locator('.unread-count')).toHaveText('1')
  await expect(subRowB.locator('.unread-count')).toHaveText('1')

  await page.getByLabel('メニューを開く').click()
  const markAllReadItem = page.getByRole('button', { name: 'すべて既読にする' })
  await expect(markAllReadItem).toBeEnabled()

  page.once('dialog', (d) => void d.accept())
  await markAllReadItem.click()

  await expect(subRowA.locator('.unread-count')).toHaveText('')
  await expect(subRowB.locator('.unread-count')).toHaveText('')

  // Nothing left unread -- the menu item disables itself rather than
  // offering a no-op confirm.
  await page.getByLabel('メニューを開く').click()
  await expect(page.getByRole('button', { name: 'すべて既読にする' })).toBeDisabled()
  await page.keyboard.press('Escape')

  // This suite shares one DB/sidebar across every spec (see
  // playwright.config.ts) -- leave both feeds unsubscribed again.
  async function unsubscribe(row: Locator): Promise<void> {
    await row.click()
    page.once('dialog', (d) => void d.accept())
    await page.getByTitle('フィード詳細').click()
    await page.locator('.unsubscribe-button').click()
  }
  await unsubscribe(subRowA)
  await unsubscribe(subRowB)
})
