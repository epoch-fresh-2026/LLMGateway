import { useEffect, useState } from 'react'
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { useSession } from './auth/AuthProvider'
import { DashboardPage } from './pages/DashboardPage'
import { LogsPage } from './pages/LogsPage'
import { ChannelsPage } from './pages/ChannelsPage'
import { ProfilePage } from './pages/ProfilePage'
import { ModelsPage } from './pages/ModelsPage'
import { LimitsPage, QuotasPage, PricingPage, SettingsPage } from './pages/ConsolePages'
import { UsagePage } from './pages/UsagePage'
import { DocsPage } from './pages/DocsPage'
import { LoginPage } from './pages/LoginPage'

const navGroups = [
  { label: '总览', items: [['/', '仪表盘', '▦'], ['/usage', '用量统计', '⌁'], ['/logs', '请求日志', '☷'], ['/docs', 'API 文档', '▤']] },
  { label: '资源管理', items: [['/channels', '渠道管理', '≋'], ['/models', '模型路由', '◈'], ['/profile', '我的资料', '♙']] },
  { label: '运营', items: [['/limits', '限流规则', '◴'], ['/quotas', '周期配额', '◉'], ['/pricing', '计费定价', '◈'], ['/settings', '系统设置', '⚙']] },
] as const

const titles: Record<string, [string, string]> = {
  '/': ['运营仪表盘', '网关运行总览'], '/usage': ['用量统计', '按用户 / 模型聚合的 token 与费用'], '/logs': ['请求日志', 'usage_logs 明细'],
  '/docs': ['API 文档', '下游与管理端接口文档（应用内阅读）'], '/channels': ['渠道管理', '上游渠道 · 健康 / 权重 / 余额'], '/models': ['模型路由', '对外模型目录与路由策略'],
  '/profile': ['我的资料', '账户资料与网关 Key'], '/limits': ['限流规则', 'rate_limit_rules · 短窗口速率控制'], '/quotas': ['周期配额', 'UTC 日/月 token 与费用额度'],
  '/pricing': ['计费定价', '渠道×模型单价'], '/settings': ['系统设置', '控制台与网关信息'],
}

export default function App() {
  const { account, loading } = useSession()

  if (loading) {
    return <div className="auth-page"><div className="auth-card"><p>加载中…</p></div></div>
  }
  if (!account) {
    return <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="*" element={<Navigate to="/login" replace />} />
    </Routes>
  }
  return <AppShell />
}

function AppShell() {
  const location = useLocation()
  const navigate = useNavigate()
  const { account, logout } = useSession()
  const [dark, setDark] = useState(true)
  const [notice, setNotice] = useState(false)
  const [title, subtitle] = titles[location.pathname] || titles['/']

  useEffect(() => { document.documentElement.classList.toggle('light', !dark) }, [dark])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        document.getElementById('global-search')?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const initials = account ? account.username.slice(0, 2).toUpperCase() : 'OP'

  return <div className="app-shell">
    <aside className="sidebar"><div className="brand"><span className="brand-mark">ϟ</span><div><b>MyApi</b><small>LLM API 网关</small></div></div><nav className="nav">{navGroups.map(group => <div className="nav-group" key={group.label}><div className="nav-label">{group.label}</div>{group.items.map(([path, label, icon]) => <NavLink key={path} to={path} className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}><span className="nav-icon">{icon}</span><span>{label}</span>{path === '/logs' && <em>1.7k</em>}</NavLink>)}</div>)}</nav><div className="gateway-status"><div><i className="dot-live" />网关运行状态</div><div className="status-stats"><span><b>—</b><small>TTFT</small></span><span><b className="green">99.98%</b><small>30 天可用性</small></span></div></div></aside>
    <main className="main-area"><header className="topbar"><div className="crumb"><span>MyApi</span><b>/</b><span>Gateway</span><b>/</b><strong>{title}</strong></div><div className="top-actions"><div className="search"><span>⌕</span><input id="global-search" placeholder="搜索用户 / 渠道 / 模型…" /><kbd>⌘K</kbd></div><button className="icon-button" onClick={() => setDark(!dark)} aria-label="切换主题">{dark ? '☼' : '◐'}</button><div className="notice-wrap"><button className="icon-button" onClick={() => setNotice(!notice)} aria-label="通知">♧<i>3</i></button>{notice && <div className="notice-panel"><b>通知（3）</b><p><mark>警告</mark>渠道余额接近阈值</p><p><mark className="success">结算</mark>今日账单已完成</p><p><mark>路由</mark>模型路由策略已更新</p></div>}</div><button className="profile"><span>{initials}</span><label>{account?.username ?? '未登录'}<small>自助控制台</small></label></button><button className="icon-button" onClick={() => { void logout() }} aria-label="退出登录">⏻</button></div></header><section className="page-content"><div className="page-heading"><div><h1>{title}</h1><p>{subtitle}{location.pathname === '/' && <> · 数据更新于 <b>实时</b></>}</p></div><div className="page-actions">{location.pathname !== '/docs' && <button className="button ghost" onClick={() => navigate('/docs')}>▤ 查看 API 文档</button>}<button className="button primary" onClick={() => navigate('/channels')}>ϟ 新建渠道</button></div></div><Routes><Route path="/login" element={<Navigate to="/" replace />} /><Route path="/" element={<DashboardPage />} /><Route path="/usage" element={<UsagePage />} /><Route path="/logs" element={<LogsPage />} /><Route path="/channels" element={<ChannelsPage />} /><Route path="/profile" element={<ProfilePage />} /><Route path="/models" element={<ModelsPage />} /><Route path="/limits" element={<LimitsPage />} /><Route path="/quotas" element={<QuotasPage />} /><Route path="/pricing" element={<PricingPage />} /><Route path="/settings" element={<SettingsPage />} /><Route path="/docs" element={<DocsPage />} /></Routes></section></main>
  </div>
}
