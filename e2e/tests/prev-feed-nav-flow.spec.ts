import { type Locator, type Page, expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// `a` (前の購読へ) in a real browser: it must land on the feed the reader
// just finished with -- not skip over it because reading it to the end
// dropped its unread count to zero -- resume at the entry they left off at,
// and not mark the feed it leaves as read. Behavior spec:
// docs/keyboard-shortcuts.md.
//
// The landing rules themselves are covered far more cheaply in
// state/subscriptions.test.ts and state/actions.test.ts; what only a real
// browser can show is the parts that depend on layout and the DOM -- the
// restored scroll/focus position, and mark-on-leave (which measures element
// boxes) actually being skipped.

const FEED_A = 'http://127.0.0.1:18098/prev-nav-fixture-a'
const FEED_B = 'http://127.0.0.1:18098/prev-nav-fixture-b'
const FEED_C = 'http://127.0.0.1:18098/prev-nav-fixture-c'

// See nav-order-flow.spec.ts's copy of this helper: .entry-item uses
// content-visibility: auto, so racing its layout would let mark-on-leave
// see a not-yet-measured entry as small enough to be "fully visible" and
// mark it read.
async function waitForTallEntryLaidOut(page: Page): Promise<void> {
  await expect(async () => {
    const bodyBox = await page.locator('.entry-body').first().boundingBox()
    const paneBox = await page.locator('.entry-pane').boundingBox()
    expect(bodyBox).not.toBeNull()
    expect(paneBox).not.toBeNull()
    expect(bodyBox!.height).toBeGreaterThan(paneBox!.height)
  }).toPass({ timeout: 5_000 })
}

/** Waits until the *server* agrees a feed has no unread entries left.
 *
 * j marks entries read optimistically and batches the POST behind an idle
 * timer (state/entries.ts), and this suite's server additionally holds that
 * POST open for seconds on purpose (see playwright.config.ts's
 * FEEDLA_E2E_DELAY_MARK_READ_MS). Everything this test asserts after coming
 * back to a feed -- the read-entry fallback list, and the restored position
 * inside it -- depends on the refetch seeing the reads, so pin that down
 * rather than racing it. The sidebar's own badge is optimistic and so can't
 * answer this. */
async function waitForServerUnreadZero(
  page: Page,
  title: string,
): Promise<void> {
  await expect(async () => {
    const res = await page.request.get('/api/v1/subscriptions')
    expect(res.ok()).toBe(true)
    const body = (await res.json()) as {
      subscriptions: { title: string; unread_count: number }[]
    }
    const sub = body.subscriptions.find((s) => s.title === title)
    expect(sub?.unread_count).toBe(0)
  }).toPass({ timeout: 20_000 })
}

/** Reads a feed's three entries to zero unread, leaving the focus ring on
 * the third. */
async function readAllThree(page: Page, title: string): Promise<void> {
  const items = page.locator('.entry-item')
  await expect(items).toHaveCount(3)
  for (let i = 0; i < 3; i++) {
    await page.keyboard.press('j')
    await expect(items.nth(i)).toHaveClass(/read/)
  }
  await expect(items.nth(2)).toHaveClass(/focused/)
  await waitForServerUnreadZero(page, title)
}

test('a returns to the feed just finished, resumes its position, and leaves nothing read', async ({
  page,
}) => {
  await page.goto('/')

  // C goes first on purpose. AddSubscriptionDialog selects and loads each
  // feed as it's subscribed, but leaves it via plain selectFeed, which does
  // not mark anything read -- whereas the first sidebar *click* below goes
  // through selectAndLoadFeed and does. C's entry is short enough to be
  // marked by that, so it has to be off screen by then; subscribing it
  // first leaves B (tall entries, immune) as the one being clicked away
  // from.
  for (const [url, title] of [
    [FEED_C, 'Zzz Prev Nav Feed C'],
    [FEED_A, 'Zzz Prev Nav Feed A'],
    [FEED_B, 'Zzz Prev Nav Feed B'],
  ]) {
    await subscribeFeed(page, url)
    await expect(
      page.locator('.subscription-row', { hasText: title }),
    ).toBeVisible({ timeout: 10_000 })
  }

  await page.getByText('カテゴリ').click()

  // Other specs share this suite's DB/sidebar (see playwright.config.ts), so
  // scope to our three rows and assert their relative order before relying
  // on it -- see feed-server.mjs's doc comment for why they stay adjacent.
  const rows = page.locator('.subscription-row', { hasText: /Zzz Prev Nav Feed/ })
  await expect(rows).toHaveCount(3)
  await expect(rows.nth(0)).toContainText('Zzz Prev Nav Feed A')
  await expect(rows.nth(1)).toContainText('Zzz Prev Nav Feed B')
  await expect(rows.nth(2)).toContainText('Zzz Prev Nav Feed C')

  const header = page.locator('.entry-header-title')

  // Read A right through, then s to B and read that right through too --
  // the ordinary "burn down the unreads" flow that leaves both feeds at
  // zero unread and lands the reader on C.
  await rows.nth(0).click()
  await expect(header).toContainText('Zzz Prev Nav Feed A')
  await waitForTallEntryLaidOut(page)
  await readAllThree(page, 'Zzz Prev Nav Feed A')

  await page.keyboard.press('s')
  await expect(header).toContainText('Zzz Prev Nav Feed B')
  await waitForTallEntryLaidOut(page)
  await readAllThree(page, 'Zzz Prev Nav Feed B')

  await page.keyboard.press('s')
  await expect(header).toContainText('Zzz Prev Nav Feed C')
  await expect(page.locator('.entry-item')).toHaveCount(1)

  // The fix: a lands on B, the feed just read. Before this, the zero-unread
  // check in adjacentFeedId skipped straight past it to A.
  await page.keyboard.press('a')
  await expect(header).toContainText('Zzz Prev Nav Feed B')

  // B is fully read, so its recent (read) entries are shown...
  await expect(page.locator('.entry-list-note')).toContainText(
    '未読はありません。直近の記事を表示しています',
  )
  await expect(page.locator('.entry-item')).toHaveCount(3)
  // ...and the focus ring resumes on the entry the reader left off at,
  // rather than the top of the list.
  await expect(page.locator('.entry-item.focused')).toContainText(
    'Prev Nav B Third',
  )

  // C's single entry is short enough to sit entirely inside the pane, which
  // is exactly what selectAndLoadFeed's mark-on-leave marks read. `a` must
  // skip that step: the reader pressed it to go back, not to declare C read.
  await expect(rows.nth(2).locator('.unread-count')).toHaveText('1')

  // Pressing a again keeps walking back to A, which is likewise read and
  // resumes where it was left.
  await page.keyboard.press('a')
  await expect(header).toContainText('Zzz Prev Nav Feed A')
  await expect(page.locator('.entry-item.focused')).toContainText(
    'Prev Nav A Third',
  )

  // s stays asymmetric on the way back out: it only stops on feeds with
  // unread left, so B (read) is skipped and C is next.
  await page.keyboard.press('s')
  await expect(header).toContainText('Zzz Prev Nav Feed C')

  // This suite shares one DB/sidebar -- leave the three feeds unsubscribed
  // again so later specs see the sidebar they expect.
  async function unsubscribe(row: Locator): Promise<void> {
    await row.click()
    page.once('dialog', (d) => void d.accept())
    await page.getByTitle('フィード詳細').click()
    await page.locator('.unsubscribe-button').click()
  }
  await unsubscribe(rows.nth(2))
  await unsubscribe(rows.nth(1))
  await unsubscribe(rows.nth(0))
  await expect(rows).toHaveCount(0)
})
