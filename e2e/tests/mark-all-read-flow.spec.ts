import { type Locator, expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

const MARK_ALL_READ_FEED_A_URL = 'http://127.0.0.1:18098/mark-all-read-fixture-a'
const MARK_ALL_READ_FEED_B_URL = 'http://127.0.0.1:18098/mark-all-read-fixture-b'

// The Sidebar ⋮ menu's "すべて既読にする" bulk action -- unlike
// FeedDetailOverlay's 全て既読にする button (which only touches the currently
// open feed), this must clear unread_count across every subscribed feed at
// once via a single confirm.
test('サイドバーメニューからすべての未読を一括で既読にできる', async ({ page }) => {
  await page.goto('/')

  await subscribeFeed(page, MARK_ALL_READ_FEED_A_URL)

  await subscribeFeed(page, MARK_ALL_READ_FEED_B_URL)

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
  const dropdown = page.locator('.header-menu-dropdown')
  await expect(page.getByRole('button', { name: 'すべて既読にする' })).toBeDisabled()
  // Close via an outside click (Sidebar's onPointerDown handler) rather than
  // Escape -- in CI, Escape sent via page.keyboard.press landed before (or
  // without reaching) the document-level keydown listener Sidebar registers
  // only while menuOpen is true, leaving the dropdown open and blocking the
  // subscription-row click below with a "フィード管理 button intercepts
  // pointer events" timeout.
  await page.getByText('feedla', { exact: true }).click()
  await expect(dropdown).toBeHidden()

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
