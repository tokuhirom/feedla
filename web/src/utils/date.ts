export function formatUnixSeconds(sec: number): string {
  return new Date(sec * 1000).toLocaleString()
}
