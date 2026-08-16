// Mirrors internal/extract/pagewatch.Config.MaxIgnorePatternLen -- kept in
// sync manually since there's no shared schema between Go and the frontend.
const MAX_IGNORE_PATTERN_LEN = 1000

/** Turns a diff block's plain text into a regexp that matches it again,
 * for the "このブロックを無視する" recovery flow (design doc §9.4): regex
 * metacharacters are escaped so the block's own punctuation doesn't get
 * reinterpreted, then digit runs are collapsed to \d+ so a block that
 * merely differs by a date/counter (e.g. "2026-08-16 更新") still matches
 * next time. */
export function buildIgnorePattern(text: string): string {
  const trimmed = text.trim()
  const escaped = trimmed.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const withDigitRuns = escaped.replace(/\d+/g, '\\d+')
  return withDigitRuns.slice(0, MAX_IGNORE_PATTERN_LEN)
}
