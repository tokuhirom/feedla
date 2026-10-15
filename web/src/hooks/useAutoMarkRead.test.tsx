// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { markReadOptimistic } from '../state/entries'
import { useAutoMarkRead } from './useAutoMarkRead'

vi.mock('../state/entries', () => ({
  markReadOptimistic: vi.fn(),
}))

class FakeIntersectionObserver {
  observe(): void {}
  disconnect(): void {}
}

function Harness({ ids }: { ids: number[] }) {
  useAutoMarkRead(ids)
  return null
}

/** Builds the .entry-pane the hook attaches to, with the given rendered
 * entries and fixed scroll geometry (jsdom does no layout). */
function mountPane(
  renderedIds: number[],
  geometry: { scrollTop: number; clientHeight: number; scrollHeight: number },
): HTMLElement {
  const pane = document.createElement('section')
  pane.className = 'entry-pane'
  for (const id of renderedIds) {
    const el = document.createElement('article')
    el.className = 'entry-item'
    el.dataset.entryId = String(id)
    pane.appendChild(el)
  }
  for (const [key, value] of Object.entries(geometry)) {
    Object.defineProperty(pane, key, { configurable: true, value })
  }
  document.body.appendChild(pane)
  return pane
}

describe('useAutoMarkRead', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  })

  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
    vi.mocked(markReadOptimistic).mockClear()
  })

  it('marks the entries rendered at scroll time, not the ids from an earlier render', () => {
    // The effect last ran for feed C's list ([30]), but the DOM has since
    // moved on to feed B ([21, 22]) -- e.g. `a`'s restore scroll firing
    // before the effect re-runs. Reaching the bottom must not mark C.
    mountPane([21, 22], {
      scrollTop: 600,
      clientHeight: 400,
      scrollHeight: 1000,
    })
    render(<Harness ids={[30]} />)

    document.querySelector('.entry-pane')!.dispatchEvent(new Event('scroll'))

    const marked = vi.mocked(markReadOptimistic).mock.calls.map((c) => c[0])
    expect(marked).toEqual([21, 22])
  })

  it('ignores a scroll event on a pane whose content fits (a clamped leftover scrollTop)', () => {
    mountPane([30], { scrollTop: 0, clientHeight: 400, scrollHeight: 400 })
    render(<Harness ids={[30]} />)

    document.querySelector('.entry-pane')!.dispatchEvent(new Event('scroll'))

    expect(markReadOptimistic).not.toHaveBeenCalled()
  })

  it('still marks a pane whose content fits on an actual touch swipe', () => {
    mountPane([30], { scrollTop: 0, clientHeight: 400, scrollHeight: 400 })
    render(<Harness ids={[30]} />)

    document.querySelector('.entry-pane')!.dispatchEvent(new Event('touchmove'))

    expect(markReadOptimistic).toHaveBeenCalledWith(30)
  })

  it('does nothing when the scrolled pane is not at the bottom yet', () => {
    mountPane([21, 22], {
      scrollTop: 100,
      clientHeight: 400,
      scrollHeight: 1000,
    })
    render(<Harness ids={[21, 22]} />)

    document.querySelector('.entry-pane')!.dispatchEvent(new Event('scroll'))

    expect(markReadOptimistic).not.toHaveBeenCalled()
  })
})
