import type { SubscriptionKind } from '../api/types'

// Shared by FeedManagerPane's kind column and FeedDetailOverlay's same-site
// hint, so a feed's kind icon/label reads the same in both places.
export const SUBSCRIPTION_KIND_INFO: {
  value: SubscriptionKind
  icon: string
  label: string
}[] = [
  { value: 'feed', icon: '📡', label: 'フィード' },
  { value: 'pagewatch', icon: '👁', label: 'ページ監視' },
  { value: 'selector', icon: '📰', label: '記事一覧抽出' },
]

// Falls back to the raw kind string rather than rendering nothing, so a kind
// added on the server before this table catches up is still visible.
export function kindIcon(kind: SubscriptionKind): string {
  return SUBSCRIPTION_KIND_INFO.find((k) => k.value === kind)?.icon ?? kind
}

export function kindLabel(kind: SubscriptionKind): string {
  const k = SUBSCRIPTION_KIND_INFO.find((x) => x.value === kind)
  return k ? `${k.icon} ${k.label}` : kind
}
