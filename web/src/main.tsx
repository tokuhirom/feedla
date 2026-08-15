import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import { AddSubscriptionDialog } from './components/AddSubscriptionDialog'
import { EntryPane } from './components/EntryPane'
import { ErrorFeedsOverlay } from './components/ErrorFeedsOverlay'
import { FeedDetailOverlay } from './components/FeedDetailOverlay'
import { HelpOverlay } from './components/HelpOverlay'
import { IgnoreWordsOverlay } from './components/IgnoreWordsOverlay'
import { PinsOverlay } from './components/PinsOverlay'
import { SearchOverlay } from './components/SearchOverlay'
import { Sidebar } from './components/Sidebar'
import { StatsOverlay } from './components/StatsOverlay'
import { Toast } from './components/Toast'
import { useKeyboardShortcuts } from './keyboard/useKeyboardShortcuts'
import { loadEntries } from './state/entries'
import {
  groupTarget,
  loadSubscriptions,
  selectedFeedId,
} from './state/subscriptions'
import './styles/global.css'

function App() {
  useEffect(() => {
    void loadSubscriptions()
  }, [])
  useKeyboardShortcuts()

  // Establishes a base history entry so the first pushMobileDetailNav()
  // push (see state/subscriptions.ts) has something to pop back to, and
  // listens for the OS/browser back gesture popping that entry -- without
  // this, a mobile edge-swipe back from the entry pane has no in-app
  // history entry to land on and instead navigates away from feedla
  // entirely.
  useEffect(() => {
    window.history.replaceState({ feedId: null }, '')
    const onPopState = (event: PopStateEvent) => {
      const feedId =
        (event.state as { feedId: number | null } | null)?.feedId ?? null
      groupTarget.value = null
      selectedFeedId.value = feedId
      if (feedId !== null) void loadEntries(feedId)
    }
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  // On narrow viewports the sidebar and entry pane are single-pane views
  // (see the max-width: 700px block in global.css); this class picks
  // which one is showing. Wide viewports show both regardless.
  const layoutClass =
    selectedFeedId.value !== null || groupTarget.value !== null
      ? 'app-layout has-selected-feed'
      : 'app-layout'

  return (
    <div class={layoutClass}>
      <Sidebar />
      <EntryPane />
      <HelpOverlay />
      <AddSubscriptionDialog />
      <PinsOverlay />
      <SearchOverlay />
      <ErrorFeedsOverlay />
      <FeedDetailOverlay />
      <StatsOverlay />
      <IgnoreWordsOverlay />
      <Toast />
    </div>
  )
}

render(<App />, document.getElementById('app')!)
