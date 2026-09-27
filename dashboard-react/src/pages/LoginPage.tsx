import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useSession } from '../auth/AuthProvider'

type Mode = 'login' | 'register'

export function LoginPage() {
  const { login, register } = useSession()
  const navigate = useNavigate()
  const [mode, setMode] = useState<Mode>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      if (mode === 'login') {
        await login({ username, password })
      } else {
        await register({ username, password })
      }
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  return <div className="auth-page">
    <form className="auth-card" onSubmit={submit}>
      <div className="brand"><span className="brand-mark">ϟ</span><div><b>MyApi</b><small>LLM API 网关</small></div></div>
      <h1>{mode === 'login' ? '登录控制台' : '注册账户'}</h1>
      <p className="auth-hint">{mode === 'login' ? '使用用户名和密码登录。' : '用户名 3–64 字符，密码至少 8 位。'}</p>
      <label>用户名<input value={username} autoComplete="username" onChange={(event) => setUsername(event.target.value)} /></label>
      <label>密码<input type="password" value={password} autoComplete={mode === 'login' ? 'current-password' : 'new-password'} onChange={(event) => setPassword(event.target.value)} /></label>
      {error && <p className="auth-error" role="alert">{error}</p>}
      <button className="button primary" type="submit" disabled={busy}>{busy ? '处理中…' : mode === 'login' ? '登录' : '注册并登录'}</button>
      <button type="button" className="button ghost" onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError('') }}>
        {mode === 'login' ? '没有账户？注册' : '已有账户？登录'}
      </button>
    </form>
  </div>
}
