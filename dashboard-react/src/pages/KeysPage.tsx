import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createKey, deleteKey, listKeys, resetKey, updateKey } from '../api/accounts'
import { AsyncState } from '../components/feedback/AsyncState'
import { Modal } from '../components/feedback/Modal'
import type { ClientKey } from '../types/api'

function Field({ label, ...props }: React.InputHTMLAttributes<HTMLInputElement> & { label: string }) {
  return <label><span>{label}</span><input {...props} /></label>
}

export function KeysPage() {
  const queryClient = useQueryClient()
  const keys = useQuery({ queryKey: ['keys'], queryFn: () => listKeys({ page: 1, page_size: 1000 }) })

  const [creating, setCreating] = useState(false)
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

  const addKey = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    await run(async () => {
      const created = await createKey({ key_name: String(form.get('key_name') || '') || 'default', prefix: 'sk-' })
      setFullKey(created.full_key)
      await queryClient.invalidateQueries({ queryKey: ['keys'] })
      setCreating(false)
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
      <div className="panel-head"><div><h3>API 密钥</h3><p className="muted">仅本账号可用，创建/重置只显示一次明文</p></div><div className="panel-actions"><button className="button primary" onClick={() => { setError(''); setCreating(true) }}>新建</button></div></div>
      {fullKey && <div className="secret"><code>{fullKey}</code><button className="button ghost" type="button" onClick={() => setFullKey('')}>隐藏</button></div>}
      <AsyncState loading={keys.isLoading} error={keys.error} hasData={Boolean(keys.data)} onRetry={() => void keys.refetch()} />
      {keys.data && <div className="table-wrap"><table>
        <thead><tr><th>序号</th><th>名称</th><th>前缀</th><th>状态</th><th>最近使用</th><th /></tr></thead>
        <tbody>{keys.data.list.map((key, index) => <tr key={key.id}>
          <td className="mono">#{index + 1}</td>
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
      {error && !creating && <div className="error action-error">{error}</div>}
      {creating && <Modal title="新建 Key" submitText="创建" onClose={() => setCreating(false)} onSubmit={addKey}><div className="form-grid">{error && <p className="error">{error}</p>}<Field label="名称" name="key_name" placeholder="default" /></div></Modal>}
    </section>
  </div>
}
