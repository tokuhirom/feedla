// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'
import type { Entry } from '../api/types'
import { EntryItem } from './EntryItem'

function makeEntry(overrides: Partial<Entry> = {}): Entry {
  return {
    id: 1,
    feed_id: 1,
    guid: 'entry-1',
    url: 'https://example.com/article',
    title: 'Article title',
    body: '<p>Read <a href="https://example.com/related">more</a>.</p>',
    published_at: 0,
    updated_at: 0,
    fetched_at: 0,
    pinned: false,
    ...overrides,
  }
}

afterEach(cleanup)

describe('EntryItem', () => {
  it('opens links in the feed body in a separate tab', () => {
    render(<EntryItem entry={makeEntry()} focused={false} />)

    const link = screen.getByRole('link', { name: 'more' })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })
})
