/** Google's public favicon service -- no backend involvement, so a broken or
 * unreachable site still gets a reasonable fallback icon from Google. */
export function faviconUrl(url: string, size = 16): string {
  let domain = url
  try {
    domain = new URL(url).hostname
  } catch {
    // url wasn't absolute; fall back to using it as-is.
  }
  return `https://www.google.com/s2/favicons?sz=${size}&domain=${encodeURIComponent(domain)}`
}
