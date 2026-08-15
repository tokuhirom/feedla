import { signal } from '@preact/signals'

export const helpOpen = signal(false)
export const addDialogOpen = signal(false)
export const feedManagerOpen = signal(false)
// Set right before feedManagerOpen so the ⚠ sidebar badge can open
// FeedManagerOverlay pre-filtered to erroring feeds instead of a separate
// error-only screen -- read once by FeedManagerOverlay's onlyErrors state on
// mount, then irrelevant until the next open.
export const feedManagerInitialOnlyErrors = signal(false)
export const searchOpen = signal(false)
export const feedDetailOpen = signal(false)
export type ToastState = { message: string; variant: 'info' | 'error' }
export const toast = signal<ToastState | null>(null)

let toastTimer: ReturnType<typeof setTimeout> | null = null

function showToastVariant(
  message: string,
  variant: ToastState['variant'],
  ms: number,
): void {
  toast.value = { message, variant }
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    toast.value = null
  }, ms)
}

export function showToast(message: string, ms = 2500): void {
  showToastVariant(message, 'info', ms)
}

/** Longer-lived than showToast -- used for server-side failures (5xx) the
 * user didn't directly trigger with a click, so they need enough time to
 * actually notice it (e.g. while reading, not looking at the UI chrome). */
export function showErrorToast(message: string, ms = 4000): void {
  showToastVariant(message, 'error', ms)
}
