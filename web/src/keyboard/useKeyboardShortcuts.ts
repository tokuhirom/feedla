import { useEffect } from 'preact/hooks'
import { refreshCurrentFeed, selectAndLoadFeed, togglePinFocused } from '../state/actions'
import { entries, focusedIndex, moveFocus } from '../state/entries'
import { pinsOpen } from '../state/pins'
import { adjacentFeedId } from '../state/subscriptions'
import { helpOpen, searchOpen } from '../state/ui'

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable
}

function scrollEntryPane(direction: 1 | -1): void {
  document
    .querySelector('.entry-pane')
    ?.scrollBy({ top: direction * window.innerHeight * 0.8, behavior: 'smooth' })
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
        case 'k':
          e.preventDefault()
          moveFocus(-1)
          break
        case ' ':
          e.preventDefault()
          scrollEntryPane(e.shiftKey ? -1 : 1)
          break
        case 's': {
          e.preventDefault()
          const next = adjacentFeedId(1)
          if (next !== null) void selectAndLoadFeed(next)
          break
        }
        case 'a': {
          e.preventDefault()
          const prev = adjacentFeedId(-1)
          if (prev !== null) void selectAndLoadFeed(prev)
          break
        }
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
