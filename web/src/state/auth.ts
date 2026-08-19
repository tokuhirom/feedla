import { signal } from '@preact/signals'
import type { AuthUser } from '../api/client'
import * as api from '../api/client'

export type AuthState =
  | { status: 'loading' }
  | { status: 'setup' }
  | { status: 'login' }
  | { status: 'invite'; token: string }
  | { status: 'authenticated'; user: AuthUser }

export const authState = signal<AuthState>({ status: 'loading' })

// Invite links look like /invite/<token> (see AdminOverlay's issued-link
// display); the SPA has no router, but internal/web's Handler already
// falls back to index.html for any unrecognized path, so this only needs
// to parse what's already there.
function inviteTokenFromLocation(): string | null {
  const m = /^\/invite\/([^/]+)$/.exec(window.location.pathname)
  return m ? decodeURIComponent(m[1]) : null
}

// Called once on app boot (see main.tsx). GET /api/v1/auth/me is the one
// endpoint that works whether or not a session exists, so it tells us
// which of the three screens (setup / login / the real app) to show.
export async function checkAuth(): Promise<void> {
  const inviteToken = inviteTokenFromLocation()
  if (inviteToken) {
    authState.value = { status: 'invite', token: inviteToken }
    return
  }
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

export async function doAcceptInvitation(
  token: string,
  username: string,
  password: string,
): Promise<void> {
  const me = await api.acceptInvitation(token, username, password)
  if (me.user) {
    // Drop the /invite/<token> path so a later reload doesn't re-parse a
    // now-spent token and show the invite screen again.
    window.history.replaceState({}, '', '/')
    authState.value = { status: 'authenticated', user: me.user }
  }
}

// Persisted server-side (see docs/adr/0001-third-party-embed-in-feed-content.md)
// so it follows the account across devices/browsers, unlike a
// localStorage-only toggle.
export async function setInstagramEmbedsEnabled(
  enabled: boolean,
): Promise<void> {
  const user = await api.updateMe({ instagram_embeds_enabled: enabled })
  authState.value = { status: 'authenticated', user }
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
