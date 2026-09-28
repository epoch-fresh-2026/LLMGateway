import { Navigate, NavLink, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { useSession } from './auth/AuthProvider'
import { DashboardPage } from './pages/DashboardPage'
import { LogsPage } from './pages/LogsPage'
import { ChannelsPage } from './pages/ChannelsPage'
import { ProfilePage } from './pages/ProfilePage'
import { LimitsPage, QuotasPage, PricingPage } from './pages/ConsolePages'
import { UsagePage } from './pages/UsagePage'
import { DocsPage } from './pages/DocsPage'
import { LoginPage } from './pages/LoginPage'

const navGroups = [
  { label: '总览', items: [['/', '仪表盘', '▦'], ['/usage', '用量统计', '⌁'], ['/logs', '请求日志', '☷'], ['/docs', 'API 文档', '▤']] },
  { label: '资源管理', items: [['/channels', '渠道管理', '≋'], ['/profile', '我的资料', '♙']] },
  { label: '运营', items: [['/limits', '限流规则', '◴'], ['/quotas', '周期配额', '◉'], ['/pricing', '计费定价', '◈']] },
] as const

const titles: Record<string, [string, string]> = {
  '/': ['运营仪表盘', '网关运行总览'], '/usage': ['用量统计', '按用户 / 模型聚合的 token 与费用'], '/logs': ['请求日志', 'usage_logs 明细'],
  '/docs': ['API 文档', '下游与管理端接口文档（应用内阅读）'], '/channels': ['渠道管理', '上游渠道 · 健康 / 权重 / 余额'],
  '/profile': ['我的资料', '账户资料与网关 Key'], '/limits': ['限流规则', 'rate_limit_rules · 短窗口速率控制'], '/quotas': ['周期配额', 'UTC 日/月 token 与费用额度'],
  '/pricing': ['计费定价', '渠道×模型单价'],
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
  const [title, subtitle] = titles[location.pathname] || titles['/']

  const initials = account ? account.username.slice(0, 2).toUpperCase() : 'OP'

  return <div className="app-shell">
    <aside className="sidebar"><div className="brand"><span className="brand-mark">ϟ</span><div><b>MyApi</b><small>LLM API 网关</small></div></div><nav className="nav">{navGroups.map(group => <div className="nav-group" key={group.label}><div className="nav-label">{group.label}</div>{group.items.map(([path, label, icon]) => <NavLink key={path} to={path} className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}><span className="nav-icon">{icon}</span><span>{label}</span></NavLink>)}</div>)}</nav></aside>
    <main className="main-area"><header className="topbar"><div className="crumb"><span>MyApi</span><b>/</b><span>Gateway</span><b>/</b><strong>{title}</strong></div><div className="top-actions"><button className="profile"><span>{initials}</span><label>{account?.username ?? '未登录'}<small>自助控制台</small></label></button><button className="icon-button" onClick={() => { void logout() }} aria-label="退出登录">⏻</button></div></header><section className="page-content"><div className="page-heading"><div><h1>{title}</h1><p>{subtitle}{location.pathname === '/' && <> · 数据更新于 <b>实时</b></>}</p></div><div className="page-actions">{location.pathname !== '/docs' && <button className="button ghost" onClick={() => navigate('/docs')}>▤ 查看 API 文档</button>}<button className="button primary" onClick={() => navigate('/channels')}>ϟ 新建渠道</button></div></div><Routes><Route path="/login" element={<Navigate to="/" replace />} /><Route path="/" element={<DashboardPage />} /><Route path="/usage" element={<UsagePage />} /><Route path="/logs" element={<LogsPage />} /><Route path="/channels" element={<ChannelsPage />} /><Route path="/profile" element={<ProfilePage />} /><Route path="/limits" element={<LimitsPage />} /><Route path="/quotas" element={<QuotasPage />} /><Route path="/pricing" element={<PricingPage />} /><Route path="/docs" element={<DocsPage />} /></Routes></section></main>
  </div>
}
