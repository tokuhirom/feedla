import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import { AddSubscriptionDialog } from './components/AddSubscriptionDialog'
import { EntryPane } from './components/EntryPane'
import { ErrorFeedsOverlay } from './components/ErrorFeedsOverlay'
import { HelpOverlay } from './components/HelpOverlay'
import { PinsOverlay } from './components/PinsOverlay'
import { SearchOverlay } from './components/SearchOverlay'
import { Sidebar } from './components/Sidebar'
import { Toast } from './components/Toast'
import { useKeyboardShortcuts } from './keyboard/useKeyboardShortcuts'
import { loadSubscriptions } from './state/subscriptions'
import './styles/global.css'

function App() {
  useEffect(() => {
    void loadSubscriptions()
  }, [])
  useKeyboardShortcuts()

  return (
    <div class="app-layout">
      <Sidebar />
      <EntryPane />
      <HelpOverlay />
      <AddSubscriptionDialog />
      <PinsOverlay />
      <SearchOverlay />
      <ErrorFeedsOverlay />
      <Toast />
    </div>
  )
}

render(<App />, document.getElementById('app')!)
