// @vitest-environment jsdom
import { cleanup, renderHook } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'

// `R` reloads the whole page while `r` only re-crawls the current feed --
// two very different costs behind the same physical key, and the only thing
// separating them is a case-sensitive switch label that no type error would
// catch if it regressed.
describe('useKeyboardShortcuts: R (full reload)', () => {
  let reload: ReturnType<typeof vi.fn>

  beforeEach(() => {
    reload = vi.fn()
    // jsdom marks window.location [Unforgeable], so vi.spyOn(location,
    // 'reload') throws "Cannot redefine property" -- replacing the whole
    // object via stubGlobal is the way through. Nothing under test reads
    // any other location property.
    vi.stubGlobal('location', { reload })
    renderHook(() => useKeyboardShortcuts())
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  function press(key: string, target: EventTarget = document.body): void {
    const event = new KeyboardEvent('keydown', {
      key,
      bubbles: true,
      cancelable: true,
    })
    target.dispatchEvent(event)
  }

  it('reloads the page on R', () => {
    press('R')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not reload on lowercase r', () => {
    press('r')
    expect(reload).not.toHaveBeenCalled()
  })

  it('does not reload while typing in an input', () => {
    const input = document.createElement('input')
    document.body.appendChild(input)
    press('R', input)
    input.remove()
    expect(reload).not.toHaveBeenCalled()
  })

  it('does not reload on ctrl/meta/alt + R', () => {
    for (const modifier of ['ctrlKey', 'metaKey', 'altKey'] as const) {
      document.body.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'R',
          [modifier]: true,
          bubbles: true,
          cancelable: true,
        }),
      )
    }
    expect(reload).not.toHaveBeenCalled()
  })
})
