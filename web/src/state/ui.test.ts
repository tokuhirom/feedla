import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { showErrorToast, showToast, toast } from './ui'

beforeEach(() => {
  vi.useFakeTimers()
  toast.value = null
})

afterEach(() => {
  vi.useRealTimers()
})

describe('showToast', () => {
  it('sets an info toast', () => {
    showToast('saved')
    expect(toast.value).toEqual({ message: 'saved', variant: 'info' })
  })

  it('clears the toast after the default duration', () => {
    showToast('saved')
    vi.advanceTimersByTime(2499)
    expect(toast.value).not.toBeNull()
    vi.advanceTimersByTime(1)
    expect(toast.value).toBeNull()
  })

  it('clears after a custom duration', () => {
    showToast('saved', 1000)
    vi.advanceTimersByTime(999)
    expect(toast.value).not.toBeNull()
    vi.advanceTimersByTime(1)
    expect(toast.value).toBeNull()
  })

  it('restarts the timer when called again before the previous one fires', () => {
    showToast('first', 1000)
    vi.advanceTimersByTime(900)
    showToast('second', 1000)
    // The first call's timer would have fired here if it hadn't been reset.
    vi.advanceTimersByTime(200)
    expect(toast.value).toEqual({ message: 'second', variant: 'info' })
    vi.advanceTimersByTime(800)
    expect(toast.value).toBeNull()
  })
})

describe('showErrorToast', () => {
  it('sets an error-variant toast with a longer default duration', () => {
    showErrorToast('failed')
    expect(toast.value).toEqual({ message: 'failed', variant: 'error' })
    vi.advanceTimersByTime(3999)
    expect(toast.value).not.toBeNull()
    vi.advanceTimersByTime(1)
    expect(toast.value).toBeNull()
  })
})
