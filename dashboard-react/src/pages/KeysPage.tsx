import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Plus, Trash2 } from 'lucide-react'
import { createKey, deleteKey, listKeys } from '../api/accounts'
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
  const [submitting, setSubmitting] = useState(false)
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState('')

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
    if (submitting) return
    const form = new FormData(event.currentTarget)
    setSubmitting(true)
    try {
      await run(async () => {
        const created = await createKey({ key_name: String(form.get('key_name') || '').trim() || 'default', prefix: 'sk-' })
        setCreating(false)
        setCopied(false)
        setCopyError('')
        setFullKey(created.full_key)
        await queryClient.invalidateQueries({ queryKey: ['keys'] })
      })
    } finally {
      setSubmitting(false)
    }
  }

  const copyKey = async () => {
    setCopyError('')
    try {
      await navigator.clipboard.writeText(fullKey)
      setCopied(true)
    } catch {
      setCopyError('复制失败，请选中密钥后手动复制。')
    }
  }

  const closeSecret = () => { setFullKey(''); setCopied(false); setCopyError('') }

  const removeKey = (key: ClientKey) => run(async () => {
    await deleteKey(key.id)
    await queryClient.invalidateQueries({ queryKey: ['keys'] })
  })

  return <div className="bento-grid">
    <section className="panel span-12 keys-panel">
      <div className="panel-head"><div><h3>API 密钥</h3><p className="muted">API 密钥仅在创建时可查看，请妥善保存。</p></div><div className="panel-actions"><button className="button primary key-create-button" onClick={() => { setError(''); setCreating(true) }}><Plus size={14} aria-hidden="true" />新建</button></div></div>
      <AsyncState loading={keys.isLoading} error={keys.error} hasData={Boolean(keys.data)} onRetry={() => void keys.refetch()} />
      {keys.data && <div className="table-wrap"><table>
        <thead><tr><th>序号</th><th>名称</th><th>Key</th><th>创建日期</th><th>最近使用</th><th aria-label="操作" /></tr></thead>
        <tbody>{keys.data.list.map((key, index) => <tr key={key.id}>
          <td className="mono">#{index + 1}</td>
          <td>{key.key_name}</td>
          <td className="mono">{key.prefix}{key.key_suffix.substring(0, 5)}*****{key.key_suffix.substring(5)}</td>
          <td className="mono">{new Date(key.created_at).toLocaleDateString('sv-SE')}</td>
          <td className="mono">{key.last_used_at ? new Date(key.last_used_at).toLocaleString('zh-CN', { hour12: false }) : '—'}</td>
          <td className="table-actions">
            <button className="icon-button key-delete-button" title="删除密钥" aria-label={`删除密钥 ${key.key_name}`} onClick={() => void removeKey(key)}><Trash2 size={15} aria-hidden="true" /></button>
          </td>
        </tr>)}</tbody>
      </table>{!keys.data.list.length && <div className="empty">暂无 Key</div>}</div>}
      {error && !creating && <div className="error action-error">{error}</div>}
      {creating && <Modal title="创建 API key" submitText="创建" submitting={submitting} onClose={() => { if (!submitting) setCreating(false) }} onSubmit={addKey}><div className="form-grid">{error && <p className="error" role="alert">{error}</p>}<Field label="名称" name="key_name" placeholder="default" maxLength={100} /></div></Modal>}
      {fullKey && <Modal title="创建 API key" className="key-secret-modal" showActions={false} onClose={closeSecret}>
        <p className="key-secret-description">请将此 API key 保存在安全且易于访问的地方。出于安全原因，你将无法再次查看它。如果丢失了这个 key，需要重新创建。</p>
        <div className="key-secret-value"><input aria-label="新创建的 API key" value={fullKey} readOnly spellCheck={false} onFocus={event => event.currentTarget.select()} /><button type="button" className="key-copy-button" onClick={() => void copyKey()}>{copied ? <Check size={15} aria-hidden="true" /> : <Copy size={15} aria-hidden="true" />}{copied ? '已复制' : '复制'}</button></div>
        <div className="key-copy-feedback" aria-live="polite">{copyError ? <span className="error">{copyError}</span> : copied ? 'API key 已复制' : ''}</div>
        <p className="key-secret-warning">提示：不要与他人共享你的 API key，或将其暴露在浏览器或其他客户端代码中。</p>
      </Modal>}
    </section>
  </div>
}
