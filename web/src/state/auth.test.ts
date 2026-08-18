// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthMeResponse } from '../api/client'
import * as api from '../api/client'
import {
  authState,
  checkAuth,
  doAcceptInvitation,
  doLogin,
  doLogout,
  doSetup,
} from './auth'

vi.mock('../api/client', () => ({
  getMe: vi.fn(),
  login: vi.fn(),
  setup: vi.fn(),
  acceptInvitation: vi.fn(),
  logout: vi.fn(),
}))

const user = { id: 1, username: 'alice', is_admin: false }

beforeEach(() => {
  vi.clearAllMocks()
  authState.value = { status: 'loading' }
  window.history.replaceState({}, '', '/')
})

describe('checkAuth', () => {
  it('shows the invite screen for /invite/<token> without calling the API', async () => {
    window.history.replaceState({}, '', '/invite/abc123')
    await checkAuth()
    expect(authState.value).toEqual({ status: 'invite', token: 'abc123' })
    expect(api.getMe).not.toHaveBeenCalled()
  })

  it('URL-decodes the invite token', async () => {
    window.history.replaceState({}, '', '/invite/a%20b')
    await checkAuth()
    expect(authState.value).toEqual({ status: 'invite', token: 'a b' })
  })

  it('shows authenticated when getMe reports a logged-in user', async () => {
    vi.mocked(api.getMe).mockResolvedValue({
      authenticated: true,
      setup_required: false,
      user,
    } satisfies AuthMeResponse)
    await checkAuth()
    expect(authState.value).toEqual({ status: 'authenticated', user })
  })

  it('shows setup when getMe reports setup_required', async () => {
    vi.mocked(api.getMe).mockResolvedValue({
      authenticated: false,
      setup_required: true,
    })
    await checkAuth()
    expect(authState.value).toEqual({ status: 'setup' })
  })

  it('shows login when neither authenticated nor setup_required', async () => {
    vi.mocked(api.getMe).mockResolvedValue({
      authenticated: false,
      setup_required: false,
    })
    await checkAuth()
    expect(authState.value).toEqual({ status: 'login' })
  })

  it('leaves the state untouched (stuck loading) when getMe itself fails', async () => {
    vi.mocked(api.getMe).mockRejectedValue(new Error('network error'))
    await checkAuth()
    expect(authState.value).toEqual({ status: 'loading' })
  })
})

describe('doLogin / doSetup', () => {
  it('doLogin sets authenticated on success', async () => {
    vi.mocked(api.login).mockResolvedValue({
      authenticated: true,
      setup_required: false,
      user,
    })
    await doLogin('alice', 'pw')
    expect(authState.value).toEqual({ status: 'authenticated', user })
  })

  it('doSetup sets authenticated on success', async () => {
    vi.mocked(api.setup).mockResolvedValue({
      authenticated: true,
      setup_required: false,
      user,
    })
    await doSetup('alice', 'pw')
    expect(authState.value).toEqual({ status: 'authenticated', user })
  })
})

describe('doAcceptInvitation', () => {
  it('sets authenticated and strips the /invite/<token> path', async () => {
    window.history.replaceState({}, '', '/invite/abc123')
    vi.mocked(api.acceptInvitation).mockResolvedValue({
      authenticated: true,
      setup_required: false,
      user,
    })
    await doAcceptInvitation('abc123', 'alice', 'pw')
    expect(authState.value).toEqual({ status: 'authenticated', user })
    expect(window.location.pathname).toBe('/')
  })
})

describe('doLogout', () => {
  it('sets status to login', async () => {
    authState.value = { status: 'authenticated', user }
    vi.mocked(api.logout).mockResolvedValue(undefined)
    await doLogout()
    expect(authState.value).toEqual({ status: 'login' })
  })

  it('still sets status to login even if the logout request fails', async () => {
    authState.value = { status: 'authenticated', user }
    vi.mocked(api.logout).mockRejectedValue(new Error('boom'))
    await expect(doLogout()).rejects.toThrow('boom')
    expect(authState.value).toEqual({ status: 'login' })
  })
})

describe('feedla:unauthorized event', () => {
  it('drops an authenticated session back to login', () => {
    authState.value = { status: 'authenticated', user }
    window.dispatchEvent(new Event('feedla:unauthorized'))
    expect(authState.value).toEqual({ status: 'login' })
  })

  it('does nothing when not authenticated', () => {
    authState.value = { status: 'setup' }
    window.dispatchEvent(new Event('feedla:unauthorized'))
    expect(authState.value).toEqual({ status: 'setup' })
  })
})
