import { signal } from '@preact/signals'
import type { AuthUser } from '../api/client'
import * as api from '../api/client'

export type AuthState =
  | { status: 'loading' }
  | { status: 'setup' }
  | { status: 'login' }
  | { status: 'authenticated'; user: AuthUser }

export const authState = signal<AuthState>({ status: 'loading' })

// Called once on app boot (see main.tsx). GET /api/v1/auth/me is the one
// endpoint that works whether or not a session exists, so it tells us
// which of the three screens (setup / login / the real app) to show.
export async function checkAuth(): Promise<void> {
  try {
    const me = await api.getMe()
    if (me.authenticated && me.user) {
      authState.value = { status: 'authenticated', user: me.user }
    } else if (me.setup_required) {
      authState.value = { status: 'setup' }
    } else {
      authState.value = { status: 'login' }
    }
  } catch {
    // /api/v1/auth/me itself failing (network error, 5xx) leaves the user
    // on the loading screen rather than bouncing them to login -- a
    // transient failure shouldn't look like "you got logged out".
  }
}

export async function doLogin(
  username: string,
  password: string,
): Promise<void> {
  const me = await api.login(username, password)
  if (me.user) authState.value = { status: 'authenticated', user: me.user }
}

export async function doSetup(
  username: string,
  password: string,
): Promise<void> {
  const me = await api.setup(username, password)
  if (me.user) authState.value = { status: 'authenticated', user: me.user }
}

export async function doLogout(): Promise<void> {
  try {
    await api.logout()
  } finally {
    authState.value = { status: 'login' }
  }
}

// A session can expire (idle/absolute timeout) while the app is open, or a
// password change from another tab invalidates it -- api/client.ts
// dispatches this event on any 401 instead of importing this module
// directly, to avoid a state/auth.ts <-> api/client.ts import cycle.
window.addEventListener('feedla:unauthorized', () => {
  if (authState.value.status === 'authenticated') {
    authState.value = { status: 'login' }
  }
})
