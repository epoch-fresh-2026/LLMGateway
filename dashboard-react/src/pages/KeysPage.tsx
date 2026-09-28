import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createKey, deleteKey, listKeys, resetKey, updateKey } from '../api/accounts'
import { AsyncState } from '../components/feedback/AsyncState'
import type { ClientKey } from '../types/api'

export function KeysPage() {
  const queryClient = useQueryClient()
  const keys = useQuery({ queryKey: ['keys'], queryFn: () => listKeys() })

  const [keyName, setKeyName] = useState('')
  const [error, setError] = useState('')
  const [fullKey, setFullKey] = useState('')

  const run = async (action: () => Promise<void>) => {
    setError('')
    try {
      await action()
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    }
  }

  const addKey = (event: FormEvent) => {
    event.preventDefault()
    return run(async () => {
      const created = await createKey({ key_name: keyName || 'default', prefix: 'sk-' })
      setKeyName('')
      setFullKey(created.full_key)
      await queryClient.invalidateQueries({ queryKey: ['keys'] })
    })
  }

  const toggleKey = (key: ClientKey) => run(async () => {
    await updateKey(key.id, { is_active: !key.is_active })
    await queryClient.invalidateQueries({ queryKey: ['keys'] })
  })

  const rotateKey = (key: ClientKey) => run(async () => {
    const rotated = await resetKey(key.id)
    setFullKey(rotated.full_key)
  })

  const removeKey = (key: ClientKey) => run(async () => {
    await deleteKey(key.id)
    await queryClient.invalidateQueries({ queryKey: ['keys'] })
  })

  return <div className="bento-grid">
    <section className="panel span-12">
      <div className="panel-head"><div><h3>API 密钥</h3><p className="muted">仅本账号可用，创建/重置只显示一次明文</p></div></div>
      <form className="form-grid" onSubmit={addKey}>
        <label><span>名称</span><input value={keyName} placeholder="default" onChange={event => setKeyName(event.target.value)} /></label>
        <button className="button primary" type="submit">新建 Key</button>
      </form>
      {fullKey && <div className="secret"><code>{fullKey}</code><button className="button ghost" type="button" onClick={() => setFullKey('')}>隐藏</button></div>}
      <AsyncState loading={keys.isLoading} error={keys.error} hasData={Boolean(keys.data)} onRetry={() => void keys.refetch()} />
      {keys.data && <div className="table-wrap"><table>
        <thead><tr><th>名称</th><th>前缀</th><th>状态</th><th>最近使用</th><th /></tr></thead>
        <tbody>{keys.data.list.map(key => <tr key={key.id}>
          <td>{key.key_name}</td>
          <td className="mono">{key.prefix}</td>
          <td>{key.is_active ? '启用' : '停用'}</td>
          <td className="mono">{key.last_used_at ? new Date(key.last_used_at).toLocaleString('zh-CN', { hour12: false }) : '—'}</td>
          <td className="table-actions">
            <button className="button ghost" onClick={() => void toggleKey(key)}>{key.is_active ? '停用' : '启用'}</button>
            <button className="button ghost" onClick={() => void rotateKey(key)}>重置</button>
            <button className="button delete" onClick={() => void removeKey(key)}>删除</button>
          </td>
        </tr>)}</tbody>
      </table>{!keys.data.list.length && <div className="empty">暂无 Key</div>}</div>}
      {error && <div className="error action-error">{error}</div>}
    </section>
  </div>
}
