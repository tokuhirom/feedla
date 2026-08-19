import { expect, test } from '@playwright/test'

// Covers Phase C's admin-panel piece of multi-user support
// (docs/multi-user-design.md's admin-画面 row): an admin creates a new
// account through the UI, the new (non-admin) account can't see the admin
// menu item, promoting them through the UI grants it, and disabling them
// through the UI kills their live session immediately (not just future
// logins) -- see internal/store/users.go's SetUserDisabled.
const MEMBER_USERNAME = 'admin-flow-member'
const MEMBER_PASSWORD = 'admin-flow-member-pw123'

test('admin creates, promotes, and disables a user through the admin panel', async ({
  page,
  browser,
}) => {
  await page.goto('/')

  await page.getByRole('button', { name: 'メニューを開く' }).click()
  await page.getByRole('button', { name: '管理者用ツール' }).click()
  await page.waitForSelector('.admin-user-table')

  await page.getByPlaceholder('ユーザー名').fill(MEMBER_USERNAME)
  await page
    .getByPlaceholder('パスワード(12文字以上)')
    .fill(MEMBER_PASSWORD)
  await page.getByRole('button', { name: '作成' }).click()

  const memberRow = page.locator('tr', { hasText: MEMBER_USERNAME })
  await expect(memberRow).toBeVisible()
  await expect(memberRow).toContainText('一般')

  await page.getByRole('button', { name: '閉じる' }).click()

  // A second, independently logged-in browser context: the new account
  // shouldn't see the admin menu item at all. Explicit empty storageState
  // overrides the chromium project's default (see playwright.config.ts),
  // which browser.newContext() would otherwise inherit -- logging this
  // context in as e2e-admin instead of starting it logged out.
  const memberContext = await browser.newContext({
    storageState: { cookies: [], origins: [] },
  })
  const memberPage = await memberContext.newPage()
  await memberPage.goto('/')
  await memberPage.getByPlaceholder('ユーザー名').fill(MEMBER_USERNAME)
  await memberPage.getByPlaceholder('パスワード').fill(MEMBER_PASSWORD)
  await memberPage.getByRole('button', { name: 'ログイン' }).click()
  await expect(memberPage.locator('.sidebar')).toBeVisible()
  await memberPage.getByRole('button', { name: 'メニューを開く' }).click()
  await expect(
    memberPage.getByRole('button', { name: '管理者用ツール' }),
  ).toHaveCount(0)
  await memberPage.keyboard.press('Escape')

  // Back as admin: promote, then disable.
  await page.getByRole('button', { name: 'メニューを開く' }).click()
  await page.getByRole('button', { name: '管理者用ツール' }).click()
  await memberRow.getByRole('button', { name: '管理者にする' }).click()
  await expect(memberRow).toContainText('管理者')

  await memberRow.getByRole('button', { name: '無効化' }).click()
  await expect(memberRow).toContainText('無効')

  // Disabling kills the member's live session immediately, not just
  // future logins -- reloading their already-open page must bounce them
  // to the login screen.
  await memberPage.reload()
  await expect(
    memberPage.getByRole('heading', { name: 'feedla ログイン' }),
  ).toBeVisible()

  await memberContext.close()
  await page.getByRole('button', { name: '閉じる' }).click()
})
