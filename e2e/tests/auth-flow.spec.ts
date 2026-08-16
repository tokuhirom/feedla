import { expect, test } from '@playwright/test'

// Runs with a clean, logged-out browser context instead of the 'chromium'
// project's shared storageState (see auth.setup.ts) -- this file is
// specifically about the login screen itself, so it needs to start
// unauthenticated rather than reuse the pre-logged-in session.
test.use({ storageState: { cookies: [], origins: [] } })

const E2E_USERNAME = 'e2e-admin'
const E2E_PASSWORD = 'e2e-test-password-12345'

test('unauthenticated visit shows the login screen, not the app', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'feedla ログイン' })).toBeVisible()
  await expect(page.locator('.sidebar')).toBeHidden()
})

test('wrong password is rejected and stays on the login screen', async ({ page }) => {
  await page.goto('/')
  // A distinct, made-up username rather than E2E_USERNAME: a failed
  // attempt trips that account's login rate limit (see
  // internal/auth/ratelimit.go), which would otherwise block the next
  // test's legitimate login for a couple of seconds.
  await page.getByPlaceholder('ユーザー名').fill('not-a-real-account')
  await page.getByPlaceholder('パスワード').fill('totally-wrong-password')
  await page.getByRole('button', { name: 'ログイン' }).click()

  await expect(page.locator('.dialog-error')).toBeVisible()
  await expect(page.locator('.sidebar')).toBeHidden()
})

test('correct login reaches the app, and logout returns to the login screen', async ({ page }) => {
  await page.goto('/')
  await page.getByPlaceholder('ユーザー名').fill(E2E_USERNAME)
  await page.getByPlaceholder('パスワード').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'ログイン' }).click()
  await expect(page.locator('.sidebar')).toBeVisible()

  await page.getByRole('button', { name: 'メニューを開く' }).click()
  await page.getByRole('button', { name: 'ログアウト' }).click()

  await expect(page.getByRole('heading', { name: 'feedla ログイン' })).toBeVisible()
  await expect(page.locator('.sidebar')).toBeHidden()
})
