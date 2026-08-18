import { describe, expect, it } from 'vitest'
import { buildIgnorePattern } from './ignorePattern'

describe('buildIgnorePattern', () => {
  it('trims surrounding whitespace', () => {
    expect(buildIgnorePattern('  hello world  ')).toBe('hello world')
  })

  it('escapes regexp metacharacters', () => {
    expect(buildIgnorePattern('a.b*c?d')).toBe('a\\.b\\*c\\?d')
  })

  it('collapses digit runs to \\d+ so dates/counters still match next time', () => {
    expect(buildIgnorePattern('2026-08-16 更新')).toBe('\\d+-\\d+-\\d+ 更新')
  })

  it('produces a pattern that actually matches a re-diffed block with a different counter', () => {
    const pattern = buildIgnorePattern('閲覧数: 123')
    const re = new RegExp(pattern)
    expect(re.test('閲覧数: 456')).toBe(true)
  })

  it('truncates to MAX_IGNORE_PATTERN_LEN (1000) chars', () => {
    const text = 'a'.repeat(2000)
    expect(buildIgnorePattern(text).length).toBe(1000)
  })
})
