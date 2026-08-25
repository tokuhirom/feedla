import { useEffect } from 'preact/hooks'
import { markReadOptimistic } from '../state/entries'

/** Marks an entry read once the reader has scrolled past it (its bottom
 * edge has crossed above the entry pane's top edge), so touch users can
 * clear unreads by scrolling alone -- there's no keyboard on a phone to
 * drive the j/k flow moveFocus() implements. Runs alongside j/k rather
 * than replacing it: both funnel into the same idempotent
 * markReadOptimistic, and "scrolled past" already matches what j means on
 * desktop, so enabling this there too is a natural extension, not a
 * behavior change.
 *
 * entryIds is the currently-rendered entry list's ids, used only to
 * re-attach the observer to fresh .entry-item nodes after a re-render
 * (e.g. switching feeds); the ids themselves aren't read.
 */
export function useAutoMarkRead(entryIds: number[]): void {
  useEffect(() => {
    const root = document.querySelector('.entry-pane')
    if (!root) return

    const observer = new IntersectionObserver(
      (observed) => {
        for (const item of observed) {
          if (item.isIntersecting) continue
          // A target detached from the document (e.g. this feed's entries
          // were just replaced by the "読み込み中…" placeholder on a feed
          // switch) reports a zeroed, disconnected boundingClientRect, which
          // the "scrolled past" check below can't tell apart from a real
          // entry that scrolled off the top -- without this guard, merely
          // navigating away (even via `a`, which explicitly asks not to
          // mark anything read) could mark the entry read.
          if (!item.target.isConnected) continue
          // rootBounds is null only if the root itself is detached, which
          // can't happen here since we just queried it from the DOM.
          if (item.boundingClientRect.bottom > item.rootBounds!.top) continue
          const id = Number((item.target as HTMLElement).dataset.entryId)
          if (Number.isFinite(id)) markReadOptimistic(id)
        }
      },
      { root, threshold: 0 },
    )

    for (const el of root.querySelectorAll<HTMLElement>(
      '.entry-item[data-entry-id]',
    )) {
      observer.observe(el)
    }

    // The observer above only catches an entry once it has scrolled
    // entirely above the pane -- the *last* entry in the list can never do
    // that (there's nothing left to scroll it past), so it would otherwise
    // never get marked read on a phone with no j to fall back on. Once the
    // reader actually scrolls the pane to its end, treat every currently
    // loaded entry as read; markReadOptimistic is a no-op for ones already
    // read, so this only affects the tail that the observer missed.
    function onScroll(): void {
      const atBottom =
        root!.scrollTop + root!.clientHeight >= root!.scrollHeight - 2
      if (!atBottom) return
      for (const id of entryIds) markReadOptimistic(id)
    }
    root.addEventListener('scroll', onScroll, { passive: true })
    // If the pane's content is short enough to fit without overflowing (a
    // single short entry is the common case), it never becomes scrollable
    // and no 'scroll' event ever fires, so the tail-fallback above is
    // unreachable -- there's simply no j to fall back on either. A touch
    // swipe still fires 'touchmove' even when it can't actually move
    // anything, so use that as the equivalent "the reader engaged with this
    // pane" signal on touch devices. Deliberately not firing this on mount:
    // fitting on screen doesn't by itself mean the reader has looked yet
    // (e.g. a longer list that still happens to fit one screen) -- an
    // actual gesture is what distinguishes "seen" from "just loaded".
    root.addEventListener('touchmove', onScroll, { passive: true })

    return () => {
      observer.disconnect()
      root.removeEventListener('scroll', onScroll)
      root.removeEventListener('touchmove', onScroll)
    }
  }, [entryIds])
}
