import { test as setup } from '@playwright/test'
import { authFile } from '../playwright.config'

// e2e/testserver/main.go seeds a fixed admin account (bypassing the
// interactive setup screen, since that's a one-time UI flow better tested
// on its own -- see auth-flow.spec.ts) with these credentials before the
// server starts listening.
const E2E_USERNAME = 'e2e-admin'
const E2E_PASSWORD = 'e2e-test-password-12345'

// Runs once before every other test file (see the 'setup' project and its
// dependents in playwright.config.ts): logs in through the real login
// screen and stashes the resulting session cookie so every other spec's
// page.goto('/') starts already authenticated, exactly like the
// pre-auth suite did.
setup('authenticate', async ({ page }) => {
  await page.goto('/')
  await page.getByPlaceholder('ユーザー名').fill(E2E_USERNAME)
  await page.getByPlaceholder('パスワード').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'ログイン' }).click()
  await page.locator('.sidebar').waitFor({ state: 'visible' })
  await page.context().storageState({ path: authFile })
})
