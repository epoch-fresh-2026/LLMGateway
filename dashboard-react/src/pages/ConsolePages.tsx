import { useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createRateLimit, deleteRateLimit, listRateLimits, updateRateLimit } from '../api/ratelimit'
import { createQuotaPolicy, deleteQuotaPolicy, listQuotaPolicies, listQuotaUsage } from '../api/quota'
import { createPricing, deletePricing, listChannels, listModels, listPricing } from '../api/catalog'
import { listKeys } from '../api/accounts'
import { useSession } from '../auth/AuthProvider'
import { Modal } from '../components/feedback/Modal'
import type { PricingCreateInput, QuotaPolicy, QuotaPolicyCreateInput, RateLimit, RateLimitCreateInput } from '../types/api'

function Field({ label, ...props }: React.InputHTMLAttributes<HTMLInputElement> & { label: string }) {
  return <label><span>{label}</span><input {...props} /></label>
}

export function LimitsPage() {
  const query = useQuery({ queryKey: ['rate-limits'], queryFn: () => listRateLimits() })
  const keys = useQuery({ queryKey: ['keys'], queryFn: () => listKeys({ page: 1, page_size: 1000 }) })
  const channels = useQuery({ queryKey: ['channels'], queryFn: () => listChannels({ page: 1, page_size: 1000 }) })
  const models = useQuery({ queryKey: ['models'], queryFn: () => listModels({ status: 1, page: 1, page_size: 1000 }) })
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [targetType, setTargetType] = useState<RateLimitCreateInput['target_type']>('user')
  const [targetValue, setTargetValue] = useState('*')

  const keyNames = new Map((keys.data?.list || []).map(key => [key.id, key.key_name]))
  const channelNames = new Map((channels.data?.list || []).map(channel => [channel.id, channel.name]))
  const targetLabel = (rule: RateLimit) => {
    if (rule.target_type === 'user') return '本人'
    if (rule.target_type === 'api_key') return rule.target_value === '*' ? '全部密钥' : `密钥 ${keyNames.get(Number(rule.target_value)) ?? `#${rule.target_value}`}`
    if (rule.target_type === 'model') return rule.target_value === '*' ? '全部模型' : `模型 ${rule.target_value}`
    if (rule.target_type === 'channel') return rule.target_value === '*' ? '全部渠道' : `渠道 ${channelNames.get(Number(rule.target_value)) ?? `#${rule.target_value}`}`
    return rule.target_value
  }

  const changeTarget = (value: string) => {
    setTargetType(value as RateLimitCreateInput['target_type'])
    if (value === 'api_key') setTargetValue(keys.data?.list[0] ? String(keys.data.list[0].id) : '*')
    else if (value === 'model') setTargetValue(models.data?.list[0] ? models.data.list[0].model_name : '*')
    else if (value === 'channel') setTargetValue(channels.data?.list[0] ? String(channels.data.list[0].id) : '*')
    else setTargetValue('*')
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    try {
      setError('')
      await createRateLimit({
        rule_name: String(form.get('rule_name') || ''),
        target_type: targetType,
        target_value: targetType === 'user' ? '*' : targetValue,
        metric: String(form.get('metric') || 'rpm') as RateLimitCreateInput['metric'],
        limit_value: Number(form.get('limit_value')),
        window_seconds: Number(form.get('window_seconds')),
        action: 'reject',
        priority: Number(form.get('priority') || 0),
        enabled: form.get('enabled') === 'on',
      })
      setCreating(false)
      await query.refetch()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '创建失败')
    }
  }

  return <section className="panel">
    <div className="panel-head"><div><h3>限流规则</h3><p className="muted">rate_limit_rules · 短窗口速率控制</p></div><div className="panel-actions"><button className="button ghost" onClick={() => void query.refetch()}>刷新</button><button className="button primary" onClick={() => { setError(''); setTargetType('user'); setTargetValue('*'); setCreating(true) }}>新建</button></div></div>
    {error && !creating && <p className="error">{error}</p>}
    {query.isLoading ? <div className="empty">加载中…</div> : <div className="table-wrap"><table><thead><tr><th>规则名</th><th>作用域</th><th>目标</th><th>指标</th><th>限额</th><th>状态</th><th /></tr></thead><tbody>{query.data?.list.map(rule => <tr key={rule.id}><td>{rule.rule_name}</td><td>{rule.target_type}</td><td>{targetLabel(rule)}</td><td>{rule.metric}</td><td>{rule.limit_value} / {rule.window_seconds}s</td><td>{rule.enabled ? '启用' : '停用'}</td><td><button className="button ghost" onClick={() => void updateRateLimit(rule.id, { enabled: !rule.enabled }).then(() => query.refetch())}>切换</button><button className="button delete" onClick={() => void deleteRateLimit(rule.id).then(() => query.refetch())}>删除</button></td></tr>)}</tbody></table></div>}
    {creating && <Modal title="新建限流规则" submitText="创建" onClose={() => setCreating(false)} onSubmit={submit}><div className="form-grid">{error && <p className="error">{error}</p>}<Field label="规则名" name="rule_name" required /><label><span>作用域</span><select value={targetType} onChange={event => changeTarget(event.target.value)}><option value="user">user（当前用户）</option><option value="api_key">api_key（指定密钥）</option><option value="model">model（指定模型）</option><option value="channel">channel（指定渠道）</option></select></label><label><span>目标值</span>{targetType === 'user' ? <input value="本人（当前用户全部请求）" readOnly disabled /> : targetType === 'api_key' ? <select value={targetValue} onChange={event => setTargetValue(event.target.value)}><option value="*">全部密钥（*）</option>{keys.data?.list.map(key => <option key={key.id} value={String(key.id)}>{key.key_name} #{key.id}</option>)}</select> : targetType === 'model' ? <select value={targetValue} onChange={event => setTargetValue(event.target.value)}><option value="*">全部模型（*）</option>{models.data?.list.map(model => <option key={model.model_name} value={model.model_name}>{model.model_name}</option>)}</select> : <select value={targetValue} onChange={event => setTargetValue(event.target.value)}><option value="*">全部渠道（*）</option>{channels.data?.list.map(channel => <option key={channel.id} value={String(channel.id)}>{channel.name}</option>)}</select>}</label><label><span>指标</span><select name="metric" defaultValue="rpm"><option value="rpm">rpm</option><option value="tpm">tpm</option><option value="concurrency">concurrency</option></select></label><Field label="限额" name="limit_value" type="number" min={1} required /><Field label="窗口秒数" name="window_seconds" type="number" min={1} required /><Field label="优先级" name="priority" type="number" defaultValue={0} /><label className="checkbox"><input name="enabled" type="checkbox" defaultChecked /> 启用</label></div></Modal>}
  </section>
}

export function QuotasPage() {
  const { account } = useSession()
  const policies = useQuery({ queryKey: ['quota-policies'], queryFn: () => listQuotaPolicies() })
  const usage = useQuery({ queryKey: ['quota-usage'], queryFn: () => listQuotaUsage() })
  const keys = useQuery({ queryKey: ['keys'], queryFn: () => listKeys({ page: 1, page_size: 1000 }) })
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [scopeType, setScopeType] = useState<QuotaPolicyCreateInput['scope_type']>('user')
  const [scopeKeyId, setScopeKeyId] = useState('')

  const keyNames = new Map((keys.data?.list || []).map(key => [key.id, key.key_name]))
  const scopeLabel = (policy: QuotaPolicy) => policy.scope_type === 'user'
    ? `用户 ${account?.username ?? `#${policy.scope_id}`}`
    : `密钥 ${keyNames.get(policy.scope_id) ?? `#${policy.scope_id}`}`

  const changeScope = (value: string) => {
    setScopeType(value as QuotaPolicyCreateInput['scope_type'])
    setScopeKeyId(value === 'api_key' ? String(keys.data?.list[0]?.id ?? '') : '')
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const tokenLimit = form.get('token_limit') ? Number(form.get('token_limit')) : undefined
    const costLimit = String(form.get('cost_limit') || '')
    try {
      setError('')
      if (tokenLimit === undefined && !costLimit) throw new Error('token 限额与费用限额至少填写一项')
      const scopeId = scopeType === 'user' ? account?.id : Number(scopeKeyId)
      if (!scopeId) throw new Error(scopeType === 'user' ? '当前用户信息缺失' : '请选择密钥')
      const base = {
        policy_name: String(form.get('policy_name') || ''),
        scope_type: scopeType,
        scope_id: scopeId,
        period_type: String(form.get('period_type') || 'day') as QuotaPolicyCreateInput['period_type'],
        enabled: form.get('enabled') === 'on',
      }
      const input: QuotaPolicyCreateInput = tokenLimit === undefined
        ? { ...base, cost_limit: costLimit }
        : { ...base, token_limit: tokenLimit, ...(costLimit ? { cost_limit: costLimit } : {}) }
      await createQuotaPolicy(input)
      setCreating(false)
      await policies.refetch()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '创建失败')
    }
  }

  return <section className="panel">
    <div className="panel-head"><div><h3>周期配额</h3><p className="muted">UTC 日/月 token 与费用额度</p></div><div className="panel-actions"><button className="button ghost" onClick={() => { void policies.refetch(); void usage.refetch() }}>刷新</button><button className="button primary" onClick={() => { setError(''); setScopeType('user'); setScopeKeyId(''); setCreating(true) }}>新建</button></div></div>
    {error && !creating && <p className="error">{error}</p>}
    {policies.isLoading ? <div className="empty">加载中…</div> : <div className="table-wrap"><table><thead><tr><th>策略</th><th>作用域</th><th>周期</th><th>状态</th><th /></tr></thead><tbody>{policies.data?.list.map(policy => <tr key={policy.id}><td>{policy.policy_name}</td><td>{scopeLabel(policy)}</td><td>{policy.period_type}</td><td>{policy.enabled ? '启用' : '停用'}</td><td><button className="button delete" onClick={() => void deleteQuotaPolicy(policy.id).then(() => policies.refetch())}>删除</button></td></tr>)}</tbody></table></div>}
    {creating && <Modal title="新建周期配额" submitText="创建" onClose={() => setCreating(false)} onSubmit={submit}><div className="form-grid">{error && <p className="error">{error}</p>}<Field label="策略名" name="policy_name" required /><label><span>作用域</span><select value={scopeType} onChange={event => changeScope(event.target.value)}><option value="user">user（当前用户）</option><option value="api_key">api_key（指定密钥）</option></select></label><label><span>作用域对象</span>{scopeType === 'user' ? <input value={`当前用户 #${account?.id ?? '—'} ${account?.username ?? ''}`} readOnly disabled /> : <select value={scopeKeyId} onChange={event => setScopeKeyId(event.target.value)}>{keys.data?.list.length ? keys.data.list.map(key => <option key={key.id} value={String(key.id)}>{key.key_name} #{key.id}</option>) : <option value="">（暂无可选密钥）</option>}</select>}</label><label><span>周期</span><select name="period_type" defaultValue="day"><option value="day">day</option><option value="month">month</option></select></label><Field label="Token 限额（可选）" name="token_limit" type="number" min={1} /><Field label="费用限额（可选）" name="cost_limit" /><label className="checkbox"><input name="enabled" type="checkbox" defaultChecked /> 启用</label></div></Modal>}
  </section>
}

export function PricingPage() {
  const query = useQuery({ queryKey: ['pricing'], queryFn: () => listPricing() })
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    try {
      setError('')
      const cached = String(form.get('cached_input_price_per_1m') || '')
      const input: PricingCreateInput = {
        channel_id: Number(form.get('channel_id')),
        model_name: String(form.get('model_name') || ''),
        input_price_per_1m: String(form.get('input_price_per_1m') || ''),
        output_price_per_1m: String(form.get('output_price_per_1m') || ''),
        currency: String(form.get('currency') || 'USD'),
      }
      if (cached) input.cached_input_price_per_1m = cached
      await createPricing(input)
      setCreating(false)
      await query.refetch()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '创建失败')
    }
  }

  return <section className="panel">
    <div className="panel-head"><div><h3>计费定价</h3><p className="muted">渠道×模型单价</p></div><div className="panel-actions"><button className="button ghost" onClick={() => void query.refetch()}>刷新</button><button className="button primary" onClick={() => { setError(''); setCreating(true) }}>新建</button></div></div>
    {error && !creating && <p className="error">{error}</p>}
    {query.isLoading ? <div className="empty">加载中…</div> : <div className="table-wrap"><table><thead><tr><th>渠道</th><th>模型</th><th>输入</th><th>输出</th><th /></tr></thead><tbody>{query.data?.list.map(row => <tr key={row.id}><td>{row.channel_name}</td><td>{row.model_name}</td><td>{row.input_price_per_1m}</td><td>{row.output_price_per_1m}</td><td><button className="button delete" onClick={() => void deletePricing({ channel_id: row.channel_id, model_name: row.model_name }).then(() => query.refetch())}>删除</button></td></tr>)}</tbody></table></div>}
    {creating && <Modal title="新建计费定价" submitText="创建" onClose={() => setCreating(false)} onSubmit={submit}><div className="form-grid">{error && <p className="error">{error}</p>}<Field label="渠道 ID" name="channel_id" type="number" min={1} required /><Field label="模型名" name="model_name" required /><Field label="输入单价 / 1M" name="input_price_per_1m" placeholder="0.100000" required /><Field label="输出单价 / 1M" name="output_price_per_1m" placeholder="0.200000" required /><Field label="缓存输入单价 / 1M（可选）" name="cached_input_price_per_1m" placeholder="0.000000" /><Field label="币种" name="currency" defaultValue="USD" required /></div></Modal>}
  </section>
}
