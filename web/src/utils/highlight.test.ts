import { describe, expect, it } from 'vitest'
import { highlightSegments } from './highlight'

describe('highlightSegments', () => {
  it('returns the whole text unmatched when the query is empty', () => {
    expect(highlightSegments('hello world', '')).toEqual([
      { text: 'hello world', match: false },
    ])
  })

  it('returns the whole text unmatched when the query is only whitespace', () => {
    expect(highlightSegments('hello world', '   ')).toEqual([
      { text: 'hello world', match: false },
    ])
  })

  it('splits a single match into before/match/after segments', () => {
    expect(highlightSegments('hello world', 'world')).toEqual([
      { text: 'hello ', match: false },
      { text: 'world', match: true },
    ])
  })

  it('matches case-insensitively', () => {
    expect(highlightSegments('Hello World', 'world')).toEqual([
      { text: 'Hello ', match: false },
      { text: 'World', match: true },
    ])
  })

  it('matches every occurrence', () => {
    expect(highlightSegments('foo bar foo', 'foo')).toEqual([
      { text: 'foo', match: true },
      { text: ' bar ', match: false },
      { text: 'foo', match: true },
    ])
  })

  it('escapes regexp metacharacters in the query', () => {
    expect(highlightSegments('a.b', '.')).toEqual([
      { text: 'a', match: false },
      { text: '.', match: true },
      { text: 'b', match: false },
    ])
  })

  it('returns the whole text unmatched when there is no match', () => {
    expect(highlightSegments('hello world', 'xyz')).toEqual([
      { text: 'hello world', match: false },
    ])
  })
})
