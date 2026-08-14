import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import { AddSubscriptionDialog } from './components/AddSubscriptionDialog'
import { EntryPane } from './components/EntryPane'
import { ErrorFeedsOverlay } from './components/ErrorFeedsOverlay'
import { FeedDetailOverlay } from './components/FeedDetailOverlay'
import { HelpOverlay } from './components/HelpOverlay'
import { PinsOverlay } from './components/PinsOverlay'
import { SearchOverlay } from './components/SearchOverlay'
import { Sidebar } from './components/Sidebar'
import { StatsOverlay } from './components/StatsOverlay'
import { Toast } from './components/Toast'
import { useKeyboardShortcuts } from './keyboard/useKeyboardShortcuts'
import { loadSubscriptions, selectedFeedId } from './state/subscriptions'
import './styles/global.css'

function App() {
  useEffect(() => {
    void loadSubscriptions()
  }, [])
  useKeyboardShortcuts()

  // On narrow viewports the sidebar and entry pane are single-pane views
  // (see the max-width: 700px block in global.css); this class picks
  // which one is showing. Wide viewports show both regardless.
  const layoutClass = selectedFeedId.value !== null ? 'app-layout has-selected-feed' : 'app-layout'

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
      <Toast />
    </div>
  )
}

render(<App />, document.getElementById('app')!)
