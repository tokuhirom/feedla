import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { Stats } from '../api/types'

export const stats = signal<Stats | null>(null)
export const loadingStats = signal(false)
export const statsOpen = signal(false)

export async function loadStats(): Promise<void> {
  loadingStats.value = true
  try {
    stats.value = await api.getStats()
  } finally {
    loadingStats.value = false
  }
}
