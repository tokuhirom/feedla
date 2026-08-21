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
import { loadScrapeSources } from './state/scrapeSources'
import { loadStats } from './state/stats'
import {
  clearMobileBackPending,
  feedManagerMode,
  groupTarget,
  loadSubscriptions,
  searchMode,
  selectedFeedId,
} from './state/subscriptions'
import {
  hydrateSignalsFromLocation,
  loadDataForRoute,
  startUrlSync,
} from './state/url'
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

  // Unread counts (tab title, sidebar badges) otherwise only change when the
  // user manually reloads or takes an action that touches subscriptions --
  // this keeps them roughly in sync with feeds crawled in the background.
  useEffect(() => {
    const UNREAD_POLL_MS = 30 * 60 * 1000
    const timer = setInterval(() => void loadSubscriptions(), UNREAD_POLL_MS)
    return () => clearInterval(timer)
  }, [])
  useKeyboardShortcuts()

  // Restores screen state (selected feed/group/search/フィード管理 +
  // open overlays) from the URL on first load -- see state/url.ts -- and
  // keeps the URL in sync with that state from then on. Also listens for
  // the OS/browser back/forward gesture (including the mobile edge-swipe
  // pushMobileDetailNav sets up, see state/subscriptions.ts) and re-derives
  // screen state from wherever it lands, instead of navigating away from
  // feedla entirely.
  useEffect(() => {
    const initialRoute = hydrateSignalsFromLocation()
    const dispose = startUrlSync()
    void loadDataForRoute(initialRoute.view)
    return dispose
  }, [])

  useEffect(() => {
    const onPopState = () => {
      clearMobileBackPending()
      const route = hydrateSignalsFromLocation()
      void loadDataForRoute(route.view)
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
      return <SetupScreen restoreHint={authState.value.restoreHint} />
    case 'login':
      return <LoginScreen />
    case 'invite':
      return <InviteAcceptScreen token={authState.value.token} />
    case 'authenticated':
      return <App />
  }
}

render(<Root />, document.getElementById('app')!)
