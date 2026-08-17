import { useEffect, useState } from 'preact/hooks'
import {
  adminOpen,
  adminUsers,
  createAdminUser,
  loadAdminUsers,
  setAdminUserAdmin,
  setAdminUserDisabled,
} from '../state/admin'
import { authState } from '../state/auth'
import { showToast } from '../state/ui'
import { formatUnixSeconds } from '../utils/date'

export function AdminOverlay() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (adminOpen.value) void loadAdminUsers()
  }, [adminOpen.value])

  if (!adminOpen.value) return null

  const selfID =
    authState.value.status === 'authenticated' ? authState.value.user.id : -1

  async function submit(): Promise<void> {
    if (!username.trim() || password.length < 12) return
    setSubmitting(true)
    try {
      await createAdminUser(username.trim(), password, isAdmin)
      setUsername('')
      setPassword('')
      setIsAdmin(false)
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  async function toggleAdmin(id: number, next: boolean): Promise<void> {
    try {
      await setAdminUserAdmin(id, next)
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err))
    }
  }

  async function toggleDisabled(id: number, next: boolean): Promise<void> {
    try {
      await setAdminUserDisabled(id, next)
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div class="dialog-overlay" onClick={() => (adminOpen.value = false)}>
      <div
        class="dialog-panel help-panel-wide"
        onClick={(e) => e.stopPropagation()}
      >
        <h2>ユーザー管理</h2>

        <div class="admin-user-table-wrap">
          <table class="admin-user-table">
            <thead>
              <tr>
                <th>ユーザー名</th>
                <th>権限</th>
                <th>状態</th>
                <th>作成日</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {adminUsers.value.map((u) => (
                <tr key={u.id}>
                  <td>{u.username}</td>
                  <td>{u.is_admin ? '管理者' : '一般'}</td>
                  <td>{u.is_disabled ? '無効' : '有効'}</td>
                  <td>{formatUnixSeconds(u.created_at)}</td>
                  <td class="admin-user-actions">
                    <button
                      type="button"
                      onClick={() => void toggleAdmin(u.id, !u.is_admin)}
                    >
                      {u.is_admin ? '管理者を外す' : '管理者にする'}
                    </button>
                    <button
                      type="button"
                      onClick={() => void toggleDisabled(u.id, !u.is_disabled)}
                    >
                      {u.is_disabled ? '有効化' : '無効化'}
                    </button>
                    {u.id === selfID && (
                      <span class="admin-user-self-note">(自分)</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <h3>ユーザーを追加</h3>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <input
            type="text"
            placeholder="ユーザー名"
            value={username}
            onInput={(e) => setUsername((e.target as HTMLInputElement).value)}
          />
          <input
            type="password"
            placeholder="パスワード(12文字以上)"
            value={password}
            onInput={(e) => setPassword((e.target as HTMLInputElement).value)}
          />
          <label class="admin-user-is-admin-label">
            <input
              type="checkbox"
              checked={isAdmin}
              onChange={(e) =>
                setIsAdmin((e.target as HTMLInputElement).checked)
              }
            />
            管理者にする
          </label>
          <div class="dialog-actions">
            <button
              type="submit"
              disabled={submitting || !username.trim() || password.length < 12}
            >
              作成
            </button>
          </div>
        </form>

        <div class="dialog-actions">
          <button type="button" onClick={() => (adminOpen.value = false)}>
            閉じる
          </button>
        </div>
      </div>
    </div>
  )
}
