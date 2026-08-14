function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

// Locale-independent "2020-05-25 13:25" instead of Date#toLocaleString(),
// whose output (e.g. "5/25/2020, 1:52:20 PM") varies by browser locale.
export function formatUnixSeconds(sec: number): string {
  const d = new Date(sec * 1000)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}
