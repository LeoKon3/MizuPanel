import { type ReactNode, useEffect, useMemo, useState } from 'react'
import { Check, FolderCog, LayoutList, Loader2, Pencil, Plus, Tags, Trash2, X } from 'lucide-react'

import { createNodeGroup, createNodeTag, deleteNodeGroup, deleteNodeTag, getNodeGroups, getNodeTags, updateNodeGroup, updateNodeTag } from '../api/client'
import type { NodeGroup, NodeGroupSummary, NodeTag, NodeTagSummary } from '../types'
import { Toast } from './Toast'

export type HostViewMode = 'browse' | 'batch'
export type HostGroupFilter = 'all' | 'ungrouped' | string
export type HostTagFilter = 'all' | string

type NodeOrganizationControlsProps = {
  view: HostViewMode
  onViewChange: (view: HostViewMode) => void
  groupFilter: HostGroupFilter
  onGroupFilterChange: (groupID: HostGroupFilter) => void
  tagFilter: HostTagFilter
  onTagFilterChange: (tagID: HostTagFilter) => void
  groups: NodeGroupSummary[]
  tags: NodeTagSummary[]
  onChanged: () => Promise<void> | void
}

export function NodeOrganizationControls({ view, onViewChange, groupFilter, onGroupFilterChange, tagFilter, onTagFilterChange, groups, tags, onChanged }: NodeOrganizationControlsProps) {
  const [catalogOpen, setCatalogOpen] = useState(false)
  const orderedGroups = useMemo(() => [...groups].sort((left, right) => left.name.localeCompare(right.name, 'zh-CN')), [groups])
  const orderedTags = useMemo(() => [...tags].sort((left, right) => left.name.localeCompare(right.name, 'zh-CN')), [tags])

  return (
    <>
      <section className="flex flex-col gap-3 border-y border-border/80 bg-surface/55 px-3 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2">
          <div className="inline-flex rounded-2xl border border-border bg-card p-1" role="group" aria-label="主机视图">
            <ViewButton active={view === 'browse'} onClick={() => onViewChange('browse')} icon={<LayoutList size={14} />} label="浏览" />
            <ViewButton active={view === 'batch'} onClick={() => onViewChange('batch')} icon={<Check size={14} />} label="批量管理" />
          </div>
          <label className="sr-only" htmlFor="host-group-filter">筛选分组</label>
          <select id="host-group-filter" value={groupFilter} onChange={(event) => onGroupFilterChange(event.target.value)} className="soft-input min-h-10 min-w-36 px-3 text-xs font-black">
            <option value="all">全部分组</option>
            {orderedGroups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}
            <option value="ungrouped">未分组</option>
          </select>
          <label className="sr-only" htmlFor="host-tag-filter">筛选标签</label>
          <select id="host-tag-filter" value={tagFilter} onChange={(event) => onTagFilterChange(event.target.value)} className="soft-input min-h-10 min-w-36 px-3 text-xs font-black">
            <option value="all">全部标签</option>
            {orderedTags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
          </select>
        </div>
        <button type="button" onClick={() => setCatalogOpen(true)} className="soft-button inline-flex min-h-10 items-center justify-center gap-2 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-surface">
          <FolderCog size={15} aria-hidden="true" />管理分组与标签
        </button>
      </section>
      <OrganizationCatalogModal open={catalogOpen} onClose={() => setCatalogOpen(false)} onChanged={onChanged} />
    </>
  )
}

function ViewButton({ active, onClick, icon, label }: { active: boolean, onClick: () => void, icon: ReactNode, label: string }) {
  return (
    <button type="button" aria-pressed={active} onClick={onClick} className={`soft-button inline-flex min-h-8 items-center gap-1.5 px-3 text-xs font-black ${active ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-surface hover:text-foreground'}`}>
      {icon}{label}
    </button>
  )
}

type OrganizationCatalogModalProps = {
  open: boolean
  onClose: () => void
  onChanged: () => Promise<void> | void
}

type DeleteTarget = { kind: 'group' | 'tag', id: string, name: string, nodeCount: number }

const tagColors: NodeTag['color'][] = ['green', 'teal', 'blue', 'amber', 'red', 'gray']

function OrganizationCatalogModal({ open, onClose, onChanged }: OrganizationCatalogModalProps) {
  const [tab, setTab] = useState<'groups' | 'tags'>('groups')
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [tags, setTags] = useState<NodeTag[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createColor, setCreateColor] = useState<NodeTag['color']>('teal')
  const [editing, setEditing] = useState<{ kind: 'group' | 'tag', id: string, name: string, color: NodeTag['color'] }>()
  const [deleting, setDeleting] = useState<DeleteTarget>()
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' }>()

  const loadCatalog = async () => {
    setLoading(true)
    try {
      const [groupResponse, tagResponse] = await Promise.all([getNodeGroups(), getNodeTags()])
      setGroups(groupResponse.groups)
      setTags(tagResponse.tags)
    } catch (error) {
      setToast({ message: `分组标签加载失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!open) return
    void loadCatalog()
  }, [open])

  useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !saving && !deleting) onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [deleting, onClose, open, saving])

  if (!open) return null

  const create = async () => {
    if (!createName.trim()) return
    setSaving(true)
    try {
      if (tab === 'groups') {
        await createNodeGroup(createName)
        setToast({ message: '主机分组创建成功', type: 'success' })
      } else {
        await createNodeTag(createName, createColor)
        setToast({ message: '主机标签创建成功', type: 'success' })
      }
      setCreateName('')
      await loadCatalog()
      await onChanged()
    } catch (error) {
      setToast({ message: `${tab === 'groups' ? '主机分组' : '主机标签'}创建失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const saveEdit = async () => {
    if (!editing || !editing.name.trim()) return
    setSaving(true)
    try {
      if (editing.kind === 'group') {
        await updateNodeGroup(editing.id, editing.name)
        setToast({ message: '主机分组保存成功', type: 'success' })
      } else {
        await updateNodeTag(editing.id, editing.name, editing.color)
        setToast({ message: '主机标签保存成功', type: 'success' })
      }
      setEditing(undefined)
      await loadCatalog()
      await onChanged()
    } catch (error) {
      setToast({ message: `${editing.kind === 'group' ? '主机分组' : '主机标签'}保存失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const confirmDelete = async () => {
    if (!deleting) return
    setSaving(true)
    try {
      if (deleting.kind === 'group') {
        await deleteNodeGroup(deleting.id)
        setToast({ message: '主机分组删除成功', type: 'success' })
      } else {
        await deleteNodeTag(deleting.id)
        setToast({ message: '主机标签删除成功', type: 'success' })
      }
      setDeleting(undefined)
      await loadCatalog()
      await onChanged()
    } catch (error) {
      setToast({ message: `${deleting.kind === 'group' ? '主机分组' : '主机标签'}删除失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-5">
      <section role="dialog" aria-modal="true" aria-label="管理主机分组与标签" className="soft-modal-shell flex max-h-[88vh] w-full max-w-3xl flex-col">
        <header className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
          <div>
            <p className="text-xs font-black tracking-[0.16em] text-primary">HOST ORGANIZATION</p>
            <h3 className="mt-1 text-lg font-black text-foreground">管理分组与标签</h3>
            <p className="mt-1 text-xs font-bold text-muted-foreground">分组用于主导航，标签用于交叉筛选。</p>
          </div>
          <button type="button" aria-label="关闭" disabled={saving} onClick={onClose} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button>
        </header>
        <div className="border-b border-border px-5 py-3">
          <div className="inline-flex rounded-2xl border border-border bg-surface p-1">
            <ViewButton active={tab === 'groups'} onClick={() => { setTab('groups'); setEditing(undefined) }} icon={<FolderCog size={14} />} label="分组" />
            <ViewButton active={tab === 'tags'} onClick={() => { setTab('tags'); setEditing(undefined) }} icon={<Tags size={14} />} label="标签" />
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <div className="flex flex-col gap-2 rounded-2xl border border-border bg-surface/70 p-3 sm:flex-row sm:items-center">
            <input aria-label={tab === 'groups' ? '新分组名称' : '新标签名称'} value={createName} maxLength={tab === 'groups' ? 64 : 32} onChange={(event) => setCreateName(event.target.value)} placeholder={tab === 'groups' ? '输入新分组名称' : '输入新标签名称'} className="soft-input min-h-10 min-w-0 flex-1 px-3 text-sm font-bold" />
            {tab === 'tags' ? <ColorSelect value={createColor} onChange={setCreateColor} /> : null}
            <button type="button" onClick={() => void create()} disabled={saving || !createName.trim()} className="soft-button inline-flex min-h-10 items-center justify-center gap-2 bg-primary px-4 text-xs font-black text-primary-foreground disabled:opacity-60"><Plus size={15} />创建</button>
          </div>
          {loading ? <div className="flex min-h-40 items-center justify-center text-sm font-bold text-muted-foreground"><Loader2 className="mr-2 animate-spin" size={18} />正在加载</div> : null}
          {!loading && tab === 'groups' ? (
            <div className="mt-4 divide-y divide-border rounded-2xl border border-border bg-card">
              {groups.map((group) => <CatalogRow key={group.id} name={group.name} detail={`${group.node_count} 台节点`} editing={editing?.kind === 'group' && editing.id === group.id ? editing : undefined} saving={saving} onEdit={() => setEditing({ kind: 'group', id: group.id, name: group.name, color: 'gray' })} onEditingChange={(name) => setEditing((current) => current ? { ...current, name } : current)} onSave={() => void saveEdit()} onCancel={() => setEditing(undefined)} onDelete={() => setDeleting({ kind: 'group', id: group.id, name: group.name, nodeCount: group.node_count })} />)}
              {groups.length === 0 ? <EmptyCatalog label="尚未创建分组" /> : null}
            </div>
          ) : null}
          {!loading && tab === 'tags' ? (
            <div className="mt-4 divide-y divide-border rounded-2xl border border-border bg-card">
              {tags.map((tag) => <CatalogRow key={tag.id} name={tag.name} detail={`${tag.node_count} 台节点`} tag={tag} editing={editing?.kind === 'tag' && editing.id === tag.id ? editing : undefined} saving={saving} onEdit={() => setEditing({ kind: 'tag', id: tag.id, name: tag.name, color: tag.color })} onEditingChange={(name) => setEditing((current) => current ? { ...current, name } : current)} onColorChange={(color) => setEditing((current) => current ? { ...current, color } : current)} onSave={() => void saveEdit()} onCancel={() => setEditing(undefined)} onDelete={() => setDeleting({ kind: 'tag', id: tag.id, name: tag.name, nodeCount: tag.node_count })} />)}
              {tags.length === 0 ? <EmptyCatalog label="尚未创建标签" /> : null}
            </div>
          ) : null}
        </div>
        <footer className="soft-modal-footer flex justify-end border-t px-5 py-4"><button type="button" onClick={onClose} disabled={saving} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black text-foreground">完成</button></footer>
      </section>
      {deleting ? (
        <div className="soft-modal-overlay fixed inset-0 z-[60] flex items-center justify-center px-3">
          <section role="dialog" aria-modal="true" aria-label={`删除${deleting.kind === 'group' ? '分组' : '标签'}`} className="soft-modal-shell w-full max-w-md">
            <header className="soft-modal-header border-b px-5 py-4"><h4 className="text-base font-black text-foreground">删除“{deleting.name}”</h4></header>
            <div className="px-5 py-5 text-sm font-bold leading-6 text-muted-foreground">{deleting.kind === 'group' ? `该分组包含 ${deleting.nodeCount} 台节点，删除后这些节点会移到“未分组”。` : `该标签关联 ${deleting.nodeCount} 台节点，删除只会解除标签关联。`}</div>
            <footer className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4"><button type="button" disabled={saving} onClick={() => setDeleting(undefined)} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black">取消</button><button type="button" disabled={saving} onClick={() => void confirmDelete()} className="soft-button min-h-10 bg-danger px-4 text-xs font-black text-white disabled:opacity-60">{saving ? '正在删除...' : '确认删除'}</button></footer>
          </section>
        </div>
      ) : null}
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(undefined)} /> : null}
    </div>
  )
}

function CatalogRow({ name, detail, tag, editing, saving, onEdit, onEditingChange, onColorChange, onSave, onCancel, onDelete }: { name: string, detail: string, tag?: NodeTag, editing?: { name: string, color: NodeTag['color'] }, saving: boolean, onEdit: () => void, onEditingChange: (name: string) => void, onColorChange?: (color: NodeTag['color']) => void, onSave: () => void, onCancel: () => void, onDelete: () => void }) {
  return (
    <div className="flex min-h-16 flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
      {editing ? <input aria-label="编辑名称" autoFocus value={editing.name} onChange={(event) => onEditingChange(event.target.value)} className="soft-input min-h-9 min-w-0 flex-1 px-3 text-sm font-bold" /> : <div className="min-w-0 flex-1"><div className="flex items-center gap-2">{tag ? <NodeTagChip tag={tag} /> : <span className="truncate text-sm font-black text-foreground">{name}</span>}</div><p className="mt-1 text-xs font-bold text-muted-foreground">{detail}</p></div>}
      {editing && tag && onColorChange ? <ColorSelect value={editing.color} onChange={onColorChange} /> : null}
      <div className="flex shrink-0 items-center gap-2">
        {editing ? <><button type="button" aria-label="保存" disabled={saving || !editing.name.trim()} onClick={onSave} className="soft-button inline-flex h-9 w-9 items-center justify-center bg-primary text-primary-foreground"><Check size={15} /></button><button type="button" aria-label="取消编辑" disabled={saving} onClick={onCancel} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={15} /></button></> : <><button type="button" aria-label={`编辑 ${name}`} onClick={onEdit} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground"><Pencil size={15} /></button><button type="button" aria-label={`删除 ${name}`} onClick={onDelete} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-danger/25 bg-danger/10 text-danger"><Trash2 size={15} /></button></>}
      </div>
    </div>
  )
}

function ColorSelect({ value, onChange }: { value: NodeTag['color'], onChange: (color: NodeTag['color']) => void }) {
  return <select aria-label="标签颜色" value={value} onChange={(event) => onChange(event.target.value as NodeTag['color'])} className="soft-input min-h-10 px-3 text-xs font-black">{tagColors.map((color) => <option key={color} value={color}>{colorLabel(color)}</option>)}</select>
}

function EmptyCatalog({ label }: { label: string }) {
  return <div className="px-4 py-10 text-center text-sm font-bold text-muted-foreground">{label}</div>
}

export function NodeTagChip({ tag, compact = false }: { tag: NodeTagSummary, compact?: boolean }) {
  const tone = tagTone(tag.color)
  return <span className={`inline-flex max-w-full items-center rounded-full border px-2 py-0.5 font-black ${compact ? 'text-[10px]' : 'text-xs'} ${tone}`}><span className="truncate">{tag.name}</span></span>
}

function tagTone(color: NodeTagSummary['color']) {
  if (color === 'green') return 'border-success/25 bg-success/10 text-success'
  if (color === 'teal') return 'border-primary/25 bg-primary/10 text-primary'
  if (color === 'blue') return 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300'
  if (color === 'amber') return 'border-warning/25 bg-warning/10 text-warning'
  if (color === 'red') return 'border-danger/25 bg-danger/10 text-danger'
  return 'border-border bg-muted text-muted-foreground'
}

function colorLabel(color: NodeTag['color']) {
  return ({ green: '绿色', teal: '青色', blue: '蓝色', amber: '琥珀色', red: '红色', gray: '灰色' })[color]
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}
