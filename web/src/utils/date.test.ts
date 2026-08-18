import { describe, expect, it } from 'vitest'
import { formatUnixSeconds } from './date'

describe('formatUnixSeconds', () => {
  it('formats a unix timestamp as locale-independent "YYYY-MM-DD HH:mm"', () => {
    const d = new Date(2020, 4, 25, 13, 25, 0)
    expect(formatUnixSeconds(Math.floor(d.getTime() / 1000))).toBe(
      '2020-05-25 13:25',
    )
  })

  it('zero-pads single-digit month, day, hour, and minute', () => {
    const d = new Date(2026, 0, 5, 3, 7, 0)
    expect(formatUnixSeconds(Math.floor(d.getTime() / 1000))).toBe(
      '2026-01-05 03:07',
    )
  })

  it('handles midnight', () => {
    const d = new Date(2026, 0, 1, 0, 0, 0)
    expect(formatUnixSeconds(Math.floor(d.getTime() / 1000))).toBe(
      '2026-01-01 00:00',
    )
  })
})
