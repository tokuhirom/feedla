import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// Screen state (selected feed, search, フィード管理 filters, open overlays)
// used to live only in memory -- reloading always dropped back to the
// top-level view. This covers state/url.ts's URL <-> signal sync: each
// screen transition below must show up in the URL, and reloading must
// restore it. Own fixture path (see feed-server.mjs) so it doesn't collide
// with other specs sharing the DB (see playwright.config.ts).
const FIXTURE_FEED_URL = 'http://127.0.0.1:18098/url-sync-fixture'

test('screen state (feed, search, フィード管理 filters, overlay) reflects in the URL and survives a reload', async ({
  page,
}) => {
  await page.goto('/')

  await subscribeFeed(page, FIXTURE_FEED_URL)
  const subRow = page.locator('.subscription-row', { hasText: 'Url Sync Fixture Feed' })
  await expect(subRow).toBeVisible({ timeout: 10_000 })

  // Subscribing auto-selects the new feed (see AddSubscriptionDialog).
  await expect(page).toHaveURL(/\/feed\/\d+$/)
  await page.reload()
  await expect(page.locator('.entry-item', { hasText: 'Url Sync First Item' })).toBeVisible({
    timeout: 10_000,
  })

  // / replaces the per-feed header with an inline search box; the query
  // shows up as ?q= and the search stays live across a reload. Opening
  // search leaves the previously-selected feed (openSearch resets
  // selectedFeedId), so the URL switches straight from /feed/:id to
  // /search.
  await page.keyboard.press('/')
  const searchHeader = page.locator('.search-header')
  await expect(searchHeader).toBeVisible()
  await searchHeader.locator('input[type="text"]').fill('Url Sync')
  await searchHeader.getByRole('button', { name: '検索' }).click()

  await expect(page).toHaveURL(/\/search\?q=Url(\+|%20)Sync$/)
  await expect(
    page.locator('.entry-pane .entry-item', { hasText: 'Url Sync First Item' }),
  ).toBeVisible()

  await page.reload()
  await expect(
    page.locator('.entry-pane .entry-item', { hasText: 'Url Sync First Item' }),
  ).toBeVisible({ timeout: 10_000 })

  // フィード管理's own text/kind filters land on /manage as query params --
  // opening it from the sidebar menu likewise leaves search mode behind.
  await page.getByLabel('メニューを開く').click()
  await page.getByRole('button', { name: 'フィード管理' }).click()
  await expect(page).toHaveURL('/manage')

  await page.locator('.feed-manager-search').fill('Url Sync')
  await expect(page).toHaveURL(/\/manage\?q=Url(\+|%20)Sync$/)

  await page.getByRole('button', { name: /📡 フィード/ }).click()
  await expect(page).toHaveURL(/kind=feed/)
  await expect(page).toHaveURL(/q=Url(\+|%20)Sync/)

  await page.reload()
  await expect(page.locator('.feed-manager-search')).toHaveValue('Url Sync', {
    timeout: 10_000,
  })
  await expect(page.getByRole('button', { name: /📡 フィード/ })).toHaveClass(/active/)

  // ? opens the help overlay; unlike the main-view transitions above, an
  // overlay layers on top of whatever's showing (still /manage?... here),
  // so its open state shows up as an added &ov=help and survives a reload
  // without touching the rest of the URL. Blur first -- useKeyboardShortcuts
  // ignores keys while an <input> is focused (see .feed-manager-search
  // above), and ? would otherwise just get typed into it.
  await page.locator('.feed-manager-search').blur()
  await page.keyboard.press('?')
  await expect(page.locator('.help-overlay')).toBeVisible()
  await expect(page).toHaveURL(/[?&]ov=help(&|$)/)
  await expect(page).toHaveURL(/\/manage\?/)

  await page.reload()
  await expect(page.locator('.help-overlay')).toBeVisible({ timeout: 10_000 })
  await page.getByRole('button', { name: '閉じる' }).click()

  // This suite shares one DB/sidebar across every spec (see
  // playwright.config.ts) -- leave the fixture subscription behind
  // unsubscribed so it doesn't shift other specs' sidebar-adjacency
  // ordering.
  await subRow.click()
  page.once('dialog', (d) => void d.accept())
  await page.getByTitle('フィード詳細').click()
  await page.locator('.unsubscribe-button').click()
})

// Regression coverage for the bug state/subscriptions.ts's isFeedlaNavEntry
// exists to prevent: on mobile, opening a deep link (a bookmark, a shared
// URL, a PWA start URL) as the tab's very first navigation makes
// isInDetail() true immediately via hydrateSignalsFromLocation, with no
// pushMobileDetailNav-pushed history entry underneath it. Naively calling
// history.back() there is either a silent no-op (jamming the back button
// forever, since no popstate ever fires to clear mobileBackPending) or a
// real navigation away from feedla. 戻る must instead just clear the
// signals synchronously and land on the subscription list, matching the
// desktop behavior.
test('mobile: opening a feed URL as the tab entry point, then tapping 戻る, lands on the list instead of jamming or leaving the app', async ({
  page,
}) => {
  await page.goto('/')
  await subscribeFeed(page, FIXTURE_FEED_URL)
  const subRow = page.locator('.subscription-row', { hasText: 'Url Sync Fixture Feed' })
  await expect(subRow).toBeVisible({ timeout: 10_000 })
  await expect(page).toHaveURL(/\/feed\/\d+$/)
  const feedUrl = page.url()

  // Switching to a narrow viewport and re-navigating to the feed's own URL
  // simulates a deep link: nothing in this tab's history was pushed by
  // pushMobileDetailNav (that only ever fires below the mobile breakpoint,
  // and this navigation happens outside the app entirely), so
  // window.history.state on the resulting entry carries no feedlaNav
  // marker.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(feedUrl)
  await expect(page.locator('.entry-pane')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('.sidebar')).toBeHidden()

  await page.locator('.back-button').click()
  await expect(page.locator('.sidebar')).toBeVisible()
  await expect(page.locator('.entry-pane')).toBeHidden()
  // Still on feedla, not navigated away -- and 戻る remains responsive
  // (not jammed by a mobileBackPending that never got cleared).
  await expect(page).toHaveURL('/')
  await subRow.click()
  await expect(page.locator('.entry-pane')).toBeVisible()
  await page.locator('.back-button').click()
  await expect(page.locator('.sidebar')).toBeVisible()

  await page.setViewportSize({ width: 1280, height: 720 })
  await subRow.click()
  page.once('dialog', (d) => void d.accept())
  await page.getByTitle('フィード詳細').click()
  await page.locator('.unsubscribe-button').click()
})
