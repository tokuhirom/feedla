import { batch, signal } from '@preact/signals'
import type { SubscriptionKind } from '../api/types'

export type FeedManagerKindFilter = 'all' | SubscriptionKind

// FeedManagerPane's own filter state, lifted out of the component into
// signals so state/url.ts can observe (and restore) it -- see
// state/actions.ts's openFeedManager/resetFeedManagerFilters for the reset
// behavior on a fresh open, and state/url.ts's applyRouteToSignals for
// restoring these from a bookmarked/reloaded URL instead.
export const feedManagerQuery = signal('')
// Kind is an independent axis from the ⚠ エラーのみ view: "which feeds use
// selector extraction" is a question you ask about healthy feeds too, so
// this filter stays at the top level and combines with everything else.
export const feedManagerKindFilter = signal<FeedManagerKindFilter>('all')
export const feedManagerOnlyErrors = signal(false)
// Extra narrowing filters, only surfaced in the ⚠ エラーのみ view -- see
// FeedManagerPane's own comment on resetErrorFilters for why.
export const feedManagerMinErrorCount = signal('')
export const feedManagerUrlNeedle = signal('')
export const feedManagerErrorNeedle = signal('')

// batch()'d so state/url.ts's startUrlSync effect (which reacts to every one
// of these signals) fires once per reset instead of once per field --
// otherwise each intermediate combination would briefly replaceState() the
// URL before the next field lands.
export function resetErrorFilters(): void {
  batch(() => {
    feedManagerMinErrorCount.value = ''
    feedManagerUrlNeedle.value = ''
    feedManagerErrorNeedle.value = ''
  })
}

/** Resets every filter to its default, seeding onlyErrors from the caller --
 * called by openFeedManager each time the pane is freshly opened (from the
 * sidebar's ⚠ badge or ⋮ menu), so re-opening it always starts from a clean
 * slate regardless of whatever was left over from a previous visit. Restoring
 * filters from a URL (reload/bookmark) goes through applyRouteToSignals
 * instead, which does NOT call this. */
export function resetFeedManagerFilters(onlyErrors: boolean): void {
  batch(() => {
    feedManagerKindFilter.value = 'all'
    feedManagerQuery.value = ''
    feedManagerOnlyErrors.value = onlyErrors
    resetErrorFilters()
  })
}
