import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { IgnoreWord } from '../api/types'
import { loadEntries } from './entries'
import { loadSubscriptions, selectedFeedId } from './subscriptions'

export const ignoreWords = signal<IgnoreWord[]>([])
export const loadingIgnoreWords = signal(false)
export const ignoreWordsOpen = signal(false)

export async function loadIgnoreWords(): Promise<void> {
  loadingIgnoreWords.value = true
  try {
    const res = await api.listIgnoreWords()
    ignoreWords.value = res.ignore_words
  } finally {
    loadingIgnoreWords.value = false
  }
}

// Adding/removing a word re-filters entries already fetched, so unread
// counts and the currently open feed's entry list must be refreshed too.
async function refreshAffectedViews(): Promise<void> {
  await loadSubscriptions()
  if (selectedFeedId.value !== null) {
    await loadEntries(selectedFeedId.value)
  }
}

export async function addIgnoreWord(word: string): Promise<void> {
  await api.addIgnoreWord(word)
  await Promise.all([loadIgnoreWords(), refreshAffectedViews()])
}

export async function removeIgnoreWordById(id: number): Promise<void> {
  await api.removeIgnoreWord(id)
  ignoreWords.value = ignoreWords.value.filter((w) => w.id !== id)
  await refreshAffectedViews()
}
