import { useState } from 'preact/hooks'
import type { RestoreHint } from '../api/client'
import * as api from '../api/client'
import { checkAuth, doSetup } from '../state/auth'

const MIN_PASSWORD_LEN = 12

interface SetupScreenProps {
  restoreHint?: RestoreHint
}

// Explains why this instance landed on "create an admin account" instead
// of transparently restoring a prior DB, so an operator who expected the
// latter (e.g. redeploying to a new host from an existing backup) can tell
// "no backup config found" apart from "configured but nothing there yet"
// apart from "misconfigured" without having to shell in and read logs.
function describeRestoreHint(hint: RestoreHint): string {
  if (!hint.local_configured && !hint.remote_configured) {
    return (
      'バックアップ設定(FR_BACKUP_DIR / FR_BACKUP_REMOTE_*)が見つからなかったため、' +
      '新規セットアップとして扱われています。既存データを復元したい場合は、' +
      '環境変数を設定してからインスタンスを再起動してください。'
    )
  }

  const parts: string[] = []
  if (hint.remote_error) {
    parts.push(
      'リモートバックアップ(FR_BACKUP_REMOTE_*)への接続に失敗しました。エンドポイント/認証情報の設定を確認してください。',
    )
  } else if (hint.remote_configured) {
    parts.push(
      hint.remote_has_snapshot
        ? 'リモートバックアップにスナップショットが見つかっています。'
        : 'リモートバックアップ(FR_BACKUP_REMOTE_*)は設定されていますが、スナップショットが見つかりませんでした。',
    )
  } else {
    parts.push('リモートバックアップ(FR_BACKUP_REMOTE_*)は未設定です。')
  }

  if (hint.local_configured) {
    parts.push(
      hint.local_has_snapshot
        ? 'ローカルバックアップにもスナップショットが見つかっています。'
        : 'ローカルバックアップ(FR_BACKUP_DIR)は設定されていますが、スナップショットが見つかりませんでした。',
    )
  } else {
    parts.push('ローカルバックアップ(FR_BACKUP_DIR)は未設定です。')
  }

  if (hint.local_has_snapshot || hint.remote_has_snapshot) {
    parts.push(
      'スナップショットは見つかっていますが、その中にまだ管理者アカウントが設定されていない可能性があります。',
    )
  }
  return parts.join(' ')
}

// feedla-YYYYMMDD.db -> "YYYY-MM-DD"; anything unexpected falls back to
// the raw name rather than hiding what would be restored.
function snapshotDateLabel(name: string): string {
  const m = /^feedla-(\d{4})(\d{2})(\d{2})\.db$/.exec(name)
  return m ? `${m[1]}-${m[2]}-${m[3]}` : name
}

// How long to keep polling for the server to come back after a restore
// request (the swap itself is quick; this mostly covers a slow remote
// snapshot download before the restart even begins).
const RESTORE_POLL_TIMEOUT_MS = 90_000
const RESTORE_POLL_INTERVAL_MS = 1_500

export function SetupScreen({ restoreHint }: SetupScreenProps) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)

  const canRestore =
    restoreHint?.restore_supported === true && !!restoreHint.latest_snapshot

  async function startRestore(): Promise<void> {
    setRestoring(true)
    setRestoreError(null)
    try {
      await api.restoreFromBackup()
    } catch (e) {
      setRestoreError(e instanceof Error ? e.message : String(e))
      setRestoring(false)
      return
    }
    // 202 means the server staged the snapshot and is restarting to swap
    // it in. Poll /auth/me until the restored instance answers with setup
    // no longer pending; connection errors while the process is down are
    // expected and just mean "keep waiting".
    const deadline = Date.now() + RESTORE_POLL_TIMEOUT_MS
    while (Date.now() < deadline) {
      await new Promise((resolve) =>
        setTimeout(resolve, RESTORE_POLL_INTERVAL_MS),
      )
      try {
        const me = await api.getMe()
        if (!me.setup_required) {
          await checkAuth()
          return
        }
      } catch {
        // server still restarting
      }
    }
    setRestoring(false)
    setRestoreError(
      '復元後のサーバー再起動を確認できませんでした。ページを再読み込みして状態を確認してください。',
    )
  }

  const passwordTooShort =
    password.length > 0 && password.length < MIN_PASSWORD_LEN
  const passwordsMismatch = confirm.length > 0 && password !== confirm

  async function submit(): Promise<void> {
    setSubmitting(true)
    setError(null)
    try {
      await doSetup(username, password)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const canSubmit =
    username.trim() !== '' &&
    password.length >= MIN_PASSWORD_LEN &&
    password === confirm

  return (
    <div class="auth-screen">
      <div class="dialog-panel">
        <h1>feedla 初期セットアップ</h1>
        <p class="auth-hint">
          {canRestore
            ? 'この画面は最初の1回だけ表示されます。バックアップから復元するか、新しく管理者アカウントを作成してください。'
            : '管理者アカウントを作成します。この画面は最初の1回だけ表示されます。'}
        </p>
        {canRestore && restoreHint?.latest_snapshot ? (
          <div class="restore-choice">
            <p class="auth-hint">
              {snapshotDateLabel(restoreHint.latest_snapshot)} のバックアップ(
              {restoreHint.latest_snapshot_source === 'remote'
                ? 'リモート'
                : 'ローカル'}
              )が見つかりました。
            </p>
            <button
              type="button"
              onClick={() => void startRestore()}
              disabled={restoring || submitting}
            >
              {restoring
                ? '復元中… サーバーの再起動を待っています'
                : 'このバックアップから復元する'}
            </button>
            {restoreError && <p class="dialog-error">{restoreError}</p>}
            <p class="auth-hint">または、新しく管理者アカウントを作成:</p>
          </div>
        ) : (
          restoreHint && (
            <p class="auth-hint restore-hint">
              {describeRestoreHint(restoreHint)}
            </p>
          )
        )}
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (canSubmit) void submit()
          }}
        >
          <input
            type="text"
            placeholder="ユーザー名"
            value={username}
            onInput={(e) => setUsername((e.target as HTMLInputElement).value)}
            autoFocus
            autoComplete="username"
          />
          <input
            type="password"
            placeholder={`パスワード(${MIN_PASSWORD_LEN}文字以上)`}
            value={password}
            onInput={(e) => setPassword((e.target as HTMLInputElement).value)}
            autoComplete="new-password"
          />
          <input
            type="password"
            placeholder="パスワード(確認)"
            value={confirm}
            onInput={(e) => setConfirm((e.target as HTMLInputElement).value)}
            autoComplete="new-password"
          />
          {passwordTooShort && (
            <p class="auth-hint">
              パスワードは{MIN_PASSWORD_LEN}文字以上にしてください。
            </p>
          )}
          {passwordsMismatch && (
            <p class="dialog-error">パスワードが一致しません。</p>
          )}
          {error && <p class="dialog-error">{error}</p>}
          <button
            type="submit"
            disabled={submitting || restoring || !canSubmit}
          >
            {submitting ? '作成中…' : '管理者アカウントを作成'}
          </button>
        </form>
      </div>
    </div>
  )
}
