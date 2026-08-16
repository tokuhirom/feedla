import { useEffect, useRef, useState } from 'preact/hooks'
import { runSearch } from '../state/actions'
import { loadingEntries } from '../state/entries'
import { clearSelectedFeed, searchQuery } from '../state/subscriptions'

/** Replaces the normal per-feed Header while searchMode is on -- entries
 * (search results) render below it exactly like a feed/group's, via
 * EntryPane's search branch. */
export function SearchHeader() {
  const [text, setText] = useState(searchQuery.value)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  return (
    <header class="entry-header search-header">
      <button
        type="button"
        class="back-button"
        title="購読一覧へ戻る"
        onClick={() => clearSelectedFeed()}
      >
        ‹
      </button>
      <form
        class="search-header-form"
        onSubmit={(e) => {
          e.preventDefault()
          void runSearch(text)
        }}
      >
        <input
          ref={inputRef}
          type="text"
          placeholder="キーワードで記事を検索"
          value={text}
          onInput={(e) => setText((e.target as HTMLInputElement).value)}
        />
        <button
          type="submit"
          disabled={loadingEntries.value || text.trim() === ''}
        >
          {loadingEntries.value ? '検索中…' : '検索'}
        </button>
      </form>
    </header>
  )
}
