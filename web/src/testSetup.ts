import '@testing-library/jest-dom/vitest'

// jsdom doesn't implement matchMedia -- several components/state modules
// (mobile breakpoint checks, EntryItem's collapse threshold) call it
// unconditionally, so without this every such call throws "Not
// implemented: window.matchMedia" inside jsdom.
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
}
