// The behavior of every key below -- what it lands on, what it marks read,
// and when it does nothing -- is specified in docs/keyboard-shortcuts.md.
// Keep that document in sync when changing anything here; the s/a landing
// rules in particular are easy to break in ways no type error catches.
import { useEffect } from 'preact/hooks'
import {
  adjustRating,
  goToNextFeed,
  goToPreviousFeed,
  openSearch,
  refreshCurrentFeed,
  togglePinFocused,
} from '../state/actions'
import {
  entries,
  focusedIndex,
  isAtLastVisibleEntry,
  moveFocus,
} from '../state/entries'
import { pinsOpen } from '../state/pins'
import { selectedFeedId } from '../state/subscriptions'
import { helpOpen } from '../state/ui'

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return (
    target.tagName === 'INPUT' ||
    target.tagName === 'TEXTAREA' ||
    target.isContentEditable
  )
}

function scrollEntryPane(direction: 1 | -1): void {
  document.querySelector('.entry-pane')?.scrollBy({
    top: direction * window.innerHeight * 0.8,
    behavior: 'smooth',
  })
}

export function useKeyboardShortcuts(): void {
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent): void {
      if (isTypingTarget(e.target) || e.ctrlKey || e.metaKey || e.altKey) return

      switch (e.key) {
        case 'j':
          e.preventDefault()
          moveFocus(1)
          break
        case 'J':
          // Once j has walked to the last entry, moveFocus(1) is a no-op --
          // Shift+J there instead moves on to the next feed (s), which
          // skips fully-read feeds (see adjacentFeedId), so reading through
          // a feed and continuing to the next unread one can stay on one
          // key. Before the last entry it behaves just like j.
          e.preventDefault()
          if (isAtLastVisibleEntry()) {
            goToNextFeed()
          } else {
            moveFocus(1)
          }
          break
        case 'k':
          e.preventDefault()
          moveFocus(-1)
          break
        case ' ':
          e.preventDefault()
          scrollEntryPane(e.shiftKey ? -1 : 1)
          break
        case 's':
          e.preventDefault()
          goToNextFeed()
          break
        case 'a':
          e.preventDefault()
          goToPreviousFeed()
          break
        case '+':
          e.preventDefault()
          if (selectedFeedId.value !== null)
            void adjustRating(selectedFeedId.value, 1)
          break
        case '-':
          e.preventDefault()
          if (selectedFeedId.value !== null)
            void adjustRating(selectedFeedId.value, -1)
          break
        case 'v': {
          e.preventDefault()
          const entry = entries.value[focusedIndex.value]
          if (entry) window.open(entry.url, '_blank', 'noopener,noreferrer')
          break
        }
        case 'r':
          e.preventDefault()
          void refreshCurrentFeed()
          break
        case '?':
          e.preventDefault()
          helpOpen.value = !helpOpen.value
          break
        case 'p':
          e.preventDefault()
          void togglePinFocused()
          break
        case 'o':
          e.preventDefault()
          pinsOpen.value = true
          break
        case '/':
          e.preventDefault()
          openSearch()
          break
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])
}
