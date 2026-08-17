import { signal } from '@preact/signals'
import * as api from '../api/client'
import type { AdminUser } from '../api/types'

export const adminUsers = signal<AdminUser[]>([])
export const adminOpen = signal(false)

export async function loadAdminUsers(): Promise<void> {
  const res = await api.listAdminUsers()
  adminUsers.value = res.users
}

export async function createAdminUser(
  username: string,
  password: string,
  isAdmin: boolean,
): Promise<void> {
  await api.createAdminUser({ username, password, is_admin: isAdmin })
  await loadAdminUsers()
}

export async function setAdminUserAdmin(
  id: number,
  isAdmin: boolean,
): Promise<void> {
  const updated = await api.patchAdminUser(id, { is_admin: isAdmin })
  adminUsers.value = adminUsers.value.map((u) => (u.id === id ? updated : u))
}

export async function setAdminUserDisabled(
  id: number,
  isDisabled: boolean,
): Promise<void> {
  const updated = await api.patchAdminUser(id, { is_disabled: isDisabled })
  adminUsers.value = adminUsers.value.map((u) => (u.id === id ? updated : u))
}
