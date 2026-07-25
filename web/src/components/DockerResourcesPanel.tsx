import { type ReactNode, useMemo, useState } from 'react'
import { Download, LoaderCircle, RotateCw, Trash2, X } from 'lucide-react'

import type { DockerImage, DockerNetwork, DockerResourceAction, DockerResourceListResponse, DockerResourceType, DockerVolume } from '../types'
import { formatBytes } from '../lib/format'

type ResourceView = 'images' | 'volumes' | 'networks'
type UsageFilter = 'all' | 'used' | 'unused'

type PendingDelete = {
  resourceType: DockerResourceType
  resourceID: string
  label: string
}

type DockerResourcesPanelProps = {
  response?: DockerResourceListResponse
  loading: boolean
  online: boolean
  onRefresh: () => Promise<void> | void
  onAction: (resourceType: DockerResourceType, resourceID: string, action: DockerResourceAction) => Promise<{ success: boolean; error?: string }>
  onShowToast: (message: string, type: 'success' | 'error') => void
}

export function DockerResourcesPanel({ response, loading, online, onRefresh, onAction, onShowToast }: DockerResourcesPanelProps) {
  const [view, setView] = useState<ResourceView>('images')
  const [usageFilter, setUsageFilter] = useState<UsageFilter>('all')
  const [search, setSearch] = useState('')
  const [imageReference, setImageReference] = useState('')
  const [actionLoading, setActionLoading] = useState<string>()
  const [pendingDelete, setPendingDelete] = useState<PendingDelete>()

  const images = response?.images ?? []
  const volumes = response?.volumes ?? []
  const networks = response?.networks ?? []
  const keyword = search.trim().toLowerCase()

  const filteredImages = useMemo(() => images.filter((image) => {
    if (!matchesUsageFilter(image.containers, usageFilter)) return false
    if (!keyword) return true
    return [image.id, image.full_id ?? '', ...image.tags].some((value) => value.toLowerCase().includes(keyword))
  }), [images, keyword, usageFilter])
  const filteredVolumes = useMemo(() => volumes.filter((volume) => {
    if (!matchesUsageFilter(volume.ref_count, usageFilter)) return false
    if (!keyword) return true
    return [volume.name, volume.driver ?? '', volume.scope ?? '', volume.mountpoint ?? '', volume.compose_project ?? ''].some((value) => value.toLowerCase().includes(keyword))
  }), [keyword, usageFilter, volumes])
  const filteredNetworks = useMemo(() => networks.filter((network) => {
    if (!matchesUsageFilter(network.containers.length, usageFilter)) return false
    if (!keyword) return true
    return [network.name, network.id, network.full_id ?? '', network.driver ?? '', network.scope ?? '', ...network.subnets, ...network.containers].some((value) => value.toLowerCase().includes(keyword))
  }), [keyword, networks, usageFilter])

  const runAction = async (resourceType: DockerResourceType, resourceID: string, action: DockerResourceAction) => {
    const key = `${resourceType}:${resourceID}:${action}`
    const operation = resourceActionText(resourceType, action)
    setActionLoading(key)
    try {
      await onAction(resourceType, resourceID, action)
      onShowToast(`${operation}成功`, 'success')
      if (action === 'pull') setImageReference('')
      setPendingDelete(undefined)
      await onRefresh()
    } catch (error) {
      const reason = error instanceof Error ? error.message : '未知错误'
      onShowToast(`${operation}失败: ${reason}`, 'error')
    } finally {
      setActionLoading(undefined)
    }
  }

  const pullImage = () => {
    const reference = imageReference.trim()
    if (!reference) {
      onShowToast('镜像拉取失败: 请输入镜像引用', 'error')
      return
    }
    void runAction('image', reference, 'pull')
  }

  const refresh = () => {
    if (loading || actionLoading) return
    void onRefresh()
  }

  if (loading && !response) {
    return <PanelState icon={<LoaderCircle size={18} className="animate-spin" />} title="正在读取 Docker 资源" detail="正在汇总磁盘占用、镜像、数据卷和网络。" />
  }
  if (response && !response.supported) {
    return <PanelState title="当前 Agent 不支持资源管理" detail={response.error || '请将 Agent 升级到最新版本后重试。'} tone="warning" />
  }
  if (response?.error && !response.success) {
    return <PanelState title="Docker 资源读取失败" detail={response.error} tone="danger" action={<button type="button" onClick={refresh} disabled={!online || loading} className="soft-button min-h-9 border border-border bg-card px-3 text-xs font-black text-foreground disabled:opacity-60">重新加载</button>} />
  }

  const currentCount = view === 'images' ? filteredImages.length : view === 'volumes' ? filteredVolumes.length : filteredNetworks.length
  const totalCount = view === 'images' ? images.length : view === 'volumes' ? volumes.length : networks.length

  return (
    <div className="min-w-0">
      <div aria-label="Docker 磁盘占用" className="grid grid-cols-4 divide-x divide-border border-b border-border bg-surface/70">
        <UsageItem label="镜像层" value={response?.usage.image_layers} />
        <UsageItem label="容器写层" value={response?.usage.container_writable} />
        <UsageItem label="数据卷" value={response?.usage.volumes} />
        <UsageItem label="构建缓存" value={response?.usage.build_cache} />
      </div>

      <div className="border-b border-border bg-surface/40 px-4 py-3">
        <div className="flex min-w-0 items-center justify-between gap-3">
          <div className="flex shrink-0 rounded-2xl border border-border bg-card p-1 shadow-inner" role="tablist" aria-label="Docker 资源类型">
            {([
              ['images', `镜像 ${images.length}`],
              ['volumes', `数据卷 ${volumes.length}`],
              ['networks', `网络 ${networks.length}`]
            ] as const).map(([value, label]) => (
              <button key={value} type="button" role="tab" aria-selected={view === value} onClick={() => setView(value)} className={`min-h-9 whitespace-nowrap rounded-xl px-3 text-xs font-black transition ${view === value ? 'bg-slate-950 text-white' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>{label}</button>
            ))}
          </div>
          <button type="button" aria-label="刷新 Docker 资源" title="刷新" onClick={refresh} disabled={!online || loading || Boolean(actionLoading)} className="soft-button inline-flex h-10 w-10 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"><RotateCw size={16} className={loading ? 'animate-spin' : ''} aria-hidden="true" /></button>
        </div>
        <div className="mt-3 flex min-w-0 items-center gap-2 border-t border-border/70 pt-3">
          <div className="flex shrink-0 rounded-2xl border border-border bg-card p-1 shadow-inner" aria-label="资源使用状态筛选">
            {([['all', '全部'], ['used', '使用中'], ['unused', '未使用']] as const).map(([value, label]) => (
              <button key={value} type="button" aria-pressed={usageFilter === value} onClick={() => setUsageFilter(value)} className={`min-h-9 whitespace-nowrap rounded-xl px-3 text-xs font-black transition ${usageFilter === value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>{label}</button>
            ))}
          </div>
          <input aria-label="搜索 Docker 资源" value={search} onChange={(event) => setSearch(event.target.value)} placeholder={resourceSearchPlaceholder(view)} className="soft-input min-h-10 min-w-0 flex-1 px-3 text-sm font-semibold" />
        </div>

        {view === 'images' ? (
          <div className="mt-3 flex items-center gap-2 border-t border-border/70 pt-3">
            <div className="min-w-0 flex-1">
              <label htmlFor="docker-image-reference" className="sr-only">镜像引用</label>
              <input id="docker-image-reference" value={imageReference} onChange={(event) => setImageReference(event.target.value)} onKeyDown={(event) => event.key === 'Enter' && pullImage()} placeholder="输入镜像引用，例如 nginx:latest" disabled={!online || Boolean(actionLoading)} className="soft-input min-h-10 w-full px-3 font-mono text-sm font-semibold" />
            </div>
            <button type="button" onClick={pullImage} disabled={!online || Boolean(actionLoading) || !imageReference.trim()} className="soft-button inline-flex min-h-10 shrink-0 items-center gap-2 bg-primary px-4 text-xs font-black text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50">
              {actionLoading?.endsWith(':pull') ? <LoaderCircle size={15} className="animate-spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
              拉取镜像
            </button>
          </div>
        ) : null}
      </div>

      <div className="flex items-center justify-between border-b border-border px-4 py-2 text-[11px] font-black uppercase tracking-[0.16em] text-muted-foreground">
        <span>{resourceViewLabel(view)}</span>
        <span>{currentCount === totalCount ? `${totalCount} 项` : `${currentCount} / ${totalCount} 项`}</span>
      </div>

      {currentCount === 0 ? (
        <div className="px-4 py-10 text-center">
          <p className="text-sm font-black text-foreground">{totalCount === 0 ? `暂无${resourceViewLabel(view)}` : '没有符合条件的资源'}</p>
          <p className="mt-1 text-xs font-semibold text-muted-foreground">{totalCount === 0 ? 'Docker Engine 当前没有返回此类资源。' : '请调整使用状态筛选或搜索关键词。'}</p>
        </div>
      ) : view === 'images' ? (
        <ImageTable images={filteredImages} busy={Boolean(actionLoading)} onDelete={(image) => setPendingDelete({ resourceType: 'image', resourceID: image.full_id || image.id, label: image.tags[0] || image.id })} />
      ) : view === 'volumes' ? (
        <VolumeTable volumes={filteredVolumes} busy={Boolean(actionLoading)} onDelete={(volume) => setPendingDelete({ resourceType: 'volume', resourceID: volume.name, label: volume.name })} />
      ) : (
        <NetworkTable networks={filteredNetworks} busy={Boolean(actionLoading)} onDelete={(network) => setPendingDelete({ resourceType: 'network', resourceID: network.full_id || network.id, label: network.name })} />
      )}

      {pendingDelete ? (
        <div className="fixed inset-0 z-[85] flex items-center justify-center bg-slate-950/35 p-4" onClick={() => !actionLoading && setPendingDelete(undefined)}>
          <section role="dialog" aria-modal="true" aria-label={`删除${resourceTypeLabel(pendingDelete.resourceType)}`} className="w-full max-w-md overflow-hidden rounded-[28px] border border-danger/25 bg-card shadow-2xl" onClick={(event) => event.stopPropagation()}>
            <div className="flex items-start justify-between gap-3 border-b border-danger/20 bg-danger/10 px-5 py-4">
              <div className="min-w-0">
                <p className="text-[11px] font-black uppercase tracking-[0.2em] text-danger">Destructive action</p>
                <h3 className="mt-1 text-lg font-black text-foreground">删除{resourceTypeLabel(pendingDelete.resourceType)}</h3>
              </div>
              <button type="button" aria-label="关闭删除确认" onClick={() => setPendingDelete(undefined)} disabled={Boolean(actionLoading)} className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground disabled:opacity-50"><X size={16} aria-hidden="true" /></button>
            </div>
            <div className="space-y-3 px-5 py-5">
              <p className="text-sm font-bold leading-6 text-foreground">确认删除 <span className="break-all font-mono font-black">{pendingDelete.label}</span>？</p>
              <p className={`rounded-2xl border px-4 py-3 text-xs font-bold leading-5 ${pendingDelete.resourceType === 'volume' ? 'border-danger/30 bg-danger/10 text-danger' : 'border-warning/30 bg-warning/10 text-warning'}`}>
                {pendingDelete.resourceType === 'volume' ? '数据卷中的数据会被永久删除且无法恢复。MizuPanel 只允许删除当前未被容器引用的数据卷。' : pendingDelete.resourceType === 'network' ? '仅未连接容器且不属于 Docker 系统网络的网络可以删除。' : '仅当前未被任何容器引用的镜像可以删除。'}
              </p>
            </div>
            <div className="flex justify-end gap-2 border-t border-border bg-surface px-5 py-4">
              <button type="button" onClick={() => setPendingDelete(undefined)} disabled={Boolean(actionLoading)} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black text-muted-foreground disabled:opacity-50">取消</button>
              <button type="button" onClick={() => void runAction(pendingDelete.resourceType, pendingDelete.resourceID, 'remove')} disabled={Boolean(actionLoading)} className="soft-button inline-flex min-h-10 items-center gap-2 bg-danger px-4 text-xs font-black text-white disabled:opacity-50">{actionLoading ? <LoaderCircle size={15} className="animate-spin" aria-hidden="true" /> : null}确认删除</button>
            </div>
          </section>
        </div>
      ) : null}
    </div>
  )
}

function UsageItem({ label, value }: { label: string; value?: number }) {
  return <div className="min-w-0 px-4 py-3"><p className="truncate text-[10px] font-black uppercase tracking-[0.16em] text-muted-foreground">{label}</p><p className="mt-1 truncate font-mono text-sm font-black text-foreground" title={value === undefined ? 'Docker Engine 未返回占用数据' : formatBytes(value)}>{value === undefined ? '—' : formatBytes(value)}</p></div>
}

function ImageTable({ images, busy, onDelete }: { images: DockerImage[]; busy: boolean; onDelete: (image: DockerImage) => void }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] table-fixed text-left">
        <colgroup><col className="w-[30%]" /><col className="w-[14%]" /><col className="w-[12%]" /><col className="w-[12%]" /><col className="w-[10%]" /><col className="w-[16%]" /><col className="w-[6%]" /></colgroup>
        <thead className="bg-surface text-[10px] font-black uppercase tracking-[0.15em] text-muted-foreground"><tr><Th>镜像</Th><Th>ID</Th><Th>大小</Th><Th>共享层</Th><Th>容器</Th><Th>创建时间</Th><Th align="right">操作</Th></tr></thead>
        <tbody className="divide-y divide-border">
          {images.map((image) => {
            const removable = image.containers === 0
            const primaryTag = image.tags[0] || '未标记镜像'
            return <tr key={image.full_id || image.id} className="bg-card hover:bg-surface/60"><Td><p className="truncate font-mono text-xs font-black text-foreground" title={primaryTag}>{primaryTag}</p>{image.tags.length > 1 ? <p className="mt-1 text-[11px] font-bold text-muted-foreground">另有 {image.tags.length - 1} 个标签</p> : null}</Td><Td mono title={image.full_id}>{image.id}</Td><Td>{formatOptionalBytes(image.size)}</Td><Td>{formatOptionalBytes(image.shared_size)}</Td><Td><UsageCount value={image.containers} suffix="个" /></Td><Td>{formatResourceTime(image.created_at)}</Td><Td align="right"><DeleteButton label={`删除镜像 ${primaryTag}`} disabled={busy || !removable} title={removable ? '删除未使用镜像' : image.containers === undefined ? '无法确认镜像引用状态' : '镜像仍被容器引用'} onClick={() => onDelete(image)} /></Td></tr>
          })}
        </tbody>
      </table>
    </div>
  )
}

function VolumeTable({ volumes, busy, onDelete }: { volumes: DockerVolume[]; busy: boolean; onDelete: (volume: DockerVolume) => void }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] table-fixed text-left">
        <colgroup><col className="w-[35%]" /><col className="w-[14%]" /><col className="w-[12%]" /><col className="w-[12%]" /><col className="w-[21%]" /><col className="w-[6%]" /></colgroup>
        <thead className="bg-surface text-[10px] font-black uppercase tracking-[0.15em] text-muted-foreground"><tr><Th>数据卷</Th><Th>驱动 / Scope</Th><Th>大小</Th><Th>引用</Th><Th>Compose 项目</Th><Th align="right">操作</Th></tr></thead>
        <tbody className="divide-y divide-border">
          {volumes.map((volume) => {
            const removable = volume.ref_count === 0
            return <tr key={volume.name} className="bg-card hover:bg-surface/60"><Td><p className="truncate font-mono text-xs font-black text-foreground" title={volume.name}>{volume.name}</p><p className="mt-1 truncate text-[11px] font-semibold text-muted-foreground" title={volume.mountpoint}>{volume.mountpoint || '挂载点未知'}</p></Td><Td><p className="truncate text-xs font-black text-foreground">{volume.driver || '—'}</p><p className="mt-1 text-[11px] font-bold text-muted-foreground">{volume.scope || '—'}</p></Td><Td>{formatOptionalBytes(volume.size)}</Td><Td><UsageCount value={volume.ref_count} suffix="个" /></Td><Td mono title={volume.compose_project}>{volume.compose_project || '—'}</Td><Td align="right"><DeleteButton label={`删除数据卷 ${volume.name}`} disabled={busy || !removable} title={removable ? '删除未引用数据卷' : volume.ref_count === undefined ? '无法确认数据卷引用状态' : '数据卷仍被容器引用'} onClick={() => onDelete(volume)} /></Td></tr>
          })}
        </tbody>
      </table>
    </div>
  )
}

function NetworkTable({ networks, busy, onDelete }: { networks: DockerNetwork[]; busy: boolean; onDelete: (network: DockerNetwork) => void }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] table-fixed text-left">
        <colgroup><col className="w-[29%]" /><col className="w-[14%]" /><col className="w-[21%]" /><col className="w-[15%]" /><col className="w-[15%]" /><col className="w-[6%]" /></colgroup>
        <thead className="bg-surface text-[10px] font-black uppercase tracking-[0.15em] text-muted-foreground"><tr><Th>网络</Th><Th>驱动 / Scope</Th><Th>子网</Th><Th>连接容器</Th><Th>保护状态</Th><Th align="right">操作</Th></tr></thead>
        <tbody className="divide-y divide-border">
          {networks.map((network) => {
            const removable = !network.protected && network.containers.length === 0
            return <tr key={network.full_id || network.id} className="bg-card hover:bg-surface/60"><Td><p className="truncate font-mono text-xs font-black text-foreground" title={network.name}>{network.name}</p><p className="mt-1 truncate font-mono text-[11px] font-semibold text-muted-foreground" title={network.full_id}>{network.id}</p></Td><Td><p className="truncate text-xs font-black text-foreground">{network.driver || '—'}</p><p className="mt-1 text-[11px] font-bold text-muted-foreground">{network.scope || '—'}</p></Td><Td mono title={network.subnets.join(', ')}>{network.subnets.join(', ') || '—'}</Td><Td><UsageCount value={network.containers.length} suffix="个" /></Td><Td>{network.protected ? <span className="inline-flex rounded-full bg-warning/10 px-2 py-1 text-[11px] font-black text-warning">系统保护</span> : <span className="text-xs font-bold text-muted-foreground">普通网络</span>}</Td><Td align="right"><DeleteButton label={`删除网络 ${network.name}`} disabled={busy || !removable} title={network.protected ? 'Docker 系统网络不可删除' : network.containers.length > 0 ? '网络仍有容器连接' : '删除未连接网络'} onClick={() => onDelete(network)} /></Td></tr>
          })}
        </tbody>
      </table>
    </div>
  )
}

function Th({ children, align = 'left' }: { children: ReactNode; align?: 'left' | 'right' }) {
  return <th className={`px-3 py-2.5 ${align === 'right' ? 'text-right' : ''}`}>{children}</th>
}

function Td({ children, mono = false, title, align = 'left' }: { children: ReactNode; mono?: boolean; title?: string; align?: 'left' | 'right' }) {
  return <td className={`min-w-0 px-3 py-3 text-xs font-bold text-muted-foreground ${mono ? 'truncate font-mono' : ''} ${align === 'right' ? 'text-right' : ''}`} title={title}>{children}</td>
}

function UsageCount({ value, suffix }: { value?: number; suffix: string }) {
  if (value === undefined) return <span title="Docker Engine 未返回引用数量">—</span>
  return <span className={value > 0 ? 'font-black text-foreground' : 'text-muted-foreground'}>{value} {suffix}</span>
}

function DeleteButton({ label, disabled, title, onClick }: { label: string; disabled: boolean; title: string; onClick: () => void }) {
  return <button type="button" aria-label={label} title={title} onClick={onClick} disabled={disabled} className="soft-button inline-flex h-8 w-8 items-center justify-center border border-border bg-card text-muted-foreground hover:border-danger/30 hover:bg-danger/10 hover:text-danger disabled:cursor-not-allowed disabled:opacity-35"><Trash2 size={14} aria-hidden="true" /></button>
}

function PanelState({ icon, title, detail, tone = 'default', action }: { icon?: ReactNode; title: string; detail: string; tone?: 'default' | 'warning' | 'danger'; action?: ReactNode }) {
  const toneClass = tone === 'danger' ? 'border-danger/30 bg-danger/10 text-danger' : tone === 'warning' ? 'border-warning/30 bg-warning/10 text-warning' : 'border-border bg-surface text-muted-foreground'
  return <div className={`m-4 flex items-center gap-3 rounded-2xl border px-4 py-4 ${toneClass}`}>{icon ? <span className="shrink-0">{icon}</span> : null}<div className="min-w-0 flex-1"><p className="text-sm font-black text-foreground">{title}</p><p className="mt-1 text-xs font-bold leading-5">{detail}</p></div>{action}</div>
}

function matchesUsageFilter(count: number | undefined, filter: UsageFilter) {
  if (filter === 'all') return true
  if (count === undefined) return false
  return filter === 'used' ? count > 0 : count === 0
}

function formatOptionalBytes(value?: number) {
  return value === undefined ? '—' : formatBytes(value)
}

function formatResourceTime(value?: number) {
  if (!value) return '—'
  return new Date(value * 1000).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function resourceViewLabel(view: ResourceView) {
  return view === 'images' ? '镜像' : view === 'volumes' ? '数据卷' : '网络'
}

function resourceSearchPlaceholder(view: ResourceView) {
  return view === 'images' ? '搜索标签或 ID' : view === 'volumes' ? '搜索卷名、挂载点或项目' : '搜索网络名、ID 或子网'
}

function resourceTypeLabel(type: DockerResourceType) {
  return type === 'image' ? '镜像' : type === 'volume' ? '数据卷' : '网络'
}

function resourceActionText(type: DockerResourceType, action: DockerResourceAction) {
  return `${resourceTypeLabel(type)}${action === 'pull' ? '拉取' : '删除'}`
}
