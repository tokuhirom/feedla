// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import type { InspectElement } from '../api/types'
import {
  findContainingItem,
  generateItemSelectorCandidates,
  generateTitleSelectorCandidates,
  isStableClass,
} from './selectorGen'

// Builds a small InspectElement[] mimicking a <ul id="news-list"> of
// <li class="post">record N</li> items, each with an <h3 class="title">
// child -- the shape §10.6's worked example (`#news-list > li`) describes.
function newsListFixture(itemCount: number): InspectElement[] {
  const elements: InspectElement[] = [
    { id: 1, tag: 'ul', html_id: 'news-list', parent_id: 0 },
  ]
  let nextId = 2
  for (let i = 0; i < itemCount; i++) {
    const liId = nextId++
    elements.push({ id: liId, tag: 'li', classes: ['post'], parent_id: 1 })
    elements.push({
      id: nextId++,
      tag: 'h3',
      classes: ['title'],
      parent_id: liId,
    })
  }
  return elements
}

describe('isStableClass', () => {
  it('keeps author-written class names', () => {
    expect(isStableClass('post')).toBe(true)
    expect(isStableClass('article-title')).toBe(true)
  })

  it('rejects purely numeric classes', () => {
    expect(isStableClass('123')).toBe(false)
  })

  it('rejects long hex-like hashes', () => {
    expect(isStableClass('a1b2c3d4e5')).toBe(false)
  })

  it('rejects CSS-module-style word+hash suffixes', () => {
    expect(isStableClass('Button_1a2b3c')).toBe(false)
    expect(isStableClass('css-1a2b3c4d')).toBe(false)
  })
})

describe('generateItemSelectorCandidates', () => {
  it('finds the repeating <li> as an item, scoped under its unique parent', () => {
    const elements = newsListFixture(5)
    // click on the 3rd <li> (id 6: 1=ul,2=li,3=h3,4=li,5=h3,6=li,...)
    const candidates = generateItemSelectorCandidates(elements, 6)

    expect(candidates.length).toBeGreaterThan(0)
    const best = candidates[0]
    expect(best.selector).toBe('#news-list > li.post')
    expect(best.matchCount).toBe(5)
    expect(best.matchedIds).toEqual([2, 4, 6, 8, 10])
  })

  it('drops candidates that match only 1 element', () => {
    // A single <li> with no repeating siblings anywhere in the document.
    const elements: InspectElement[] = [
      { id: 1, tag: 'div', parent_id: 0 },
      { id: 2, tag: 'li', classes: ['post'], parent_id: 1 },
    ]
    const candidates = generateItemSelectorCandidates(elements, 2)
    expect(candidates.every((c) => c.matchCount > 1)).toBe(true)
  })

  it('falls back to a document-wide signature match for grid layouts (items are not literal siblings)', () => {
    // Three <div class="row"> each wrapping one <article class="card">,
    // so the cards never share a parent -- §10.6's grid-layout fallback.
    const elements: InspectElement[] = []
    let nextId = 1
    const rootId = nextId++
    elements.push({ id: rootId, tag: 'section', parent_id: 0 })
    const cardIds: number[] = []
    for (let i = 0; i < 3; i++) {
      const rowId = nextId++
      elements.push({
        id: rowId,
        tag: 'div',
        classes: ['row'],
        parent_id: rootId,
      })
      const cardId = nextId++
      elements.push({
        id: cardId,
        tag: 'article',
        classes: ['card'],
        parent_id: rowId,
      })
      cardIds.push(cardId)
    }

    const candidates = generateItemSelectorCandidates(elements, cardIds[1])
    const cardCandidate = candidates.find((c) => c.selector.includes('article'))
    expect(cardCandidate).toBeDefined()
    expect(cardCandidate?.matchCount).toBe(3)
    expect(cardCandidate?.matchedIds).toEqual(cardIds)
  })

  it('returns no candidates for an unknown clicked id', () => {
    expect(generateItemSelectorCandidates(newsListFixture(3), 9999)).toEqual([])
  })

  // Mimics an old-school archive page: a nav menu of nested class-less
  // <ul>/<li>/<a>, a calendar-style table full of <td><a>, and the actual
  // listing as a class-less, id-less <ul> sitting directly under <body>.
  // The listing shares its structural signature with the nav, so only a
  // body-anchored container ("body > ul") can single it out.
  function archiveFixture(): {
    elements: InspectElement[]
    listingLiIds: number[]
    clickedLinkId: number
  } {
    const elements: InspectElement[] = []
    let nextId = 1
    // nav: <div class="nav"> holding two <ul>s of 5 <li><a> each
    const navDiv = nextId++
    elements.push({ id: navDiv, tag: 'div', classes: ['nav'], parent_id: 0 })
    for (let u = 0; u < 2; u++) {
      const ul = nextId++
      elements.push({ id: ul, tag: 'ul', parent_id: navDiv })
      for (let i = 0; i < 5; i++) {
        const li = nextId++
        elements.push({ id: li, tag: 'li', parent_id: ul })
        elements.push({ id: nextId++, tag: 'a', parent_id: li })
      }
    }
    // calendar: <table> of 3 rows x 6 <td><a>
    const table = nextId++
    elements.push({ id: table, tag: 'table', parent_id: 0 })
    for (let r = 0; r < 3; r++) {
      const tr = nextId++
      elements.push({ id: tr, tag: 'tr', parent_id: table })
      for (let c = 0; c < 6; c++) {
        const td = nextId++
        elements.push({ id: td, tag: 'td', parent_id: tr })
        elements.push({ id: nextId++, tag: 'a', parent_id: td })
      }
    }
    // the listing: a bare <ul> directly under body, 8 <li><a> items
    const listingUl = nextId++
    elements.push({ id: listingUl, tag: 'ul', parent_id: 0 })
    const listingLiIds: number[] = []
    let clickedLinkId = 0
    for (let i = 0; i < 8; i++) {
      const li = nextId++
      elements.push({ id: li, tag: 'li', parent_id: listingUl })
      const a = nextId++
      elements.push({ id: a, tag: 'a', parent_id: li })
      listingLiIds.push(li)
      if (i === 2) clickedLinkId = a
    }
    return { elements, listingLiIds, clickedLinkId }
  }

  it('anchors a class-less top-level listing to body instead of a non-unique :nth-of-type container', () => {
    const { elements, listingLiIds, clickedLinkId } = archiveFixture()
    const candidates = generateItemSelectorCandidates(elements, clickedLinkId)

    const best = candidates[0]
    expect(best.selector).toBe('body > ul > li')
    expect(best.matchCount).toBe(8)
    expect(best.matchedIds).toEqual(listingLiIds)
    // The pre-body-anchor failure mode: an unverified nth container that
    // also matched the nav's <ul>s must not come back.
    expect(candidates.some((c) => c.selector.includes(':nth-of-type'))).toBe(
      false,
    )
  })

  it('ranks sibling-repeat candidates above larger document-wide fallbacks', () => {
    const { elements, clickedLinkId } = archiveFixture()
    const candidates = generateItemSelectorCandidates(elements, clickedLinkId)

    // The bare "a" fallback matches every link on the page (nav + calendar
    // + listing) and outnumbers the listing candidate, but must not outrank
    // it.
    const bareLink = candidates.find((c) => c.selector === 'a')
    expect(bareLink).toBeDefined()
    expect(bareLink!.matchCount).toBeGreaterThan(8)
    expect(candidates.indexOf(bareLink!)).toBeGreaterThan(
      candidates.findIndex((c) => c.selector === 'body > ul > li'),
    )
  })

  it('still offers a candidate for a 2-item list (below the scoring threshold of 3)', () => {
    const elements = newsListFixture(2)
    const candidates = generateItemSelectorCandidates(elements, 2)
    const best = candidates.find((c) => c.selector === '#news-list > li.post')
    expect(best?.matchCount).toBe(2)
  })
})

describe('generateTitleSelectorCandidates', () => {
  it('generates a relative selector unique within the item', () => {
    const elements = newsListFixture(5)
    // item element id 6 (the 3rd <li>), title element id 7 (its <h3>)
    const candidates = generateTitleSelectorCandidates(elements, 6, 7)
    expect(candidates).toHaveLength(1)
    expect(candidates[0].selector).toBe('h3.title')
    expect(candidates[0].matchCount).toBe(1)
  })

  it('falls back to :nth-of-type when the title element repeats inside the item', () => {
    const elements: InspectElement[] = [
      { id: 1, tag: 'li', parent_id: 0 },
      { id: 2, tag: 'span', classes: ['tag'], parent_id: 1 },
      { id: 3, tag: 'span', classes: ['tag'], parent_id: 1 },
    ]
    const candidates = generateTitleSelectorCandidates(elements, 1, 3)
    expect(candidates).toHaveLength(1)
    expect(candidates[0].selector).toBe('span.tag:nth-of-type(2)')
    expect(candidates[0].matchCount).toBe(1)
  })

  it('returns nothing for a click outside the item subtree', () => {
    const elements = newsListFixture(5)
    // item id 2 (1st <li>), clicked id 7 belongs to a different <li> (3rd)
    expect(generateTitleSelectorCandidates(elements, 2, 7)).toEqual([])
  })

  it('returns nothing when the click is the item element itself', () => {
    const elements = newsListFixture(5)
    expect(generateTitleSelectorCandidates(elements, 2, 2)).toEqual([])
  })
})

describe('generated selectors are syntactically valid CSS (escaping)', () => {
  it('escapes special characters in class names (e.g. Tailwind-style "md:flex") so the selector is usable', () => {
    const elements: InspectElement[] = [
      { id: 1, tag: 'ul', html_id: 'list:main', parent_id: 0 },
      { id: 2, tag: 'li', classes: ['md:flex'], parent_id: 1 },
      { id: 3, tag: 'li', classes: ['md:flex'], parent_id: 1 },
      { id: 4, tag: 'li', classes: ['md:flex'], parent_id: 1 },
    ]
    const candidates = generateItemSelectorCandidates(elements, 2)
    expect(candidates.length).toBeGreaterThan(0)
    const selector = candidates[0].selector

    // Build a real DOM mirroring the structure above and confirm the
    // generated selector string is both syntactically valid and matches
    // exactly the elements it claims to -- a raw, unescaped Tailwind-style
    // class here would otherwise be a likely source of selectors the
    // server's cascadia.Compile rejects as invalid.
    document.body.innerHTML = ''
    const ul = document.createElement('ul')
    ul.id = 'list:main'
    for (let i = 0; i < 3; i++) {
      const li = document.createElement('li')
      li.className = 'md:flex'
      ul.appendChild(li)
    }
    document.body.appendChild(ul)

    expect(document.querySelectorAll(selector).length).toBe(3)
  })

  it('escapes an id starting with a digit', () => {
    const elements: InspectElement[] = [
      { id: 1, tag: 'div', html_id: '1st-section', parent_id: 0 },
      { id: 2, tag: 'li', parent_id: 1 },
      { id: 3, tag: 'li', parent_id: 1 },
    ]
    const candidates = generateItemSelectorCandidates(elements, 2)
    const selector = candidates[0].selector

    document.body.innerHTML = ''
    const div = document.createElement('div')
    div.id = '1st-section'
    div.appendChild(document.createElement('li'))
    div.appendChild(document.createElement('li'))
    document.body.appendChild(div)

    expect(document.querySelectorAll(selector).length).toBe(2)
  })
})

describe('findContainingItem', () => {
  it('finds which matched item id a click belongs to', () => {
    const elements = newsListFixture(5)
    const matchedIds = [2, 4, 6, 8, 10]
    expect(findContainingItem(elements, matchedIds, 7)).toBe(6)
    expect(findContainingItem(elements, matchedIds, 6)).toBe(6)
  })

  it('returns null when the click is outside every matched item', () => {
    const elements: InspectElement[] = [
      { id: 1, tag: 'header', parent_id: 0 },
      { id: 2, tag: 'li', parent_id: 0 },
    ]
    expect(findContainingItem(elements, [2], 1)).toBeNull()
  })
})
