function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export interface HighlightSegment {
  text: string
  match: boolean
}

/** Splits `text` into plain/matched segments for keyword highlighting in
 * plain-text contexts (e.g. an entry title rendered as JSX). Case-insensitive,
 * matches every occurrence. */
export function highlightSegments(
  text: string,
  query: string,
): HighlightSegment[] {
  const q = query.trim()
  if (!q) return [{ text, match: false }]

  const re = new RegExp(escapeRegExp(q), 'gi')
  const segments: HighlightSegment[] = []
  let lastIndex = 0
  let m: RegExpExecArray | null
  // biome-ignore lint/suspicious/noAssignInExpressions: standard exec loop
  while ((m = re.exec(text))) {
    if (m.index > lastIndex) {
      segments.push({ text: text.slice(lastIndex, m.index), match: false })
    }
    segments.push({ text: m[0], match: true })
    lastIndex = m.index + m[0].length
  }
  if (lastIndex < text.length) {
    segments.push({ text: text.slice(lastIndex), match: false })
  }
  return segments.length > 0 ? segments : [{ text, match: false }]
}

/** Wraps every occurrence of `query` in `root`'s text nodes with a <mark>,
 * for highlighting inside already-rendered HTML (EntryItem's sanitized
 * entry.body, set via dangerouslySetInnerHTML) without touching tags or
 * attributes -- a naive string replace on the HTML source could match
 * inside an attribute value or break entity-encoded text. Walking text
 * nodes and rewriting only their content sidesteps both. Safe to call
 * more than once: nodes already inside a <mark> are skipped. */
export function highlightElementText(root: HTMLElement, query: string): void {
  const q = query.trim()
  if (!q) return
  const re = new RegExp(escapeRegExp(q), 'gi')

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const parent = (node as Text).parentElement
      if (
        parent &&
        (parent.tagName === 'MARK' ||
          parent.tagName === 'SCRIPT' ||
          parent.tagName === 'STYLE')
      ) {
        return NodeFilter.FILTER_REJECT
      }
      return NodeFilter.FILTER_ACCEPT
    },
  })
  const textNodes: Text[] = []
  let node: Node | null
  // biome-ignore lint/suspicious/noAssignInExpressions: standard walker loop
  while ((node = walker.nextNode())) {
    textNodes.push(node as Text)
  }

  for (const textNode of textNodes) {
    const text = textNode.textContent ?? ''
    re.lastIndex = 0
    if (!re.test(text)) continue

    re.lastIndex = 0
    const frag = document.createDocumentFragment()
    let lastIndex = 0
    let m: RegExpExecArray | null
    // biome-ignore lint/suspicious/noAssignInExpressions: standard exec loop
    while ((m = re.exec(text))) {
      if (m.index > lastIndex) {
        frag.appendChild(
          document.createTextNode(text.slice(lastIndex, m.index)),
        )
      }
      const mark = document.createElement('mark')
      mark.className = 'search-highlight'
      mark.textContent = m[0]
      frag.appendChild(mark)
      lastIndex = m.index + m[0].length
    }
    if (lastIndex < text.length) {
      frag.appendChild(document.createTextNode(text.slice(lastIndex)))
    }
    textNode.parentNode?.replaceChild(frag, textNode)
  }
}
