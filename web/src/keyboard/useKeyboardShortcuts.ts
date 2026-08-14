import { useEffect } from 'preact/hooks'
import { refreshCurrentFeed, selectAndLoadFeed } from '../state/actions'
import { entries, focusedIndex, moveFocus } from '../state/entries'
import { adjacentFeedId } from '../state/subscriptions'
import { helpOpen, showToast } from '../state/ui'

// p/o//: keys have no backend yet (pins + FTS search land in Phase5 --
// see the Phase4 plan). Registering them with a toast beats silently
// ignoring the keypress: it tells the user the shortcut exists but isn't
// wired up yet, instead of looking broken.
const NOT_IMPLEMENTED: Record<string, string> = {
  p: 'pin (p) は Phase 5 で実装予定です',
  o: 'pin一覧 (o) は Phase 5 で実装予定です',
  '/': '検索 (/) は Phase 5 で実装予定です',
}

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
        case 'o':
        case '/':
          e.preventDefault()
          showToast(NOT_IMPLEMENTED[e.key])
          break
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])
}
