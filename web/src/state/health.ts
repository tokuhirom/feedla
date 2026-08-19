import { signal } from '@preact/signals'
import * as api from '../api/client'

export const version = signal<string | null>(null)

export async function loadVersion(): Promise<void> {
  if (version.value !== null) return
  try {
    version.value = (await api.getHealth()).version
  } catch {
    version.value = 'unknown'
  }
}
