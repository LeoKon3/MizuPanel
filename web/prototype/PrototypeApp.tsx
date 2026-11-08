import { useCallback, useEffect, useRef, useState } from 'react'
import { Activity, ArrowLeft, ArrowUpRight, Blocks, Bot, Check, ChevronRight, ClipboardList, LayoutDashboard, Menu, Moon, Search, Send, Server, Settings, ShieldCheck, Sun, TriangleAlert, X } from 'lucide-react'
import { BrandLogo } from '../src/components/BrandLogo'
import { Toast } from '../src/components/Toast'
import { HostList } from './HostList'
import { HostDetail } from './HostDetail'
import { Dialog, Notice, ScopeNote, Status } from './UI'
import { hosts, initialFilters, parseRoute, routeHash, type Filters, type Host, type Route, type Scenario } from './model'

type Modal = { kind: 'switch' } | { kind: 'terminal'; host: Host } | { kind: 'scope'; label: string } | { kind: 'discard'; next: Route }
type ChatMessage = { role: 'user' | 'assistant'; content: string }
const navigation = [
  { id: 'hosts', label: '主机', icon: Server }, { id: 'services', label: '应用服务', icon: Blocks },
  { id: 'overview', label: '概览', icon: LayoutDashboard }, { id: 'k8s', label: 'Kubernetes', icon: Blocks },
  { id: 'alerts', label: '告警', icon: TriangleAlert }, { id: 'uptime', label: '服务拨测', icon: Activity },
  { id: 'tasks', label: '任务中心', icon: ClipboardList }, { id: 'audit', label: '审计日志', icon: ShieldCheck }
]
const defaultFile = (id: string) => `# ${id}\n# 本地样板草稿，不写入真实文件\nmaintenance_window = "02:00–03:00"\n`
const safeTheme = () => {
  const param = new URLSearchParams(location.search).get('theme')
  if (param === 'light' || param === 'dark') return param
  try { return localStorage.getItem('mizupanel-prototype-theme') === 'dark' ? 'dark' : 'light' } catch { return 'light' }
}

function HostPicker({ current, onPick }: { current: string; onPick: (host: Host) => void }) {
  const [query, setQuery] = useState('')
  const matches = hosts.filter(h => `${h.name} ${h.address}`.toLowerCase().includes(query.toLowerCase()))
  return <div className="host-picker"><label className="search-field"><Search /><input data-autofocus aria-label="查找主机" placeholder="输入名称或 IP 地址" value={query} onChange={e => setQuery(e.target.value)} /></label>
    <div className="picker-results">{matches.map(host => <button key={host.id} onClick={() => onPick(host)} aria-label={`切换到 ${host.name}`}><span><strong>{host.name}</strong><small>{host.address}</small></span><Status status={host.status} />{host.id === current && <Check size={16} />}</button>)}{!matches.length && <p>没有匹配的主机</p>}</div>
  </div>
}

function ChatBody({ context, draft, onDraft, messages, onSend, onNew }: { context: string; draft: string; onDraft: (value: string) => void; messages: ChatMessage[]; onSend: () => void; onNew: () => void }) {
  return <div className="chat-body">
    <div className="chat-context"><span>上下文：{context}</span><button className="text-button" onClick={onNew}>新建对话</button></div>
    <div className="chat-messages" aria-live="polite">
      {!messages.length ? <div className="chat-empty"><Bot size={28} /><h3>从当前主机开始排查</h3><p>可以描述问题，或选择一个只读诊断方向。</p><button onClick={() => onDraft(`检查 ${context} 的资源使用情况，不执行变更。`)}>检查资源使用<ArrowUpRight size={14} /></button><button onClick={() => onDraft(`分析 ${context} 的进程快照，不执行变更。`)}>分析进程快照<ArrowUpRight size={14} /></button></div> : messages.map((message, index) => <div key={index} className={`chat-message ${message.role}`}><span>{message.role === 'user' ? '你' : '本地样板'}</span><p>{message.content}</p></div>)}
    </div>
    <form className="chat-composer" onSubmit={event => { event.preventDefault(); onSend() }}>
      <label className="sr-only" htmlFor="prototype-composer">发送给 AI 助手</label>
      <textarea id="prototype-composer" placeholder="描述需要排查的问题…" value={draft} onChange={event => onDraft(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); onSend() } }} />
      <div className="composer-footer"><span>不调用模型 · 不执行任何操作</span><button className="icon-button primary" type="submit" aria-label="预览发送" disabled={!draft.trim()}><Send /></button></div>
    </form>
    <p className="chat-safety">正式接入后，所有变更仍需在对话中确认。</p>
  </div>
}

export function PrototypeApp() {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash))
  const [theme, setTheme] = useState<'light' | 'dark'>(safeTheme)
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [selected, setSelected] = useState(new Set<string>())
  const [scrollTop, setScrollTop] = useState(0)
  const [scenario, setScenario] = useState<Scenario>('ready')
  const [refreshedAt, setRefreshedAt] = useState('14:30:00')
  const [refreshing, setRefreshing] = useState(false)
  const [modal, setModal] = useState<Modal | null>(null)
  const [aiOpen, setAiOpen] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const [draft, setDraft] = useState('')
  const [chatContext, setChatContext] = useState('主机列表')
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([])
  const [fileDraft, setFileDraft] = useState(() => defaultFile(route.page === 'host' ? route.id : ''))
  const [savedFile, setSavedFile] = useState(fileDraft)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const current = route.page === 'host' ? hosts.find(host => host.id === route.id) : undefined
  const dirty = route.page === 'host' && fileDraft !== savedFile
  const routeRef = useRef(route)
  const historyIndex = useRef(0)
  const restoringHistory = useRef(false)
  useEffect(() => {
    history.replaceState({ prototypeIndex: 0 }, '', location.href)
  }, [])
  const pushRoute = (next: Route) => {
    if (location.hash === routeHash(next)) return
    historyIndex.current += 1
    history.pushState({ prototypeIndex: historyIndex.current }, '', routeHash(next))
  }
  const applyRoute = useCallback((next: Route) => {
    const previous = routeRef.current
    if (previous.page !== 'host' || next.page !== 'host' || previous.id !== next.id) {
      const clean = defaultFile(next.page === 'host' ? next.id : '')
      setFileDraft(clean); setSavedFile(clean)
    }
    routeRef.current = next
    setRoute(next)
    clearTimeout(refreshTimer.current)
    setSelected(new Set()); setScenario('ready'); setRefreshing(false); setNavOpen(false)
  }, [])
  const navigate = (next: Route) => {
    if (dirty && (next.page !== 'host' || next.id !== (route.page === 'host' ? route.id : '') || next.tab !== 'files')) { setModal({ kind: 'discard', next }); return }
    pushRoute(next)
    applyRoute(next)
  }
  useEffect(() => {
    const onHistory = (event: PopStateEvent) => {
      if (restoringHistory.current) { restoringHistory.current = false; return }
      const next = parseRoute(location.hash)
      const index = typeof event.state?.prototypeIndex === 'number' ? event.state.prototypeIndex : historyIndex.current - 1
      if (dirty) {
        const delta = historyIndex.current - index
        if (delta) { restoringHistory.current = true; history.go(delta) }
        else history.replaceState({ prototypeIndex: historyIndex.current }, '', routeHash(route))
        setModal({ kind: 'discard', next })
        return
      }
      historyIndex.current = index
      applyRoute(next)
    }
    window.addEventListener('popstate', onHistory)
    return () => window.removeEventListener('popstate', onHistory)
  }, [dirty, route, applyRoute])
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try { localStorage.setItem('mizupanel-prototype-theme', theme) } catch { /* storage may be unavailable */ }
  }, [theme])
  useEffect(() => () => clearTimeout(refreshTimer.current), [])
  const scope = (label: string) => setModal({ kind: 'scope', label })
  const refresh = () => {
    clearTimeout(refreshTimer.current)
    setRefreshing(true)
    if (scenario !== 'ready') setScenario('loading')
    refreshTimer.current = setTimeout(() => { setRefreshing(false); setScenario('ready'); setRefreshedAt(new Date().toLocaleTimeString('zh-CN', { hour12: false })); setToast({ type: 'success', message: '模拟快照刷新成功' }) }, 450)
  }
  const changeScenario = (value: Scenario) => { clearTimeout(refreshTimer.current); setRefreshing(false); setScenario(value); setSelected(new Set()) }
  const openAI = () => {
    if (!draft && !chatMessages.length) setChatContext(current?.name ?? '主机列表')
    setAiOpen(true)
  }
  const send = () => {
    if (!draft.trim()) return
    setChatMessages(previous => [...previous, { role: 'user', content: draft.trim() }, { role: 'assistant', content: `已在本地预览你的问题，上下文为 ${chatContext}。这是交互样板，未调用模型，也未执行命令。正式接入后将在这里展示基于实际数据的分析与待确认操作。` }])
    setDraft('')
  }
  const newChat = () => { setChatContext(current?.name ?? '主机列表'); setDraft(''); setChatMessages([]) }
  const sectionName = route.page === 'placeholder' ? navigation.find(item => item.id === route.name)?.label ?? '系统设置' : route.page === 'ai' ? 'AI 运维助手' : '主机'
  const main = route.page === 'hosts' ? <HostList rows={hosts} filters={filters} onFilter={setFilters} selected={selected} onSelect={setSelected} onOpen={host => navigate({ page: 'host', id: host.id, tab: 'overview' })} onTerminal={host => setModal({ kind: 'terminal', host })} onAction={scope} scenario={scenario} onScenario={changeScenario} scrollTop={scrollTop} onScroll={setScrollTop} refreshedAt={refreshedAt} refreshing={refreshing} onRefresh={refresh} />
    : route.page === 'host' && current ? <HostDetail host={current} tab={route.tab} onTab={tab => navigate({ ...route, tab })} onBack={() => navigate({ page: 'hosts' })} onSwitch={() => setModal({ kind: 'switch' })} onTerminal={() => setModal({ kind: 'terminal', host: current })} onAction={scope} scenario={scenario} onScenario={changeScenario} fileDraft={fileDraft} savedFile={savedFile} onFileChange={setFileDraft} onFileSave={() => { setSavedFile(fileDraft); setToast({ type: 'success', message: '样板草稿保存成功（仅当前页面）' }) }} refreshing={refreshing} onRefresh={refresh} />
    : route.page === 'ai' ? <section className="page ai-workspace"><div className="page-title"><h1>AI 运维助手</h1><button className="button" onClick={() => navigate({ page: 'hosts' })}><ArrowLeft />返回主机</button></div><ChatBody context={chatContext} draft={draft} onDraft={setDraft} messages={chatMessages} onSend={send} onNew={newChat} /></section>
    : <section className="page"><div className="page-title"><h1>{route.page === 'host' ? '未找到主机' : sectionName}</h1></div><div className="empty-state"><h2>{route.page === 'host' ? '样板中没有这台主机' : '此页面不在本轮样板范围内'}</h2><p>本轮体验主机列表、详情概览、进程与 AI 抽屉。现有功能保持不变。</p><button className="button" onClick={() => navigate({ page: 'hosts' })}>返回主机列表</button></div></section>

  return <>
    <div className="app-shell" inert={!!modal || aiOpen}>
      <aside className={`sidebar ${navOpen ? 'nav-open' : ''}`}>
        <a className="brand" href="#/hosts" onClick={event => { event.preventDefault(); navigate({ page: 'hosts' }) }}><BrandLogo className="h-7 w-7" /><span>MizuPanel</span></a>
        <nav aria-label="全局导航">{navigation.map((item, index) => <button key={item.id} className={`nav-item ${index === 4 || index === 6 ? 'nav-gap' : ''} ${((route.page === 'host' || route.page === 'hosts') && item.id === 'hosts') || (route.page === 'placeholder' && route.name === item.id) ? 'active' : ''}`} onClick={() => navigate(item.id === 'hosts' ? { page: 'hosts' } : { page: 'placeholder', name: item.id })}><item.icon size={17} /><span>{item.label}</span></button>)}</nav>
        <div className="sidebar-bottom"><button className={`nav-item ${route.page === 'placeholder' && route.name === 'settings' ? 'active' : ''}`} onClick={() => navigate({ page: 'placeholder', name: 'settings' })}><Settings size={17} />系统设置</button><div className="prototype-mark"><span className="sample-dot" />设计样板<span>仅本地</span></div></div>
      </aside>
      <div className="workspace">
        <header className="topbar"><div className="flex items-center gap-3"><button className="icon-button mobile-menu" onClick={() => setNavOpen(!navOpen)} aria-label="切换导航" aria-expanded={navOpen}>{navOpen ? <X /> : <Menu />}</button><span className="workspace-name">本地工作区</span><ChevronRight size={13} className="muted" /><span className="sample-label">模拟数据</span></div>
          <div className="flex items-center gap-2">{route.page !== 'ai' && <button className="button ghost ai-trigger" onClick={openAI}><Bot />AI 助手</button>}<span className="toolbar-divider" /><button className="icon-button" aria-label={theme === 'light' ? '切换到深色主题' : '切换到浅色主题'} onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}>{theme === 'light' ? <Moon /> : <Sun />}</button></div>
        </header>
        <main className="work-sheet">{main}<footer className="sheet-footer"><ScopeNote /><label className="scenario-control">演示状态<select aria-label="演示数据状态" value={scenario} onChange={event => changeScenario(event.target.value as Scenario)}><option value="ready">正常</option><option value="loading">加载中</option><option value="empty">空数据</option><option value="error">请求失败</option></select></label></footer></main>
      </div>
    </div>
    {modal && <Dialog key={modal.kind} title={modal.kind === 'switch' ? '切换主机' : modal.kind === 'terminal' ? `终端 · ${modal.host.name}` : modal.kind === 'discard' ? '有未保存的修改' : modal.label} className={modal.kind === 'terminal' ? 'terminal-dialog' : ''} onClose={() => setModal(null)}>
      {modal.kind === 'switch' ? <HostPicker current={current?.id ?? ''} onPick={host => { setModal(null); navigate({ page: 'host', id: host.id, tab: route.page === 'host' ? route.tab : 'overview' }) }} />
        : modal.kind === 'terminal' ? <div className="modal-content"><Notice>只读终端预览，未建立 SSH 或 WebSocket 连接。</Notice><pre className="terminal-preview">{`${modal.host.name} · ${modal.host.address}\n\n$ hostname\n${modal.host.name}\n\n$ uname -m\nx86_64\n\n# 真实连接功能将在正式接入后提供。`}</pre></div>
          : modal.kind === 'discard' ? <div className="modal-content"><p>切换工作位置会丢弃当前样板草稿的未保存修改，不会影响任何真实文件。</p><div className="modal-actions"><button className="button" onClick={() => setModal(null)}>继续编辑</button><button className="button primary" onClick={() => { const next = modal.next; setModal(null); const clean = defaultFile(next.page === 'host' ? next.id : ''); setFileDraft(clean); setSavedFile(clean); pushRoute(next); applyRoute(next) }}>丢弃并继续</button></div></div>
            : <div className="modal-content"><p>“{modal.label}”保留操作入口，本轮不连接后台、不执行真实操作。</p><p className="muted">先确认主机工作区的布局与交互，再接入现有功能。</p><div className="modal-actions"><button className="button" onClick={() => setModal(null)}>返回样板</button></div></div>}
    </Dialog>}
    {aiOpen && <Dialog title="AI 运维助手" className="ai-drawer" onClose={() => setAiOpen(false)}><div className="drawer-workspace"><button className="text-button" onClick={() => { setAiOpen(false); navigate({ page: 'ai' }) }}>打开完整工作台<ArrowUpRight size={14} /></button></div><ChatBody context={chatContext} draft={draft} onDraft={setDraft} messages={chatMessages} onSend={send} onNew={newChat} /></Dialog>}
    {toast && <Toast message={toast.message} type={toast.type} onClose={() => setToast(null)} />}
  </>
}
