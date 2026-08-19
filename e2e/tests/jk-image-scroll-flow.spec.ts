import { expect, test } from '@playwright/test'
import { subscribeFeed } from './subscribe-helper'

// Regression test for j/k landing far away from the target entry on
// image-heavy feeds. Three separate defects conspired here, and this spec
// fails if any of them comes back:
//
// - .entry-pane is a column flex container, so flex-shrink squashed
//   content-visibility-skipped entries down to their padding, making every
//   scroll-position computation see wildly wrong geometry (fixed with
//   flex-shrink: 0 + contain-intrinsic-size on .entry-item).
// - moveFocus's settle correction computed an absolute scrollTop but
//   applied it with `+=`, re-adding the whole scroll distance and slamming
//   the pane past the target.
// - a single settle pass isn't enough while content-visibility re-layouts
//   keep shifting the target, so settle re-measures until the drift is
//   gone.
//
// After each j/k press settles, the focused entry's top edge must sit
// right below the sticky header, and focus must walk entries one at a
// time -- when the scroll lands wrong, moveFocus's scroll-position resync
// makes j skip entries and k oscillate, so the walked titles are asserted
// too.

const FEED_URL = 'http://127.0.0.1:18098/image-nav-fixture'

// Entries are listed newest-first, so walking forward with j goes from
// Image Nav Item 6 down to 1.
const J_SEQUENCE = [5, 4, 3, 2, 1]
const K_SEQUENCE = [2, 3, 4, 5, 6]

async function measureFocused(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const container = document.querySelector('.entry-pane') as HTMLElement
    const header = container.querySelector('.entry-header') as HTMLElement
    const focused = container.querySelector(
      '.entry-item.focused',
    ) as HTMLElement | null
    const viewTop =
      container.getBoundingClientRect().top +
      header.getBoundingClientRect().height
    return {
      offset: focused
        ? focused.getBoundingClientRect().top - viewTop
        : Number.NaN,
      title:
        focused?.querySelector('h2, h3, .entry-title')?.textContent ?? '',
    }
  })
}

test('j/k は画像の多いエントリでも1件ずつ、ヘッダー直下に着地する', async ({
  page,
}) => {
  await page.goto('/')
  await subscribeFeed(page, FEED_URL)

  const subRow = page.locator('.subscription-row', {
    hasText: 'Image Nav Fixture Feed',
  })
  await expect(subRow).toBeVisible({ timeout: 10_000 })
  await subRow.click()

  const entries = page.locator('.entry-item')
  await expect(entries).toHaveCount(6)
  await expect(entries.first()).toHaveClass(/focused/)

  // Let the fixture images finish loading so entry heights are final.
  await page.waitForTimeout(1500)

  for (const [key, sequence] of [
    ['j', J_SEQUENCE],
    ['k', K_SEQUENCE],
  ] as const) {
    for (const expectedItem of sequence) {
      await page.keyboard.press(key)
      // Wait out the smooth scroll plus the settle correction.
      await page.waitForTimeout(1200)
      const m = await measureFocused(page)
      expect(m.title, `${key} -> Item ${expectedItem}`).toBe(
        `Image Nav Item ${expectedItem}`,
      )
      expect(
        Math.abs(m.offset),
        `${key} -> Item ${expectedItem} landing offset`,
      ).toBeLessThan(30)
    }
  }

  // Rapid presses: a superseded press's settle must not yank the pane
  // back toward its own stale target.
  for (let i = 0; i < 4; i++) {
    await page.keyboard.press('j')
    await page.waitForTimeout(250)
  }
  await page.waitForTimeout(2000)
  const rapid = await measureFocused(page)
  expect(Math.abs(rapid.offset), 'rapid j landing offset').toBeLessThan(30)

  // This suite shares one DB/sidebar across every spec (see
  // playwright.config.ts) -- leaving the feed behind would shift
  // sidebar-adjacency ordering for whichever test runs next.
  page.once('dialog', (d) => void d.accept())
  await page.getByTitle('フィード詳細').click()
  await page.locator('.unsubscribe-button').click()
  await expect(subRow).toHaveCount(0)
})
