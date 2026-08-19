// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { instagramEmbedSrc, rewriteInstagramEmbeds } from './instagramEmbed'

describe('instagramEmbedSrc', () => {
  it('builds the embed URL for a valid post permalink', () => {
    expect(instagramEmbedSrc('https://www.instagram.com/p/Cabc123-_/')).toBe(
      'https://www.instagram.com/p/Cabc123-_/embed/captioned/',
    )
  })

  it('builds the embed URL for a valid reel permalink, host without www', () => {
    expect(instagramEmbedSrc('https://instagram.com/reel/Cxyz789/')).toBe(
      'https://www.instagram.com/reel/Cxyz789/embed/captioned/',
    )
  })

  it('ignores a tracking query string', () => {
    expect(
      instagramEmbedSrc(
        'https://www.instagram.com/p/Cabc123/?utm_source=ig_embed',
      ),
    ).toBe('https://www.instagram.com/p/Cabc123/embed/captioned/')
  })

  const rejected = [
    ['wrong host', 'https://www.instagram.com.evil.example/p/Cabc123/'],
    ['host suffix confusion', 'https://evil.example/instagram.com/p/Cabc123/'],
    ['http scheme', 'http://www.instagram.com/p/Cabc123/'],
    ['path traversal', 'https://www.instagram.com/p/../admin/'],
    ['extra path segment', 'https://www.instagram.com/p/Cabc123/extra/'],
    ['unknown post kind', 'https://www.instagram.com/tv/Cabc123/'],
    ['not a permalink', 'javascript:alert(1)'],
    ['not a URL at all', 'not a url'],
  ] as const

  for (const [name, permalink] of rejected) {
    it(`rejects ${name}`, () => {
      expect(instagramEmbedSrc(permalink)).toBeNull()
    })
  }
})

describe('rewriteInstagramEmbeds', () => {
  it('replaces a valid embed blockquote with a sandboxed iframe', () => {
    const container = document.createElement('div')
    container.innerHTML =
      '<p>before</p>' +
      '<blockquote class="instagram-media" data-instgrm-permalink="https://www.instagram.com/p/Cabc123/?utm_source=ig_embed">' +
      '<a>view</a></blockquote>' +
      '<p>after</p>'

    rewriteInstagramEmbeds(container)

    const iframe = container.querySelector('iframe')
    expect(iframe).not.toBeNull()
    expect(iframe?.src).toBe(
      'https://www.instagram.com/p/Cabc123/embed/captioned/',
    )
    expect(iframe?.getAttribute('sandbox')).toBe('allow-scripts allow-popups')
    expect(iframe?.referrerPolicy).toBe('no-referrer')
    expect(container.querySelector('blockquote')).toBeNull()
  })

  it('leaves an unsafe permalink as an untouched blockquote', () => {
    const container = document.createElement('div')
    container.innerHTML =
      '<blockquote class="instagram-media" data-instgrm-permalink="https://evil.example/p/Cabc123/">fallback link</blockquote>'

    rewriteInstagramEmbeds(container)

    expect(container.querySelector('iframe')).toBeNull()
    expect(container.querySelector('blockquote')).not.toBeNull()
  })

  it('is a no-op when there is no embed', () => {
    const container = document.createElement('div')
    container.innerHTML = '<p>plain body</p>'
    rewriteInstagramEmbeds(container)
    expect(container.innerHTML).toBe('<p>plain body</p>')
  })

  it('is idempotent: running it twice does not throw or duplicate', () => {
    const container = document.createElement('div')
    container.innerHTML =
      '<blockquote class="instagram-media" data-instgrm-permalink="https://www.instagram.com/p/Cabc123/">x</blockquote>'

    rewriteInstagramEmbeds(container)
    rewriteInstagramEmbeds(container)

    expect(container.querySelectorAll('iframe').length).toBe(1)
  })
})
