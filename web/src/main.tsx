import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import { AddSubscriptionDialog } from './components/AddSubscriptionDialog'
import { EntryPane } from './components/EntryPane'
import { HelpOverlay } from './components/HelpOverlay'
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
      <Toast />
    </div>
  )
}

render(<App />, document.getElementById('app')!)
