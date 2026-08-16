import { useState } from 'preact/hooks'
import { ApiError } from '../api/client'
import { doLogin } from '../state/auth'

export function LoginScreen() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function submit(): Promise<void> {
    setSubmitting(true)
    setError(null)
    try {
      await doLogin(username, password)
    } catch (e) {
      // 429 (rate-limited) and 401 (wrong credentials) both come back as
      // "invalid username or password"-shaped errors from the server;
      // the rate-limit case gets its own message so a locked-out user
      // isn't told to just try a different password.
      if (e instanceof ApiError && e.status === 429) {
        setError('試行回数が多すぎます。しばらく待ってから再度お試しください。')
      } else {
        setError(e instanceof Error ? e.message : String(e))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div class="auth-screen">
      <div class="dialog-panel">
        <h1>feedla ログイン</h1>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (username && password) void submit()
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
            placeholder="パスワード"
            value={password}
            onInput={(e) => setPassword((e.target as HTMLInputElement).value)}
            autoComplete="current-password"
          />
          {error && <p class="dialog-error">{error}</p>}
          <button type="submit" disabled={submitting || !username || !password}>
            {submitting ? 'ログイン中…' : 'ログイン'}
          </button>
        </form>
      </div>
    </div>
  )
}
