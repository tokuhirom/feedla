import { effect, signal } from '@preact/signals'

// Per-browser (not per-account -- see docs/adr/0001-third-party-embed-in-feed-content.md)
// opt-in for turning an Instagram post/reel embed's <blockquote> into a
// sandboxed <iframe> that actually shows the post. Default off: it's
// third-party content that phones home to instagram.com on every view,
// which not everyone wants happening automatically.
const INSTAGRAM_EMBEDS_ENABLED_KEY = 'feedla:instagramEmbedsEnabled'

function loadInstagramEmbedsEnabled(): boolean {
  return localStorage.getItem(INSTAGRAM_EMBEDS_ENABLED_KEY) === 'true'
}

export const instagramEmbedsEnabled = signal<boolean>(
  loadInstagramEmbedsEnabled(),
)

effect(() => {
  localStorage.setItem(
    INSTAGRAM_EMBEDS_ENABLED_KEY,
    String(instagramEmbedsEnabled.value),
  )
})
