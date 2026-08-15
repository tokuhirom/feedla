import { useEffect } from 'preact/hooks'
import {
  adjustRating,
  refreshCurrentFeed,
  selectAndLoadFeed,
  togglePinFocused,
} from '../state/actions'
import {
  entries,
  focusedIndex,
  isAtLastVisibleEntry,
  moveFocus,
} from '../state/entries'
import { pinsOpen } from '../state/pins'
import { adjacentFeedId, selectedFeedId } from '../state/subscriptions'
import { helpOpen, searchOpen } from '../state/ui'

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

function goToNextFeed(): void {
  const next = adjacentFeedId(1)
  if (next !== null) void selectAndLoadFeed(next)
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
        case 'a': {
          e.preventDefault()
          const prev = adjacentFeedId(-1)
          if (prev !== null) void selectAndLoadFeed(prev)
          break
        }
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
          searchOpen.value = true
          break
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])
}
