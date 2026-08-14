import { signal } from '@preact/signals'

export const helpOpen = signal(false)
export const addDialogOpen = signal(false)
export const toast = signal<string | null>(null)

let toastTimer: ReturnType<typeof setTimeout> | null = null

export function showToast(message: string, ms = 2500): void {
  toast.value = message
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    toast.value = null
  }, ms)
}
