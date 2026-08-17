import { useEffect, useState } from 'preact/hooks'
import * as api from '../api/client'
import { doAcceptInvitation } from '../state/auth'

const MIN_PASSWORD_LEN = 12

export function InviteAcceptScreen({ token }: { token: string }) {
  const [checking, setChecking] = useState(true)
  const [invalid, setInvalid] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    api
      .getInvitationStatus(token)
      .then((res) => setInvalid(!res.valid))
      .catch(() => setInvalid(true))
      .finally(() => setChecking(false))
  }, [token])

  const passwordTooShort =
    password.length > 0 && password.length < MIN_PASSWORD_LEN
  const passwordsMismatch = confirm.length > 0 && password !== confirm
  const canSubmit =
    username.trim() !== '' &&
    password.length >= MIN_PASSWORD_LEN &&
    password === confirm

  async function submit(): Promise<void> {
    setSubmitting(true)
    setError(null)
    try {
      await doAcceptInvitation(token, username, password)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  if (checking) {
    return (
      <div class="auth-screen">
        <div class="dialog-panel">
          <p>招待を確認しています…</p>
        </div>
      </div>
    )
  }

  if (invalid) {
    return (
      <div class="auth-screen">
        <div class="dialog-panel">
          <h1>招待リンクが無効です</h1>
          <p class="auth-hint">
            この招待リンクは期限切れか、既に使用されています。管理者に新しい招待の発行を依頼してください。
          </p>
        </div>
      </div>
    )
  }

  return (
    <div class="auth-screen">
      <div class="dialog-panel">
        <h1>feedla アカウント作成</h1>
        <p class="auth-hint">招待リンクからアカウントを作成します。</p>
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
          <button type="submit" disabled={submitting || !canSubmit}>
            {submitting ? '作成中…' : 'アカウントを作成'}
          </button>
        </form>
      </div>
    </div>
  )
}
