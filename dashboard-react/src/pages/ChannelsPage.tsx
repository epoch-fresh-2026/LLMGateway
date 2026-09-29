import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createChannel, createChannelModel, deleteChannel, deleteChannelBreakerConfig, deleteChannelModel,
  deleteUserBreakerConfig, getChannelBreakerConfig, getUserBreakerConfig, listChannelHealth, listChannelModels,
  listChannels, loadRemoteModels, testChannel, updateChannel, updateChannelBalance, updateChannelBreakerConfig,
  updateChannelModel, updateChannelStatus, updateUserBreakerConfig,
} from '../api/catalog'
import { AsyncState } from '../components/feedback/AsyncState'
import { Modal } from '../components/feedback/Modal'
import type { Channel, ChannelBalanceInput, ChannelCreateInput, ChannelModel, ChannelModelInput, ChannelModelUpdate, ChannelUpdateInput, ChannelTestItem, Health, RemoteModel } from '../types/api'

type Editor = Partial<Channel> | null

export function ChannelsPage() {
  const client = useQueryClient()
  const channels = useQuery({ queryKey: ['channels'], queryFn: () => listChannels(), staleTime: 60_000 })
  const health = useQuery({ queryKey: ['channel-health'], queryFn: listChannelHealth, staleTime: 30_000 })
  const [editor, setEditor] = useState<Editor | undefined>()
  const [selected, setSelected] = useState<Channel | null>(null)
  const [breaker, setBreaker] = useState<Channel | null>(null)
  const [globalBreaker, setGlobalBreaker] = useState(false)
  const [error, setError] = useState('')
  const rows = channels.data?.list || []
  const healthMap = new Map((health.data?.list || []).map(row => [row.channel_id, row]))
  const refresh = async () => { await Promise.all([channels.refetch(), health.refetch()]) }
  const action = async (fn: () => Promise<unknown>) => { try { setError(''); await fn(); await refresh() } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') } }

  return <>
    <AsyncState loading={channels.isLoading || health.isLoading} error={channels.error || health.error} hasData={Boolean(channels.data || health.data)} onRetry={() => { void refresh() }} />
    <section className="panel"><div className="panel-head"><div><h3>渠道管理</h3><p className="muted">上游渠道 · 状态 / 权重 / 优先级 / 余额</p></div><div className="panel-actions"><button className="button ghost" onClick={() => void refresh()}>刷新</button><button className="button ghost" onClick={() => setGlobalBreaker(true)}>全局熔断配置</button><button className="button primary" onClick={() => setEditor(null)}>新建渠道</button></div></div>{error && <p className="error">{error}</p>}<div className="table-wrap"><table><thead><tr><th>ID</th><th>名称</th><th>Base URL</th><th>状态</th><th>权重/优先级</th><th>余额</th><th>模型</th><th>熔断</th><th>操作</th></tr></thead><tbody>{rows.map(channel => { const state = healthMap.get(channel.id)?.state || 'closed'; return <tr key={channel.id}><td className="mono">#{channel.id}</td><td><b>{channel.name}</b></td><td className="mono channel-url">{channel.base_url}</td><td><span className={`badge ${channel.status === 1 ? 'ok-bg' : 'danger-bg'}`}>{channel.status === 1 ? '启用' : '停用'}</span></td><td className="mono">{channel.weight} / {channel.priority}</td><td className="mono">{channel.balance == null ? '不限' : `$${channel.balance}`}</td><td>{channel.model_count}</td><td>{state}</td><td className="table-actions"><button className="button ghost" onClick={() => setEditor(channel)}>编辑</button><button className="button ghost" onClick={() => setSelected(channel)}>模型映射</button><button className="button ghost" onClick={() => void action(() => updateChannelStatus(channel.id, { status: channel.status === 1 ? 0 : 1 }))}>切换状态</button><button className="button ghost" onClick={() => setBreaker(channel)}>熔断配置</button><button className="button delete" onClick={() => { if (window.confirm(`确认删除渠道「${channel.name}」？`)) void action(() => deleteChannel(channel.id)) }}>删除</button></td></tr> })}</tbody></table>{!rows.length && <div className="empty">暂无渠道</div>}</div></section>
    {editor !== undefined && <ChannelEditor channel={editor} onClose={() => setEditor(undefined)} onSaved={() => { setEditor(undefined); void refresh() }} />}
    {selected && <Mappings channel={selected} onClose={() => setSelected(null)} onChanged={() => void client.invalidateQueries({ queryKey: ['channels'] })} />}
    {breaker && <BreakerConfig channel={breaker} onClose={() => setBreaker(null)} />}
    {globalBreaker && <UserBreakerConfig onClose={() => setGlobalBreaker(false)} onChanged={() => void refresh()} />}
  </>
}

// UserBreakerConfig edits the owner-level breaker default that channels inherit
// when they have no per-channel override.
function UserBreakerConfig({ onClose, onChanged }: { onClose: () => void; onChanged: () => void }) {
  const query = useQuery({ queryKey: ['user-breaker-config'], queryFn: getUserBreakerConfig })
  return <Modal title="全局熔断配置" submitText="保存" onClose={onClose} onSubmit={async event => {
    const form = new FormData(event.currentTarget)
    await updateUserBreakerConfig({
      window_seconds: Number(form.get('window_seconds')),
      minimum_samples: Number(form.get('minimum_samples')),
      error_rate_percent: Number(form.get('error_rate_percent')),
      timeout_rate_percent: Number(form.get('timeout_rate_percent')),
      cooldown_seconds: Number(form.get('cooldown_seconds')),
    })
    onChanged()
    onClose()
  }}>
    {query.isLoading ? <div className="empty">加载中…</div> : <><p className="muted">未单独配置的渠道都继承这里的默认值。</p><div className="form-grid"><Field label="窗口秒数" name="window_seconds" type="number" defaultValue={query.data?.window_seconds ?? 60} required /><Field label="最小样本" name="minimum_samples" type="number" defaultValue={query.data?.minimum_samples ?? 10} required /><Field label="错误率(%)" name="error_rate_percent" type="number" defaultValue={query.data?.error_rate_percent ?? 50} required /><Field label="超时率(%)" name="timeout_rate_percent" type="number" defaultValue={query.data?.timeout_rate_percent ?? 50} required /><Field label="冷却秒数" name="cooldown_seconds" type="number" defaultValue={query.data?.cooldown_seconds ?? 30} required /></div><button type="button" className="button ghost" onClick={() => void deleteUserBreakerConfig().then(() => { void query.refetch(); onChanged(); onClose() })}>恢复进程默认</button></>}
  </Modal>
}

function BreakerConfig({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const query = useQuery({ queryKey: ['channel-breaker', channel.id], queryFn: () => getChannelBreakerConfig(channel.id) })
  return <Modal title={`熔断配置 · ${channel.name}`} submitText="保存" onClose={onClose} onSubmit={async event => {
    const form = new FormData(event.currentTarget)
    await updateChannelBreakerConfig(channel.id, {
      window_seconds: Number(form.get('window_seconds')),
      minimum_samples: Number(form.get('minimum_samples')),
      error_rate_percent: Number(form.get('error_rate_percent')),
      timeout_rate_percent: Number(form.get('timeout_rate_percent')),
      cooldown_seconds: Number(form.get('cooldown_seconds')),
    })
    onClose()
  }}>
    {query.isLoading ? <div className="empty">加载中…</div> : <><div className="form-grid"><Field label="窗口秒数" name="window_seconds" type="number" defaultValue={query.data?.window_seconds ?? 60} required /><Field label="最小样本" name="minimum_samples" type="number" defaultValue={query.data?.minimum_samples ?? 10} required /><Field label="错误率(%)" name="error_rate_percent" type="number" defaultValue={query.data?.error_rate_percent ?? 50} required /><Field label="超时率(%)" name="timeout_rate_percent" type="number" defaultValue={query.data?.timeout_rate_percent ?? 50} required /><Field label="冷却秒数" name="cooldown_seconds" type="number" defaultValue={query.data?.cooldown_seconds ?? 30} required /></div><button type="button" className="button ghost" onClick={() => void deleteChannelBreakerConfig(channel.id).then(() => { void query.refetch(); onClose() })}>恢复全局默认</button></>}
  </Modal>
}

function ChannelEditor({ channel, onClose, onSaved }: { channel: Editor; onClose: () => void; onSaved: () => void }) {
  return <Modal title={channel?.id ? `编辑渠道 · ${channel.name}` : '新建渠道'} submitText={channel?.id ? '保存' : '创建'} onClose={onClose} onSubmit={async event => { const form = new FormData(event.currentTarget); const apiKey = String(form.get('api_key') || ''); const name = String(form.get('name') || ''); const baseUrl = String(form.get('base_url') || ''); const input: ChannelUpdateInput = { name, base_url: baseUrl, auth_type: String(form.get('auth_type') || 'bearer'), priority: Number(form.get('priority')), weight: Number(form.get('weight')), status: Number(form.get('status')) }; if (apiKey) input.api_key = apiKey; if (form.get('balance')) input.balance = String(form.get('balance')); if (channel?.id) await updateChannel(channel.id, input); else { if (!apiKey) throw new Error('新建渠道必须填写上游 API Key'); const createInput: ChannelCreateInput = { name, base_url: baseUrl, api_key: apiKey, auth_type: input.auth_type, priority: input.priority, weight: input.weight, status: input.status, balance: input.balance }; await createChannel(createInput) } onSaved() }}><div className="form-grid"><Field label="渠道名称" name="name" defaultValue={channel?.name} required /><Field label="Base URL" name="base_url" defaultValue={channel?.base_url} required /><Field label="上游 API Key" name="api_key" required={!channel?.id} /><Field label="优先级" name="priority" type="number" defaultValue={channel?.priority ?? 0} /><Field label="权重" name="weight" type="number" defaultValue={channel?.weight ?? 100} /><Field label="余额" name="balance" defaultValue={channel?.balance || ''} /><label><span>状态</span><select name="status" defaultValue={String(channel?.status ?? 1)}><option value="1">启用</option><option value="0">停用</option></select></label></div></Modal>
}

function Mappings({ channel, onClose, onChanged }: { channel: Channel; onClose: () => void; onChanged: () => void }) {
  const query = useQuery({ queryKey: ['channel-models', channel.id], queryFn: () => listChannelModels(channel.id) })
  const [editor, setEditor] = useState<ChannelModel | null | undefined>()
  const [remote, setRemote] = useState<RemoteModel[]>([])
  const [remoteError, setRemoteError] = useState('')
  const [mapping, setMapping] = useState(false)
  const mapped = new Set((query.data?.list || []).map(row => row.model_name))
  const loadRemote = async () => {
    setRemoteError('')
    try {
      const result = await loadRemoteModels(channel.id)
      if (!result.ok) throw new Error(result.error || '拉取失败')
      setRemote(result.models || [])
    } catch (reason) {
      setRemote([])
      setRemoteError(reason instanceof Error ? reason.message : '拉取失败')
    }
  }
  // One-click mapping uses the upstream real name as both the public model name
  // and the upstream model, which is what a "same-name" channel mapping means.
  const mapModels = async (ids: string[]) => {
    const pending = ids.filter(id => id && !mapped.has(id))
    if (!pending.length) return
    setMapping(true)
    setRemoteError('')
    try {
      for (const id of pending) await createChannelModel(channel.id, { model_name: id, upstream_model: id, enabled: true })
      onChanged()
      await query.refetch()
    } catch (reason) {
      setRemoteError(reason instanceof Error ? reason.message : '映射失败')
    } finally {
      setMapping(false)
    }
  }
  const save = async (input: ChannelModelInput | ChannelModelUpdate) => { if (editor) { const updateInput: ChannelModelUpdate = { upstream_model: input.upstream_model, enabled: input.enabled }; await updateChannelModel(channel.id, editor.id, updateInput) } else { if (!('model_name' in input)) throw new Error('新建映射缺少对外模型名'); await createChannelModel(channel.id, input) } setEditor(undefined); onChanged(); await query.refetch() }
  return <>
    <Modal title={`模型映射 · ${channel.name}`} onClose={onClose}>
      <div className="modal-toolbar"><button type="button" className="button ghost" onClick={() => void loadRemote()}>拉取远端模型</button><button type="button" className="button ghost" disabled={!remote.length || mapping} onClick={() => void mapModels(remote.map(item => item.id))}>一键全部映射</button><button type="button" className="button primary" onClick={() => setEditor(null)}>手动添加映射</button></div>
      {remoteError && <p className="error">{remoteError}</p>}
      {remote.length > 0 && <div className="remote-models">{remote.map(item => { const done = mapped.has(item.id); return <button type="button" key={item.id} className="remote-model" disabled={done || mapping} onClick={() => void mapModels([item.id])}><b>{item.id}</b><small>{item.id}：{item.id}</small><span>{done ? '已映射' : '点击映射'}</span></button> })}</div>}
      {query.isLoading ? <div className="empty">加载中…</div> : <div className="table-wrap mappings-table"><table><thead><tr><th>对外模型</th><th>上游模型</th><th /></tr></thead><tbody>{query.data?.list.map(row => <tr key={row.id}><td>{row.model_name}</td><td>{row.upstream_model}</td><td><button className="button ghost" onClick={() => setEditor(row)}>编辑</button><button className="button delete" onClick={() => { if (window.confirm('删除该映射？')) void deleteChannelModel(channel.id, row.id).then(() => { onChanged(); void query.refetch() }) }}>删除</button></td></tr>)}</tbody></table></div>}
    </Modal>
    {editor !== undefined && <MappingEditor mapping={editor} onClose={() => setEditor(undefined)} onSave={save} />}
  </>
}

function MappingEditor({ mapping, onClose, onSave }: { mapping: ChannelModel | null; onClose: () => void; onSave: (input: ChannelModelInput | ChannelModelUpdate) => Promise<void> }) {
  return <Modal title={mapping ? '编辑映射' : '添加映射'} submitText="保存" onClose={onClose} onSubmit={async event => {
    const form = new FormData(event.currentTarget)
    await onSave({ model_name: String(form.get('model_name') || ''), upstream_model: String(form.get('upstream_model') || ''), enabled: form.get('enabled') === 'on' })
  }}>
    <div className="form-grid mapping-form">
      <Field label="对外模型名" name="model_name" defaultValue={mapping?.model_name} readOnly={Boolean(mapping)} required />
      <Field label="上游模型名" name="upstream_model" defaultValue={mapping?.upstream_model} required />
      <label className="checkbox"><input name="enabled" type="checkbox" defaultChecked={mapping?.enabled ?? true} /> 启用</label>
    </div>
  </Modal>
}
function Field({ label, ...props }: React.InputHTMLAttributes<HTMLInputElement> & { label: string }) { return <label><span>{label}</span><input {...props} /></label> }
