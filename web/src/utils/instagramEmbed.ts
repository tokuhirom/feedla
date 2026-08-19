// Mirrors internal/crawler/parser.go's instagramPermalinkAttrPattern --
// kept in sync manually since there's no shared schema between Go and the
// frontend. The Go side is a coarse first-layer filter (it decides whether
// data-instgrm-permalink survives sanitization at all); this is the
// authoritative check before ever setting an <iframe src>, so it must not
// simply trust that the attribute reaching the DOM is safe.
const SHORTCODE_PATTERN = /^[A-Za-z0-9_-]+$/

/** Validates permalink as a genuine Instagram post/reel permalink and, if
 * valid, returns the corresponding single-post embed URL. permalink comes
 * straight from a data-* attribute an untrusted feed author wrote (see
 * rewriteInstagramEmbeds below), so this rejects anything that isn't
 * exactly "https://(www.)instagram.com/p/<id>/" or ".../reel/<id>/" -- in
 * particular it doesn't allow extra path segments or host suffixes like
 * "instagram.com.evil.example". */
export function instagramEmbedSrc(permalink: string): string | null {
  let url: URL
  try {
    url = new URL(permalink)
  } catch {
    return null
  }
  if (url.protocol !== 'https:') return null
  const host = url.hostname.toLowerCase()
  if (host !== 'instagram.com' && host !== 'www.instagram.com') return null

  const segments = url.pathname.split('/').filter((s) => s !== '')
  if (segments.length !== 2) return null
  const [kind, id] = segments
  if (kind !== 'p' && kind !== 'reel') return null
  if (!SHORTCODE_PATTERN.test(id)) return null

  return `https://www.instagram.com/${kind}/${id}/embed/captioned/`
}

/** Replaces every <blockquote class="instagram-media" data-instgrm-permalink="...">
 * inside container with a sandboxed <iframe> pointing at Instagram's own
 * single-post embed page, so users who opt in (see
 * state/settings.ts's instagramEmbedsEnabled) see the post inline instead
 * of just a "view this post" link. Only ever called when that setting is
 * on -- see docs/adr/0001-third-party-embed-in-feed-content.md.
 *
 * Idempotent: a blockquote that's already been replaced no longer matches
 * the selector, so re-running this (e.g. a second render pass) is a
 * no-op for it. */
export function rewriteInstagramEmbeds(container: HTMLElement): void {
  const blockquotes = container.querySelectorAll<HTMLQuoteElement>(
    'blockquote.instagram-media[data-instgrm-permalink]',
  )
  for (const bq of blockquotes) {
    const permalink = bq.getAttribute('data-instgrm-permalink')
    if (!permalink) continue
    const src = instagramEmbedSrc(permalink)
    if (!src) continue

    const iframe = document.createElement('iframe')
    iframe.src = src
    // No allow-same-origin: the embedded page must not be able to
    // read/write this origin's cookies or storage. Set via setAttribute
    // rather than the .sandbox DOMTokenList -- jsdom (used in tests)
    // doesn't implement it.
    iframe.setAttribute('sandbox', 'allow-scripts allow-popups')
    iframe.referrerPolicy = 'no-referrer'
    iframe.loading = 'lazy'
    iframe.width = '100%'
    iframe.height = '600'
    bq.replaceWith(iframe)
  }
}
