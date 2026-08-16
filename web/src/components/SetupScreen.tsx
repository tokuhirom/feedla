import { useState } from 'preact/hooks'
import { doSetup } from '../state/auth'

const MIN_PASSWORD_LEN = 12

export function SetupScreen() {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

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
          管理者アカウントを作成します。この画面は最初の1回だけ表示されます。
        </p>
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
            {submitting ? '作成中…' : '管理者アカウントを作成'}
          </button>
        </form>
      </div>
    </div>
  )
}
