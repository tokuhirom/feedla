import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import { AddSubscriptionDialog } from './components/AddSubscriptionDialog'
import { AdminOverlay } from './components/AdminOverlay'
import { EntryPane } from './components/EntryPane'
import { FeedDetailOverlay } from './components/FeedDetailOverlay'
import { HelpOverlay } from './components/HelpOverlay'
import { IgnoreWordsOverlay } from './components/IgnoreWordsOverlay'
import { InviteAcceptScreen } from './components/InviteAcceptScreen'
import { LoginScreen } from './components/LoginScreen'
import { PinsOverlay } from './components/PinsOverlay'
import { SetupScreen } from './components/SetupScreen'
import { Sidebar } from './components/Sidebar'
import { StatsOverlay } from './components/StatsOverlay'
import { Toast } from './components/Toast'
import { useKeyboardShortcuts } from './keyboard/useKeyboardShortcuts'
import { authState, checkAuth } from './state/auth'
import { loadEntries } from './state/entries'
import { loadScrapeSources } from './state/scrapeSources'
import { loadStats } from './state/stats'
import {
  feedManagerMode,
  groupTarget,
  loadSubscriptions,
  searchMode,
  selectedFeedId,
} from './state/subscriptions'
import './styles/global.css'

function App() {
  useEffect(() => {
    void loadSubscriptions()
    // Loaded eagerly (not just when StatsOverlay opens) so the sidebar's
    // internal-error badge (see Sidebar.tsx) can show up without the user
    // having to open クロール状況 first -- otherwise a feedla-side crawl
    // failure (see crawler.go's InternalErrorEntry) would be invisible
    // unless someone happened to go looking for it.
    void loadStats()
    // Loaded eagerly too, same reasoning as loadStats: EntryItem needs to
    // know a pagewatch feed's scrape_sources.id up front to offer the
    // "このブロックを無視する" button (§9.4) without an extra round trip per
    // entry render.
    void loadScrapeSources()
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
      searchMode.value = false
      feedManagerMode.value = false
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
    selectedFeedId.value !== null ||
    groupTarget.value !== null ||
    searchMode.value ||
    feedManagerMode.value
      ? 'app-layout has-selected-feed'
      : 'app-layout'

  return (
    <div class={layoutClass}>
      <Sidebar />
      <EntryPane />
      <HelpOverlay />
      <AddSubscriptionDialog />
      <PinsOverlay />
      <FeedDetailOverlay />
      <StatsOverlay />
      <IgnoreWordsOverlay />
      <AdminOverlay />
      <Toast />
    </div>
  )
}

function Root() {
  useEffect(() => {
    void checkAuth()
  }, [])

  switch (authState.value.status) {
    case 'loading':
      return null
    case 'setup':
      return <SetupScreen />
    case 'login':
      return <LoginScreen />
    case 'invite':
      return <InviteAcceptScreen token={authState.value.token} />
    case 'authenticated':
      return <App />
  }
}

render(<Root />, document.getElementById('app')!)
