import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// Covers docs/multi-user-design.md's Phase C completion condition: "2 ユー
// ザーでの e2e(相互に見えない・操作できないことのテストを含む)". The Go
// httptest-level suite (internal/api/idor_test.go) already proves the
// store/API layer rejects cross-user access resource by resource; this
// spec instead drives two real logged-in browser sessions end to end
// (session cookies + CSRF Origin check + the SPA's own rendering) to prove
// a member genuinely can't see or touch another user's subscription
// through the app as a whole.
const MEMBER_USERNAME = 'idor-flow-member'
const MEMBER_PASSWORD = 'idor-flow-member-pw123'
const OWNER_FIXTURE_URL = 'http://127.0.0.1:18098/idor-fixture-owner'
const OWNER_FEED_TITLE = 'IDOR Fixture Owner Feed'

test('a member cannot see or operate on another user\'s subscription', async ({
  page,
  browser,
  baseURL,
}) => {
  // admin (the default logged-in page, per playwright.config.ts's
  // storageState) subscribes to a dedicated fixture feed nobody else
  // touches.
  await page.goto('/')
  await subscribeFeed(page, OWNER_FIXTURE_URL)
  await expect(
    page.locator('.subscription-row', { hasText: OWNER_FEED_TITLE }),
  ).toBeVisible({ timeout: 10_000 })

  const adminApi = page.context().request
  const subsRes = await adminApi.get('/api/v1/subscriptions')
  const { subscriptions } = (await subsRes.json()) as {
    subscriptions: { feed_id: number; title: string }[]
  }
  const ownerFeedId = subscriptions.find((s) => s.title === OWNER_FEED_TITLE)!.feed_id

  // Create a second, non-admin account through the admin panel (same flow
  // as admin-users-flow.spec.ts).
  await page.getByRole('button', { name: 'メニューを開く' }).click()
  await page.getByRole('button', { name: 'ユーザー管理' }).click()
  await page.waitForSelector('.admin-user-table')
  await page.getByPlaceholder('ユーザー名').fill(MEMBER_USERNAME)
  await page.getByPlaceholder('パスワード(12文字以上)').fill(MEMBER_PASSWORD)
  await page.getByRole('button', { name: '作成' }).click()
  await expect(page.locator('tr', { hasText: MEMBER_USERNAME })).toBeVisible()
  await page.getByRole('button', { name: '閉じる' }).click()

  // A second, independently logged-in browser context -- explicit empty
  // storageState overrides the chromium project's default admin session
  // (see admin-users-flow.spec.ts for the same idiom).
  const memberContext = await browser.newContext({
    storageState: { cookies: [], origins: [] },
  })
  const memberPage = await memberContext.newPage()
  await memberPage.goto('/')
  await memberPage.getByPlaceholder('ユーザー名').fill(MEMBER_USERNAME)
  await memberPage.getByPlaceholder('パスワード').fill(MEMBER_PASSWORD)
  await memberPage.getByRole('button', { name: 'ログイン' }).click()
  await expect(memberPage.locator('.sidebar')).toBeVisible()

  // Invisibility: the owner's feed must not appear anywhere in the
  // member's own sidebar.
  await expect(
    memberPage.locator('.subscription-row', { hasText: OWNER_FEED_TITLE }),
  ).toHaveCount(0)

  // Inoperability: direct API calls using the member's own session
  // (memberContext.request shares its cookie jar) against the owner's
  // feed_id must not read or mutate anything. Origin must be set
  // explicitly on state-changing calls -- see feed-detail-move-folder-
  // flow.spec.ts's note on internal/api/auth_middleware.go's CSRF check.
  const memberApi = memberContext.request
  const entriesRes = await memberApi.get(`/api/v1/subscriptions/${ownerFeedId}/entries`)
  expect(entriesRes.status()).toBe(200)
  const entriesBody = (await entriesRes.json()) as { entries: unknown[] }
  expect(entriesBody.entries).toHaveLength(0)

  const patchRes = await memberApi.patch(`/api/v1/subscriptions/${ownerFeedId}`, {
    data: { rating: 5 },
    headers: { Origin: baseURL! },
  })
  expect(patchRes.status()).toBe(404)

  const deleteRes = await memberApi.delete(`/api/v1/subscriptions/${ownerFeedId}`, {
    headers: { Origin: baseURL! },
  })
  expect(deleteRes.status()).toBe(404)

  await memberContext.close()

  // Back as admin: the subscription must be completely untouched by the
  // member's rejected attempts.
  await page.reload()
  const ownerRow = page.locator('.subscription-row', { hasText: OWNER_FEED_TITLE })
  await expect(ownerRow).toBeVisible()
  const rowAfter = await adminApi.get('/api/v1/subscriptions')
  const { subscriptions: subsAfter } = (await rowAfter.json()) as {
    subscriptions: { feed_id: number; title: string; rating: number }[]
  }
  expect(subsAfter.find((s) => s.feed_id === ownerFeedId)?.rating).toBe(0)

  // Cleanup so neither the fixture subscription nor the member account
  // linger in the shared suite DB (see [[project-feedla-e2e-shared-db]]).
  await ownerRow.click()
  await page.getByTitle('フィード詳細').click()
  page.once('dialog', (d) => void d.accept())
  await page.locator('.unsubscribe-button').click()

  await page.getByRole('button', { name: 'メニューを開く' }).click()
  await page.getByRole('button', { name: 'ユーザー管理' }).click()
  await page
    .locator('tr', { hasText: MEMBER_USERNAME })
    .getByRole('button', { name: '無効化' })
    .click()
  await page.getByRole('button', { name: '閉じる' }).click()
})
