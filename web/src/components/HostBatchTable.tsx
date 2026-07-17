import { useEffect, useMemo, useState } from 'react'
import { ArrowRight, FolderInput, Loader2, Rocket, Tags, X } from 'lucide-react'

import { createNodeTag, getNodeGroups, getNodeTags, updateBatchNodeMetadata } from '../api/client'
import { formatPercent } from '../lib/format'
import type { Node, NodeGroup, NodeTag } from '../types'
import { BatchAgentUpgradeModal } from './BatchAgentUpgradeModal'
import { NodeTagChip } from './NodeOrganizationControls'
import { Toast } from './Toast'

type HostBatchTableProps = {
  nodes: Node[]
  onOpenNode: (node: Node) => void
  onNodesChanged: () => Promise<void> | void
}

type MetadataModal = 'group' | 'tags'

const tagColors: NodeTag['color'][] = ['green', 'teal', 'blue', 'amber', 'red', 'gray']

export function HostBatchTable({ nodes, onOpenNode, onNodesChanged }: HostBatchTableProps) {
  const [selectedIDs, setSelectedIDs] = useState<Set<string>>(new Set())
  const [modal, setModal] = useState<MetadataModal>()
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [tags, setTags] = useState<NodeTag[]>([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [groupID, setGroupID] = useState('ungrouped')
  const [addTagIDs, setAddTagIDs] = useState<Set<string>>(new Set())
  const [removeTagIDs, setRemoveTagIDs] = useState<Set<string>>(new Set())
  const [newTagName, setNewTagName] = useState('')
  const [newTagColor, setNewTagColor] = useState<NodeTag['color']>('teal')
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' }>()
  const [upgradeNodes, setUpgradeNodes] = useState<Node[]>()

  useEffect(() => {
    const valid = new Set(nodes.map((node) => node.id))
    setSelectedIDs((current) => new Set([...current].filter((id) => valid.has(id))))
  }, [nodes])

  const selectedNodes = useMemo(() => nodes.filter((node) => selectedIDs.has(node.id)), [nodes, selectedIDs])
  const allSelected = nodes.length > 0 && nodes.every((node) => selectedIDs.has(node.id))

  const toggleNode = (nodeID: string) => {
    setSelectedIDs((current) => {
      const next = new Set(current)
      if (next.has(nodeID)) next.delete(nodeID)
      else if (next.size < 100) next.add(nodeID)
      return next
    })
  }

  const toggleAll = () => {
    if (allSelected) {
      setSelectedIDs(new Set())
      return
    }
    setSelectedIDs(new Set(nodes.slice(0, 100).map((node) => node.id)))
  }

  const openModal = async (kind: MetadataModal) => {
    if (selectedIDs.size === 0) return
    setModal(kind)
    setCatalogLoading(true)
    try {
      const [groupResponse, tagResponse] = await Promise.all([getNodeGroups(), getNodeTags()])
      setGroups(groupResponse.groups)
      setTags(tagResponse.tags)
      setGroupID('ungrouped')
      setAddTagIDs(new Set())
      setRemoveTagIDs(new Set())
      setNewTagName('')
    } catch (error) {
      setToast({ message: `主机元数据加载失败: ${errorMessage(error)}`, type: 'error' })
      setModal(undefined)
    } finally {
      setCatalogLoading(false)
    }
  }

  const moveGroup = async () => {
    setSaving(true)
    try {
      await updateBatchNodeMetadata({ node_ids: [...selectedIDs], group_id: groupID === 'ungrouped' ? null : groupID })
      setToast({ message: `主机分组调整成功，共 ${selectedIDs.size} 台`, type: 'success' })
      setModal(undefined)
      await onNodesChanged()
    } catch (error) {
      setToast({ message: `主机分组调整失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const attachInlineTag = async () => {
    const name = newTagName.trim()
    if (!name) return
    const existing = tags.find((tag) => tag.name.trim().toLowerCase() === name.toLowerCase())
    if (existing) {
      selectTagMode(existing.id, 'add')
      setNewTagName('')
      return
    }
    setSaving(true)
    try {
      const created = await createNodeTag(name, newTagColor)
      setTags((current) => [...current, created])
      selectTagMode(created.id, 'add')
      setNewTagName('')
      setToast({ message: '主机标签创建成功', type: 'success' })
    } catch (error) {
      setToast({ message: `主机标签创建失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const selectTagMode = (tagID: string, mode: 'add' | 'remove' | 'none') => {
    setAddTagIDs((current) => {
      const next = new Set(current)
      if (mode === 'add') next.add(tagID)
      else next.delete(tagID)
      return next
    })
    setRemoveTagIDs((current) => {
      const next = new Set(current)
      if (mode === 'remove') next.add(tagID)
      else next.delete(tagID)
      return next
    })
  }

  const saveTags = async () => {
    if (addTagIDs.size === 0 && removeTagIDs.size === 0) return
    setSaving(true)
    try {
      await updateBatchNodeMetadata({ node_ids: [...selectedIDs], add_tag_ids: [...addTagIDs], remove_tag_ids: [...removeTagIDs] })
      setToast({ message: `主机标签调整成功，共 ${selectedIDs.size} 台`, type: 'success' })
      setModal(undefined)
      await onNodesChanged()
    } catch (error) {
      setToast({ message: `主机标签调整失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="soft-panel min-w-0 overflow-hidden">
      <div className="flex min-h-14 flex-wrap items-center gap-2 border-b border-border bg-surface/65 px-4 py-3" role="toolbar" aria-label="批量主机操作">
        <span className="mr-auto text-xs font-black text-muted-foreground">已选择 <span className="text-foreground">{selectedIDs.size}</span> / 100 台</span>
        <button type="button" disabled={selectedIDs.size === 0} onClick={() => void openModal('group')} className="soft-button inline-flex min-h-9 items-center gap-2 border border-border bg-card px-3 text-xs font-black text-foreground disabled:opacity-45"><FolderInput size={14} />移动分组</button>
        <button type="button" disabled={selectedIDs.size === 0} onClick={() => void openModal('tags')} className="soft-button inline-flex min-h-9 items-center gap-2 border border-border bg-card px-3 text-xs font-black text-foreground disabled:opacity-45"><Tags size={14} />调整标签</button>
        <button type="button" disabled={selectedIDs.size === 0} onClick={() => setUpgradeNodes(selectedNodes)} className="soft-button inline-flex min-h-9 items-center gap-2 bg-primary px-3 text-xs font-black text-primary-foreground disabled:opacity-45"><Rocket size={14} />升级 Agent</button>
      </div>

      <div className="overflow-x-auto">
        <table className="soft-table min-w-[980px] w-full text-left">
          <thead><tr><th className="w-12 px-4 py-3"><input aria-label="选择当前筛选结果" type="checkbox" checked={allSelected} onChange={toggleAll} /></th><th className="px-3 py-3">主机</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">分组</th><th className="px-3 py-3">标签</th><th className="px-3 py-3">IP</th><th className="px-3 py-3">CPU / 内存</th><th className="px-3 py-3">Agent</th><th className="w-16 px-3 py-3"><span className="sr-only">详情</span></th></tr></thead>
          <tbody>{nodes.map((node) => <HostBatchRow key={node.id} node={node} selected={selectedIDs.has(node.id)} onToggle={() => toggleNode(node.id)} onOpen={() => onOpenNode(node)} />)}</tbody>
        </table>
      </div>

      {nodes.length === 0 ? <div className="soft-empty-state px-5 py-12 text-center text-sm font-bold text-muted-foreground">当前筛选条件下没有主机。</div> : null}
      {modal ? <MetadataModal kind={modal} selectedCount={selectedIDs.size} loading={catalogLoading} saving={saving} groups={groups} tags={tags} groupID={groupID} setGroupID={setGroupID} addTagIDs={addTagIDs} removeTagIDs={removeTagIDs} newTagName={newTagName} setNewTagName={setNewTagName} newTagColor={newTagColor} setNewTagColor={setNewTagColor} onTagMode={selectTagMode} onAttachInlineTag={() => void attachInlineTag()} onClose={() => !saving && setModal(undefined)} onSave={() => void (modal === 'group' ? moveGroup() : saveTags())} /> : null}
      {upgradeNodes ? <BatchAgentUpgradeModal nodes={upgradeNodes} onClose={() => setUpgradeNodes(undefined)} onFinished={async ({ succeeded, failed, skipped }) => { await onNodesChanged(); setToast({ message: failed > 0 ? `Agent批量升级失败: 成功 ${succeeded}，失败 ${failed}，跳过 ${skipped}` : `Agent批量升级成功: 成功 ${succeeded}，跳过 ${skipped}`, type: failed > 0 ? 'error' : 'success' }) }} /> : null}
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(undefined)} /> : null}
    </section>
  )
}

function HostBatchRow({ node, selected, onToggle, onOpen }: { node: Node, selected: boolean, onToggle: () => void, onOpen: () => void }) {
  const metric = node.latest_metric
  const tags = node.tags ?? []
  return (
    <tr className={selected ? 'bg-primary/5' : undefined}>
      <td className="px-4 py-3"><input aria-label={`选择 ${node.name || node.hostname}`} type="checkbox" checked={selected} onChange={onToggle} /></td>
      <td className="px-3 py-3"><button type="button" onClick={onOpen} className="max-w-52 truncate text-sm font-black text-foreground hover:text-primary">{node.name || node.hostname}</button><p className="mt-0.5 max-w-52 truncate text-[11px] font-bold text-muted-foreground">{node.hostname}</p></td>
      <td className="px-3 py-3"><StatusChip status={node.status} /></td>
      <td className="px-3 py-3 text-xs font-bold text-muted-foreground">{node.group?.name ?? '未分组'}</td>
      <td className="px-3 py-3"><div className="flex max-w-56 flex-wrap gap-1">{tags.slice(0, 3).map((tag) => <NodeTagChip key={tag.id} tag={tag} compact />)}{tags.length > 3 ? <span className="text-[10px] font-black text-muted-foreground">+{tags.length - 3}</span> : null}</div></td>
      <td className="px-3 py-3 font-mono text-xs font-bold text-muted-foreground">{node.ip || '—'}</td>
      <td className="px-3 py-3 text-xs font-black text-foreground">{metric ? `${formatPercent(metric.cpu_usage)} / ${formatPercent(metric.memory_usage)}` : '— / —'}</td>
      <td className="px-3 py-3 font-mono text-xs font-bold text-muted-foreground">{node.agent_version || '未知'}</td>
      <td className="px-3 py-3"><button type="button" aria-label={`查看 ${node.name || node.hostname}`} onClick={onOpen} className="soft-button inline-flex h-8 w-8 items-center justify-center border border-border bg-card text-muted-foreground"><ArrowRight size={14} /></button></td>
    </tr>
  )
}

function MetadataModal({ kind, selectedCount, loading, saving, groups, tags, groupID, setGroupID, addTagIDs, removeTagIDs, newTagName, setNewTagName, newTagColor, setNewTagColor, onTagMode, onAttachInlineTag, onClose, onSave }: { kind: MetadataModal, selectedCount: number, loading: boolean, saving: boolean, groups: NodeGroup[], tags: NodeTag[], groupID: string, setGroupID: (id: string) => void, addTagIDs: Set<string>, removeTagIDs: Set<string>, newTagName: string, setNewTagName: (name: string) => void, newTagColor: NodeTag['color'], setNewTagColor: (color: NodeTag['color']) => void, onTagMode: (tagID: string, mode: 'add' | 'remove' | 'none') => void, onAttachInlineTag: () => void, onClose: () => void, onSave: () => void }) {
  const canSave = kind === 'group' || addTagIDs.size > 0 || removeTagIDs.size > 0
  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-5">
      <section role="dialog" aria-modal="true" aria-label={kind === 'group' ? '批量移动分组' : '批量调整标签'} className="soft-modal-shell flex max-h-[86vh] w-full max-w-2xl flex-col">
        <header className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4"><div><h3 className="text-base font-black text-foreground">{kind === 'group' ? '批量移动分组' : '批量调整标签'}</h3><p className="mt-1 text-xs font-bold text-muted-foreground">将影响 {selectedCount} 台已选主机</p></div><button type="button" aria-label="关闭" disabled={saving} onClick={onClose} className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-muted-foreground"><X size={16} /></button></header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
          {loading ? <div className="flex min-h-32 items-center justify-center text-sm font-bold text-muted-foreground"><Loader2 className="mr-2 animate-spin" size={18} />加载分组标签</div> : null}
          {!loading && kind === 'group' ? <div><label htmlFor="batch-group" className="text-xs font-black text-muted-foreground">目标分组</label><select id="batch-group" value={groupID} onChange={(event) => setGroupID(event.target.value)} className="soft-input mt-2 min-h-11 w-full px-3 text-sm font-bold"><option value="ungrouped">未分组</option>{groups.map((group) => <option key={group.id} value={group.id}>{group.name} · {group.node_count} 台</option>)}</select></div> : null}
          {!loading && kind === 'tags' ? <div className="space-y-4"><div className="flex flex-col gap-2 rounded-2xl border border-border bg-surface/70 p-3 sm:flex-row"><input aria-label="创建或选择标签" list="batch-tag-options" value={newTagName} maxLength={32} onChange={(event) => setNewTagName(event.target.value)} placeholder="输入标签名称" className="soft-input min-h-10 min-w-0 flex-1 px-3 text-sm font-bold" /><datalist id="batch-tag-options">{tags.map((tag) => <option key={tag.id} value={tag.name} />)}</datalist><select aria-label="新标签颜色" value={newTagColor} onChange={(event) => setNewTagColor(event.target.value as NodeTag['color'])} className="soft-input min-h-10 px-3 text-xs font-black">{tagColors.map((color) => <option key={color} value={color}>{color}</option>)}</select><button type="button" onClick={onAttachInlineTag} disabled={saving || !newTagName.trim()} className="soft-button min-h-10 bg-primary px-4 text-xs font-black text-primary-foreground disabled:opacity-50">加入待添加</button></div><div className="divide-y divide-border rounded-2xl border border-border bg-card">{tags.map((tag) => { const mode = addTagIDs.has(tag.id) ? 'add' : removeTagIDs.has(tag.id) ? 'remove' : 'none'; return <div key={tag.id} className="flex flex-wrap items-center gap-2 px-4 py-3"><NodeTagChip tag={tag} /><span className="mr-auto text-xs font-bold text-muted-foreground">{tag.node_count} 台节点</span><button type="button" aria-pressed={mode === 'add'} onClick={() => onTagMode(tag.id, mode === 'add' ? 'none' : 'add')} className={`soft-button min-h-8 px-3 text-xs font-black ${mode === 'add' ? 'bg-success text-white' : 'border border-success/25 bg-success/10 text-success'}`}>添加</button><button type="button" aria-pressed={mode === 'remove'} onClick={() => onTagMode(tag.id, mode === 'remove' ? 'none' : 'remove')} className={`soft-button min-h-8 px-3 text-xs font-black ${mode === 'remove' ? 'bg-danger text-white' : 'border border-danger/25 bg-danger/10 text-danger'}`}>移除</button></div>})}{tags.length === 0 ? <div className="px-4 py-8 text-center text-sm font-bold text-muted-foreground">还没有标签，可在上方直接创建。</div> : null}</div></div> : null}
        </div>
        <footer className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4"><button type="button" onClick={onClose} disabled={saving} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black">取消</button><button type="button" onClick={onSave} disabled={loading || saving || !canSave} className="soft-button min-h-10 bg-primary px-4 text-xs font-black text-primary-foreground disabled:opacity-50">{saving ? '正在保存...' : '确认应用'}</button></footer>
      </section>
    </div>
  )
}

function StatusChip({ status }: { status: string }) {
  const online = status === 'online'
  return <span className={`inline-flex shrink-0 rounded-full px-2 py-0.5 text-[10px] font-black ${online ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'}`}>{online ? '在线' : '离线'}</span>
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}
