import { expect, test } from '@playwright/test'

// Covers Phase C's invitation piece of multi-user support
// (docs/multi-user-design.md's 招待トークン制 row): an admin issues an
// invite link through the admin panel, a brand-new browser context (no
// session at all) follows it, creates an account through the accept
// screen, and lands in the app already logged in. Also covers the token
// being single-use: following the same link again shows the
// expired/invalid message instead of a second signup form.
test('admin issues an invite, a new user accepts it and lands in the app', async ({
  page,
  browser,
}) => {
  await page.goto('/')

  await page.getByRole('button', { name: 'メニューを開く' }).click()
  await page.getByRole('button', { name: 'ユーザー管理' }).click()
  await page.waitForSelector('.admin-user-table')

  await page.getByRole('button', { name: '招待リンクを発行' }).click()
  const linkInput = page.locator('.dialog-panel input[readonly]')
  await expect(linkInput).toBeVisible()
  const inviteLink = await linkInput.inputValue()
  expect(inviteLink).toContain('/invite/')

  await page.getByRole('button', { name: '閉じる' }).click()

  // A completely logged-out browser context: explicit empty storageState
  // overrides the chromium project's default (see playwright.config.ts),
  // which browser.newContext() would otherwise inherit.
  const inviteeContext = await browser.newContext({
    storageState: { cookies: [], origins: [] },
  })
  const inviteePage = await inviteeContext.newPage()
  await inviteePage.goto(inviteLink)

  await expect(
    inviteePage.getByRole('heading', { name: 'feedla アカウント作成' }),
  ).toBeVisible()
  await inviteePage.getByPlaceholder('ユーザー名').fill('invite-flow-member')
  await inviteePage
    .getByPlaceholder('パスワード(12文字以上)')
    .fill('invite-flow-member-pw123')
  await inviteePage
    .getByPlaceholder('パスワード(確認)')
    .fill('invite-flow-member-pw123')
  await inviteePage.getByRole('button', { name: 'アカウントを作成' }).click()

  await expect(inviteePage.locator('.sidebar')).toBeVisible()

  // The token is single-use: revisiting the same link (a fresh page in the
  // same already-logged-in context still re-parses the URL on load) shows
  // the invalid/expired message, not another signup form.
  await inviteePage.goto(inviteLink)
  await expect(
    inviteePage.getByRole('heading', { name: '招待リンクが無効です' }),
  ).toBeVisible()

  await inviteeContext.close()
})
