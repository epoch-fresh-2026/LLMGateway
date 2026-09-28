import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getProfile, updateProfile } from '../api/accounts'
import { AsyncState } from '../components/feedback/AsyncState'
import type { ProfileUpdateInput } from '../types/api'

export function ProfilePage() {
  const queryClient = useQueryClient()
  const profile = useQuery({ queryKey: ['profile'], queryFn: getProfile })

  const [nickname, setNickname] = useState('')
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const run = async (action: () => Promise<void>) => {
    setError('')
    setMessage('')
    try {
      await action()
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    }
  }

  const saveProfile = (event: FormEvent) => {
    event.preventDefault()
    return run(async () => {
      const input: ProfileUpdateInput = {}
      if (nickname) input.nickname = nickname
      if (newPassword) {
        input.current_password = currentPassword
        input.new_password = newPassword
      }
      await updateProfile(input)
      setNickname('')
      setCurrentPassword('')
      setNewPassword('')
      setMessage('资料已更新')
      await queryClient.invalidateQueries({ queryKey: ['profile'] })
    })
  }

  return <div className="bento-grid">
    <section className="panel span-12">
      <div className="panel-head"><div><h3>我的资料</h3><p className="muted">修改昵称与登录密码</p></div></div>
      <AsyncState loading={profile.isLoading} error={profile.error} hasData={Boolean(profile.data)} onRetry={() => void profile.refetch()} />
      {profile.data && <form className="form-grid" onSubmit={saveProfile}>
        <label><span>用户名</span><input value={profile.data.username} readOnly /></label>
        <label><span>昵称（留空不改）</span><input value={nickname} placeholder={profile.data.nickname} onChange={event => setNickname(event.target.value)} /></label>
        <label><span>当前密码（改密码时必填）</span><input type="password" value={currentPassword} autoComplete="current-password" onChange={event => setCurrentPassword(event.target.value)} /></label>
        <label><span>新密码（留空不改）</span><input type="password" value={newPassword} autoComplete="new-password" onChange={event => setNewPassword(event.target.value)} /></label>
        <button className="button primary" type="submit">保存</button>
      </form>}
      {message && <div className="muted">{message}</div>}
      {error && <div className="error action-error">{error}</div>}
    </section>
  </div>
}
