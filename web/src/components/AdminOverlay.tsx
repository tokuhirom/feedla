import { useEffect, useState } from 'preact/hooks'
import type { BackupFile } from '../api/types'
import {
  adminBackupStatus,
  adminInvitations,
  adminOpen,
  adminUsers,
  createAdminUser,
  issueInvitation,
  loadAdminBackupStatus,
  loadAdminInvitations,
  loadAdminUsers,
  setAdminUserAdmin,
  setAdminUserDisabled,
} from '../state/admin'
import { authState } from '../state/auth'
import { showToast } from '../state/ui'
import { formatUnixSeconds } from '../utils/date'

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

function BackupFileTable({ files }: { files: BackupFile[] }) {
  if (files.length === 0) {
    return <p class="admin-backup-empty">バックアップファイルはありません。</p>
  }
  return (
    <div class="admin-user-table-wrap">
      <table class="admin-user-table">
        <thead>
          <tr>
            <th>ファイル名</th>
            <th>サイズ</th>
            <th>更新日時</th>
          </tr>
        </thead>
        <tbody>
          {files.map((f) => (
            <tr key={f.name}>
              <td>{f.name}</td>
              <td>{formatBytes(f.size_bytes)}</td>
              <td>{formatUnixSeconds(f.modified_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

type AdminTab = 'users' | 'invitations' | 'backup'

export function AdminOverlay() {
  const [tab, setTab] = useState<AdminTab>('users')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [issuing, setIssuing] = useState(false)
  const [issuedLink, setIssuedLink] = useState<string | null>(null)

  useEffect(() => {
    if (adminOpen.value) {
      void loadAdminUsers()
      void loadAdminInvitations()
      void loadAdminBackupStatus()
    } else {
      setIssuedLink(null)
      setTab('users')
    }
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

  async function issue(): Promise<void> {
    setIssuing(true)
    try {
      const token = await issueInvitation()
      setIssuedLink(`${window.location.origin}/invite/${token}`)
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err))
    } finally {
      setIssuing(false)
    }
  }

  return (
    <div class="dialog-overlay" onClick={() => (adminOpen.value = false)}>
      <div
        class="dialog-panel help-panel-wide"
        onClick={(e) => e.stopPropagation()}
      >
        <h2>管理者用ツール</h2>

        <div class="admin-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'users'}
            class={`admin-tab-button${tab === 'users' ? ' active' : ''}`}
            onClick={() => setTab('users')}
          >
            ユーザー
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'invitations'}
            class={`admin-tab-button${tab === 'invitations' ? ' active' : ''}`}
            onClick={() => setTab('invitations')}
          >
            招待
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'backup'}
            class={`admin-tab-button${tab === 'backup' ? ' active' : ''}`}
            onClick={() => setTab('backup')}
          >
            バックアップ
          </button>
        </div>

        {tab === 'users' && (
          <div class="admin-tab-panel">
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
                          onClick={() =>
                            void toggleDisabled(u.id, !u.is_disabled)
                          }
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
                onInput={(e) =>
                  setUsername((e.target as HTMLInputElement).value)
                }
              />
              <input
                type="password"
                placeholder="パスワード(12文字以上)"
                value={password}
                onInput={(e) =>
                  setPassword((e.target as HTMLInputElement).value)
                }
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
                  disabled={
                    submitting || !username.trim() || password.length < 12
                  }
                >
                  作成
                </button>
              </div>
            </form>
          </div>
        )}

        {tab === 'invitations' && (
          <div class="admin-tab-panel">
            {issuedLink && (
              <div class="auth-hint">
                <p>
                  招待リンク(この画面を閉じると再表示できません。コピーしてください):
                </p>
                <input
                  type="text"
                  readOnly
                  value={issuedLink}
                  onClick={(e) => (e.target as HTMLInputElement).select()}
                />
              </div>
            )}
            <div class="admin-user-table-wrap">
              <table class="admin-user-table">
                <thead>
                  <tr>
                    <th>発行日</th>
                    <th>期限</th>
                    <th>状態</th>
                  </tr>
                </thead>
                <tbody>
                  {adminInvitations.value.map((inv) => (
                    <tr key={inv.id}>
                      <td>{formatUnixSeconds(inv.created_at)}</td>
                      <td>{formatUnixSeconds(inv.expires_at)}</td>
                      <td>{inv.used_by ? '使用済み' : '未使用'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div class="dialog-actions">
              <button
                type="button"
                onClick={() => void issue()}
                disabled={issuing}
              >
                {issuing ? '発行中…' : '招待リンクを発行'}
              </button>
            </div>
          </div>
        )}

        {tab === 'backup' && (
          <div class="admin-tab-panel">
            {adminBackupStatus.value === null ? (
              <p class="admin-backup-empty">読み込み中…</p>
            ) : (
              <>
                <h4>
                  ローカル
                  {adminBackupStatus.value.local_enabled
                    ? adminBackupStatus.value.local_dir
                      ? `(${adminBackupStatus.value.local_dir})`
                      : ''
                    : '(未設定)'}
                </h4>
                {adminBackupStatus.value.local_enabled ? (
                  <BackupFileTable
                    files={adminBackupStatus.value.local_files}
                  />
                ) : (
                  <p class="admin-backup-empty">
                    FR_BACKUP_DIR
                    が未設定のため、ローカルバックアップは無効です。
                  </p>
                )}

                <h4>
                  リモート
                  {adminBackupStatus.value.remote_enabled ? '' : '(未設定)'}
                </h4>
                {adminBackupStatus.value.remote_enabled ? (
                  <BackupFileTable
                    files={adminBackupStatus.value.remote_files}
                  />
                ) : (
                  <p class="admin-backup-empty">
                    FR_BACKUP_REMOTE_*
                    が未設定のため、リモートバックアップは無効です。
                  </p>
                )}
              </>
            )}
          </div>
        )}

        <div class="dialog-actions">
          <button type="button" onClick={() => (adminOpen.value = false)}>
            閉じる
          </button>
        </div>
      </div>
    </div>
  )
}
