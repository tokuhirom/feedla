import { describe, expect, it } from 'vitest'
import { faviconUrl } from './favicon'

describe('faviconUrl', () => {
  it('extracts the hostname from an absolute URL', () => {
    expect(faviconUrl('https://example.com/feed.xml')).toBe(
      'https://www.google.com/s2/favicons?sz=16&domain=example.com',
    )
  })

  it('uses the given size', () => {
    expect(faviconUrl('https://example.com/feed.xml', 32)).toBe(
      'https://www.google.com/s2/favicons?sz=32&domain=example.com',
    )
  })

  it('falls back to the raw input when the URL is not absolute', () => {
    expect(faviconUrl('not-a-url')).toBe(
      'https://www.google.com/s2/favicons?sz=16&domain=not-a-url',
    )
  })

  it('URL-encodes the domain', () => {
    expect(faviconUrl('日本語')).toBe(
      'https://www.google.com/s2/favicons?sz=16&domain=%E6%97%A5%E6%9C%AC%E8%AA%9E',
    )
  })
})
