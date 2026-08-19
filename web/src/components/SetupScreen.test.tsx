// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { RestoreHint } from '../api/client'
import * as api from '../api/client'
import { SetupScreen } from './SetupScreen'

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    restoreFromBackup: vi.fn(),
    getMe: vi.fn(),
  }
})

vi.mock('../state/auth', () => ({
  checkAuth: vi.fn().mockResolvedValue(undefined),
  doSetup: vi.fn().mockResolvedValue(undefined),
}))

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  cleanup()
})

const restorableHint: RestoreHint = {
  local_configured: true,
  local_has_snapshot: true,
  remote_configured: true,
  remote_has_snapshot: true,
  remote_error: false,
  restore_supported: true,
  latest_snapshot: 'feedla-20260818.db',
  latest_snapshot_source: 'remote',
}

describe('SetupScreen restore choice', () => {
  it('offers restore when a snapshot is available, with its date and source', () => {
    render(<SetupScreen restoreHint={restorableHint} />)
    expect(
      screen.getByRole('button', { name: 'このバックアップから復元する' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/2026-08-18/)).toBeInTheDocument()
    expect(screen.getByText(/リモート/)).toBeInTheDocument()
    // The create-admin form is still there as the other choice.
    expect(
      screen.getByRole('button', { name: '管理者アカウントを作成' }),
    ).toBeInTheDocument()
  })

  it('calls the restore API when the restore button is clicked', async () => {
    // Never-resolving getMe keeps the poll loop pending so the test only
    // observes the initial request + button state flip.
    vi.mocked(api.restoreFromBackup).mockResolvedValue({ status: 'restarting' })
    vi.mocked(api.getMe).mockReturnValue(new Promise(() => {}))

    render(<SetupScreen restoreHint={restorableHint} />)
    fireEvent.click(
      screen.getByRole('button', { name: 'このバックアップから復元する' }),
    )
    expect(api.restoreFromBackup).toHaveBeenCalledTimes(1)
    expect(
      await screen.findByRole('button', {
        name: '復元中… サーバーの再起動を待っています',
      }),
    ).toBeDisabled()
  })

  it('does not offer restore when the server does not support it', () => {
    render(
      <SetupScreen
        restoreHint={{ ...restorableHint, restore_supported: false }}
      />,
    )
    expect(
      screen.queryByRole('button', { name: 'このバックアップから復元する' }),
    ).not.toBeInTheDocument()
  })

  it('does not offer restore when no snapshot was found', () => {
    render(
      <SetupScreen
        restoreHint={{
          ...restorableHint,
          local_has_snapshot: false,
          remote_has_snapshot: false,
          latest_snapshot: undefined,
          latest_snapshot_source: undefined,
        }}
      />,
    )
    expect(
      screen.queryByRole('button', { name: 'このバックアップから復元する' }),
    ).not.toBeInTheDocument()
  })
})
