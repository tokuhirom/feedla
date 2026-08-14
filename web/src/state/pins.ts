import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { Pin } from '../api/types'
import { entries } from './entries'

export const pins = signal<Pin[]>([])
export const loadingPins = signal(false)
export const pinsOpen = signal(false)

export async function loadPins(): Promise<void> {
  loadingPins.value = true
  try {
    const res = await api.listPins()
    pins.value = res.pins
  } finally {
    loadingPins.value = false
  }
}

export async function removePinById(entryId: number): Promise<void> {
  await api.removePin(entryId)
  pins.value = pins.value.filter((p) => p.entry_id !== entryId)
  entries.value = entries.value.map((e) => (e.id === entryId ? { ...e, pinned: false } : e))
}
