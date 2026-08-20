// Phase F2's click-to-selector algorithm (design doc §10.6). Pure and
// DOM-free: it only ever sees the server-computed structural index
// (InspectElement[] -- tag, filtered classes, original id, parent linkage)
// returned by POST /scrape_sources/inspect, never anything read out of the
// sandboxed iframe itself. The iframe can only ever hand the picker a
// clicked element's integer id (see components/SelectorPicker.tsx); every
// selector string here is reconstructed server-side-data-only from that id.
//
// Generated selectors are a convenience, not an authority: the caller still
// has to run them through the existing preview flow (server-side cascadia
// compile + real match) before subscribing (§10.6, §10.7).
import type { InspectElement } from '../api/types'

export interface SelectorCandidate {
  selector: string
  matchCount: number
  // ids of every element the selector matches, in document order -- the
  // title-selector step uses this to figure out which matched item a
  // second click landed inside, without re-deriving it from the string.
  matchedIds: number[]
}

interface Index {
  byId: Map<number, InspectElement>
  children: Map<number, InspectElement[]>
  ordered: InspectElement[]
}

function buildIndex(elements: InspectElement[]): Index {
  const byId = new Map<number, InspectElement>()
  const children = new Map<number, InspectElement[]>()
  for (const el of elements) {
    byId.set(el.id, el)
  }
  for (const el of elements) {
    const list = children.get(el.parent_id)
    if (list) list.push(el)
    else children.set(el.parent_id, [el])
  }
  return { byId, children, ordered: elements }
}

// §10.6 step 4: classes that look generated rather than author-written --
// long hex-like hashes, pure digits, and the "word-<hash>" shape common to
// CSS Modules / CSS-in-JS output (e.g. "Button_1a2b3c", "sc-bZQltZ" is not
// caught by this heuristic and is an accepted false negative; erring
// towards keeping a class is safer than erring towards a selector that
// silently 0-matches after the next build).
const DIGITS_ONLY = /^\d+$/
const HEX_LIKE = /^[0-9a-f]{6,}$/i
const HASH_SUFFIX = /^[a-zA-Z][\w]*[-_][0-9a-f]{5,}$/

export function isStableClass(cls: string): boolean {
  if (cls === '') return false
  if (DIGITS_ONLY.test(cls)) return false
  if (HEX_LIKE.test(cls)) return false
  if (HASH_SUFFIX.test(cls)) return false
  return true
}

function stableClasses(el: InspectElement): string[] {
  return (el.classes ?? []).filter(isStableClass).sort()
}

// tag + sorted stable classes -- two elements share a "structural
// signature" if they'd plausibly come from the same template.
function signature(el: InspectElement): string {
  return `${el.tag}.${stableClasses(el).join('.')}`
}

interface Compound {
  tag?: string
  classes: string[]
  id?: string
  nth?: number // 1-indexed position among same-tag siblings; container-only (§10.6 step 2)
  // Chain-head marker for the document body. The element index has no
  // entry for <body> itself (ParentID 0 means "no surviving ancestor"),
  // but "no surviving ancestor" is equivalent to "direct child of body":
  // Sanitize drops a disallowed tag's whole subtree, so any element whose
  // real parent was dropped would have been dropped with it. A body
  // compound may only ever appear as chain[0] and matches structurally
  // (chainMatches), not against any indexed element.
  body?: true
}

// Minimal CSS.escape-alike for the identifiers found in class/id
// attributes: escapes a leading digit and any character outside
// [a-zA-Z0-9_-] one at a time. Not a full CSSOM implementation, but
// sufficient for values that reach the server's cascadia.Compile, which is
// the actual authority (§10.6's algorithm is a convenience generator).
function escapeIdent(value: string): string {
  let out = ''
  for (let i = 0; i < value.length; i++) {
    const ch = value[i]
    const code = value.charCodeAt(i)
    if (i === 0 && code >= 48 && code <= 57) {
      out += `\\3${ch} `
      continue
    }
    out += /[a-zA-Z0-9_-]/.test(ch) ? ch : `\\${ch}`
  }
  return out
}

function compoundToCSS(c: Compound): string {
  if (c.body) return 'body'
  let s: string
  if (c.id != null) {
    s = `#${escapeIdent(c.id)}`
  } else {
    s = c.tag ?? '*'
    for (const cls of c.classes) s += `.${escapeIdent(cls)}`
  }
  if (c.nth != null) s += `:nth-of-type(${c.nth})`
  return s
}

function compoundMatches(el: InspectElement, c: Compound, idx: Index): boolean {
  if (c.id != null) return el.html_id === c.id
  if (c.tag != null && el.tag !== c.tag) return false
  const classes = stableClasses(el)
  if (!c.classes.every((cl) => classes.includes(cl))) return false
  if (c.nth != null) {
    const siblings = idx.children.get(el.parent_id) ?? []
    const sameTag = siblings.filter((s) => s.tag === el.tag)
    const pos = sameTag.findIndex((s) => s.id === el.id) + 1
    if (pos !== c.nth) return false
  }
  return true
}

// chain is root-to-leaf, joined by strict child (">") combinators: the
// last compound must match el itself, and each earlier compound must match
// el's direct ancestor that many levels up, with no gaps -- which holds
// because internal/inspect.Sanitize drops a disallowed tag's whole
// subtree rather than unwrapping it (docs/feedless-site-subscription-selector.md
// §10.4), so ParentID always names the true rendered parent.
function chainMatches(
  el: InspectElement,
  chain: Compound[],
  idx: Index,
): boolean {
  if (chain.length === 0) return true
  if (!compoundMatches(el, chain[chain.length - 1], idx)) return false
  let cur = el
  for (let i = chain.length - 2; i >= 0; i--) {
    if (chain[i].body) {
      // Only ever chain[0]: cur must sit directly under <body>, i.e. have
      // no surviving ancestor in the index (see Compound.body).
      return idx.byId.get(cur.parent_id) === undefined
    }
    const parent = idx.byId.get(cur.parent_id)
    if (!parent || !compoundMatches(parent, chain[i], idx)) return false
    cur = parent
  }
  return true
}

function matchElements(chain: Compound[], idx: Index): InspectElement[] {
  if (chain.length === 0) return []
  return idx.ordered.filter((el) => chainMatches(el, chain, idx))
}

function toCandidate(chain: Compound[], idx: Index): SelectorCandidate {
  const matched = matchElements(chain, idx)
  return {
    selector: chain.map(compoundToCSS).join(' > '),
    matchCount: matched.length,
    matchedIds: matched.map((e) => e.id),
  }
}

// Builds a chain of compounds that uniquely identifies container within
// the whole document: #id -> tag.class -> widening by one real ancestor
// level at a time -> anchoring to <body> when the walk reaches the top ->
// nth-of-type on the outermost level reached (§10.6 step 2 -- nth-of-type
// is only ever used for the container side, and only when everything else
// failed: an nth compound matched mid-document is not guaranteed unique,
// so the body-anchored form is preferred wherever it applies).
function buildUniqueAncestorChain(
  container: InspectElement,
  idx: Index,
): Compound[] {
  if (container.html_id) {
    const idChain: Compound[] = [{ id: container.html_id, classes: [] }]
    if (matchElements(idChain, idx).length === 1) return idChain
  }

  const levels: { el: InspectElement; compound: Compound }[] = [
    {
      el: container,
      compound: { tag: container.tag, classes: stableClasses(container) },
    },
  ]
  const chainOf = () => levels.map((l) => l.compound)
  if (matchElements(chainOf(), idx).length === 1) return chainOf()

  let cur = container
  let reachedTop = false
  const MAX_DEPTH = 6
  for (let depth = 0; depth < MAX_DEPTH; depth++) {
    const parent = idx.byId.get(cur.parent_id)
    if (!parent) {
      reachedTop = true
      break
    }
    const compound: Compound = parent.html_id
      ? { id: parent.html_id, classes: [] }
      : { tag: parent.tag, classes: stableClasses(parent) }
    levels.unshift({ el: parent, compound })
    if (matchElements(chainOf(), idx).length === 1) return chainOf()
    cur = parent
  }

  // Anchor to <body> if the walk ran out of real ancestors: "body > ul"
  // distinguishes a top-level listing from same-shaped elements nested
  // deeper (e.g. nav menus). Only valid when the outermost level really
  // has no surviving ancestor -- after a MAX_DEPTH stop the chain head
  // still has a parent and a body prefix would just never match.
  const bodyCompound: Compound = { body: true, classes: [] }
  if (reachedTop) {
    const withBody = [bodyCompound, ...chainOf()]
    if (matchElements(withBody, idx).length === 1) return withBody
  }

  // Last resort: pin the outermost level reached with :nth-of-type,
  // body-anchored if possible. Returned even when still not unique --
  // the caller surfaces the resulting match count honestly.
  const outer = levels[0]
  const siblings = idx.children.get(outer.el.parent_id) ?? []
  const sameTag = siblings.filter((s) => s.tag === outer.el.tag)
  const pos = sameTag.findIndex((s) => s.id === outer.el.id) + 1
  levels[0] = { el: outer.el, compound: { ...outer.compound, nth: pos } }
  if (reachedTop) {
    const withBody = [bodyCompound, ...chainOf()]
    if (matchElements(withBody, idx).length === 1) return withBody
  }
  return chainOf()
}

const MAX_CLICK_ANCESTOR_DEPTH = 8
const MIN_REPEAT_COUNT = 2 // below this, the level isn't "repeating" at all

/** Generates 2-3 ranked item_selector candidates from a click inside the
 * inspected page (§10.6). Walks up from clickedId looking for the
 * smallest ancestor level whose structural signature (tag + stable
 * classes) recurs among its siblings, falling back to a document-wide
 * count for grid layouts where items aren't literal siblings. Candidates
 * matching 1 or 0 elements are dropped (§10.6 rule 5). */
export function generateItemSelectorCandidates(
  elements: InspectElement[],
  clickedId: number,
  opts: { maxCandidates?: number } = {},
): SelectorCandidate[] {
  const idx = buildIndex(elements)
  const clicked = idx.byId.get(clickedId)
  if (!clicked) return []

  const chainUp: InspectElement[] = []
  const seen = new Set<number>()
  let cur: InspectElement | undefined = clicked
  while (
    cur &&
    !seen.has(cur.id) &&
    chainUp.length < MAX_CLICK_ANCESTOR_DEPTH
  ) {
    seen.add(cur.id)
    chainUp.push(cur)
    cur = cur.parent_id ? idx.byId.get(cur.parent_id) : undefined
  }

  const scored = new Map<
    string,
    SelectorCandidate & { score: number; fromSiblings: boolean }
  >()

  for (const el of chainUp) {
    const sig = signature(el)
    const siblings = idx.children.get(el.parent_id) ?? []
    const siblingMatches = siblings.filter((s) => signature(s) === sig)

    let score = siblingMatches.length
    let useGlobalContainer = false
    if (siblingMatches.length < 3) {
      const globalMatches = idx.ordered.filter((e) => signature(e) === sig)
      if (globalMatches.length > siblingMatches.length) {
        score = globalMatches.length
        useGlobalContainer = true
      }
    }
    if (score < MIN_REPEAT_COUNT) continue

    const itemPart: Compound = { tag: el.tag, classes: stableClasses(el) }
    let containerChain: Compound[] = []
    if (!useGlobalContainer) {
      const container = idx.byId.get(el.parent_id)
      if (container) containerChain = buildUniqueAncestorChain(container, idx)
    }

    const candidate = toCandidate([...containerChain, itemPart], idx)
    if (candidate.matchCount <= 1) continue

    const fromSiblings = !useGlobalContainer
    const existing = scored.get(candidate.selector)
    if (!existing || existing.score < score) {
      scored.set(candidate.selector, { ...candidate, score, fromSiblings })
    }
  }

  // Sibling-repeat candidates rank above document-wide-fallback ones
  // regardless of raw count: sibling repetition is the primary "these are
  // the articles" signal (§10.6), while a huge global count usually means
  // the signature also matched navigation/chrome (e.g. a bare "a" matching
  // every link on the page).
  return [...scored.values()]
    .sort(
      (a, b) =>
        Number(b.fromSiblings) - Number(a.fromSiblings) || b.score - a.score,
    )
    .slice(0, opts.maxCandidates ?? 3)
    .map(({ selector, matchCount, matchedIds }) => ({
      selector,
      matchCount,
      matchedIds,
    }))
}

function collectSubtreeIds(root: InspectElement, idx: Index): Set<number> {
  const result = new Set<number>()
  const stack = [root]
  while (stack.length > 0) {
    const el = stack.pop()
    if (!el) continue
    result.add(el.id)
    for (const c of idx.children.get(el.id) ?? []) stack.push(c)
  }
  return result
}

function isDescendantOrSelf(
  el: InspectElement,
  ancestorId: number,
  idx: Index,
): boolean {
  const seen = new Set<number>()
  let cur: InspectElement | undefined = el
  while (cur && !seen.has(cur.id)) {
    if (cur.id === ancestorId) return true
    seen.add(cur.id)
    cur = cur.parent_id ? idx.byId.get(cur.parent_id) : undefined
  }
  return false
}

/** Given the item_selector's anchor element (one element the user already
 * committed to as "an item") and a second click somewhere inside it,
 * generates a relative title_selector (§10.6, last paragraph). Unlike the
 * item selector, :nth-of-type is an acceptable fallback here since this
 * selector only has to be unique inside one item, not across the whole
 * document. Returns [] if clicked isn't inside itemElementId's subtree, or
 * is itemElementId itself (nothing to point at). */
export function generateTitleSelectorCandidates(
  elements: InspectElement[],
  itemElementId: number,
  clickedId: number,
): SelectorCandidate[] {
  const idx = buildIndex(elements)
  const itemEl = idx.byId.get(itemElementId)
  const clicked = idx.byId.get(clickedId)
  if (!itemEl || !clicked || clicked.id === itemEl.id) return []
  if (!isDescendantOrSelf(clicked, itemElementId, idx)) return []

  const subtreeIds = collectSubtreeIds(itemEl, idx)
  const subtreeElements = elements.filter((e) => subtreeIds.has(e.id))
  const subtreeIdx = buildIndex(subtreeElements)

  const base: Compound = { tag: clicked.tag, classes: stableClasses(clicked) }
  const plain = toCandidate([base], subtreeIdx)
  if (plain.matchCount === 1) return [plain]

  const siblings = subtreeIdx.children.get(clicked.parent_id) ?? []
  const sameTag = siblings.filter((s) => s.tag === clicked.tag)
  const pos = sameTag.findIndex((s) => s.id === clicked.id) + 1
  const withNth = toCandidate([{ ...base, nth: pos }], subtreeIdx)
  return [withNth]
}

/** Which of itemCandidate.matchedIds (if any) clickedId falls inside --
 * used by the title-selector step to anchor the relative selector to the
 * specific matched item the user clicked into. */
export function findContainingItem(
  elements: InspectElement[],
  matchedIds: number[],
  clickedId: number,
): number | null {
  const idx = buildIndex(elements)
  for (const itemId of matchedIds) {
    if (
      isDescendantOrSelf(
        idx.byId.get(clickedId) ?? ({} as InspectElement),
        itemId,
        idx,
      )
    ) {
      return itemId
    }
  }
  return null
}
