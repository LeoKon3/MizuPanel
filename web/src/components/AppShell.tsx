import type { ReactNode } from 'react'
import { Activity, Bell, Blocks, ClipboardList, LayoutDashboard, LogOut, Moon, PanelLeftClose, PanelLeftOpen, Server, Settings, ShieldCheck, Sun } from 'lucide-react'
import { BrandLogo } from './BrandLogo'
import type { AppPage } from '../routes'

type NavPage = Exclude<AppPage, 'history' | 'logs' | 'ai'>
const items = [
  { page: 'overview', label: '概览', icon: LayoutDashboard },
  { page: 'hosts', label: '主机', icon: Server },
  { page: 'services', label: '应用服务', icon: Blocks },
  { page: 'k8s', label: 'Kubernetes', icon: Blocks },
  { page: 'alerts', label: '告警', icon: Bell, gap: true },
  { page: 'uptime', label: '服务拨测', icon: Activity },
  { page: 'tasks', label: '任务中心', icon: ClipboardList, gap: true },
  { page: 'audit', label: '审计日志', icon: ShieldCheck },
  { page: 'settings', label: '系统设置', icon: Settings, bottom: true }
] satisfies Array<{ page: NavPage; label: string; icon: typeof Server; gap?: boolean; bottom?: boolean }>

type Props = {
  page: AppPage; collapsed: boolean; onCollapse: () => void; onNavigate: (page: AppPage) => void
  theme: 'light' | 'dark'; onTheme: (theme: 'light' | 'dark') => void
  username?: string; onLogout: () => void; aiButton: ReactNode; children: ReactNode
}

export function AppShell({ page, collapsed, onCollapse, onNavigate, theme, onTheme, username, onLogout, aiButton, children }: Props) {
  return <div className={`console-shell ${collapsed ? 'is-collapsed' : ''}`}>
    <aside aria-label="MizuPanel 侧边栏" data-collapsed={collapsed} className="console-sidebar">
      <button type="button" className="console-brand" onClick={() => onNavigate('overview')} aria-label="MizuPanel 首页"><BrandLogo className="h-7 w-7 shrink-0" />{!collapsed && <span>MizuPanel</span>}</button>
      <nav aria-label="侧边导航">{items.map(item => <button type="button" key={item.page} title={item.label} aria-label={item.label} aria-current={page === item.page ? 'page' : undefined} onClick={() => onNavigate(item.page)} className={`console-nav-item ${'gap' in item ? 'nav-gap' : ''} ${'bottom' in item ? 'nav-bottom' : ''}`}>
        <item.icon size={17} aria-hidden="true" />{!collapsed && <span data-testid="sidebar-nav-label">{item.label}</span>}
      </button>)}</nav>
      <button type="button" className="console-collapse" aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'} onClick={onCollapse}>{collapsed ? <PanelLeftOpen size={16} /> : <><PanelLeftClose size={16} /><span>收起侧边栏</span></>}</button>
    </aside>
    <div className="console-workspace">
      <header className="console-topbar">
        <div className="flex min-w-0 items-center gap-3 text-xs text-muted-foreground"><span className="font-medium">运维工作区</span></div>
        <div className="flex items-center gap-2">{aiButton}
          {username && <><span className="ml-2 text-xs text-muted-foreground">{username}</span><button type="button" className="console-icon-button" aria-label="退出登录" title="退出登录" onClick={onLogout}><LogOut size={16} /></button></>}
          <div className="console-theme" aria-label="主题切换">{(['light', 'dark'] as const).map(item => <button type="button" key={item} aria-label={item === 'light' ? 'Light' : 'Dark'} title={item === 'light' ? '浅色主题' : '深色主题'} aria-pressed={theme === item} onClick={() => onTheme(item)}>{item === 'light' ? <Sun size={16} /> : <Moon size={16} />}</button>)}</div>
        </div>
      </header>
      <main className="console-sheet">{children}</main>
    </div>
  </div>
}
