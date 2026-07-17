import { useEffect, useMemo, useState } from 'react'
import { Loader2, Tags, X } from 'lucide-react'

import { createNodeTag, getNodeGroups, getNodeTags, updateBatchNodeMetadata } from '../api/client'
import type { BatchNodeMetadataUpdate, Node, NodeGroup, NodeTag } from '../types'
import { NodeTagChip } from './NodeOrganizationControls'
import { Toast } from './Toast'

const tagColors: NodeTag['color'][] = ['green', 'teal', 'blue', 'amber', 'red', 'gray']
const ungroupedID = 'ungrouped'
const maxTagsPerNode = 20

type SingleNodeOrganizationModalProps = {
  node: Node
  onClose: () => void
  onSaved: () => Promise<void> | void
}

export function SingleNodeOrganizationModal({ node, onClose, onSaved }: SingleNodeOrganizationModalProps) {
  const initialGroupID = node.group?.id ?? ungroupedID
  const initialTagIDs = useMemo(() => new Set((node.tags ?? []).map((tag) => tag.id)), [node.tags])
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [tags, setTags] = useState<NodeTag[]>([])
  const [groupID, setGroupID] = useState(initialGroupID)
  const [selectedTagIDs, setSelectedTagIDs] = useState<Set<string>>(() => new Set(initialTagIDs))
  const [loading, setLoading] = useState(true)
  const [loadingError, setLoadingError] = useState<string>()
  const [creatingTag, setCreatingTag] = useState(false)
  const [saving, setSaving] = useState(false)
  const [newTagName, setNewTagName] = useState('')
  const [newTagColor, setNewTagColor] = useState<NodeTag['color']>('teal')
  const [toast, setToast] = useState<{ message: string, type: 'success' | 'error' }>()

  const displayName = node.name || node.hostname || node.id
  const selectedTagCount = selectedTagIDs.size
  const update = useMemo(() => metadataUpdate(node.id, initialGroupID, initialTagIDs, groupID, selectedTagIDs), [groupID, initialGroupID, initialTagIDs, node.id, selectedTagIDs])
  const hasChanges = Boolean(update.group_id !== undefined || update.add_tag_ids?.length || update.remove_tag_ids?.length)
  const busy = creatingTag || saving

  const loadCatalog = () => {
    setLoading(true)
    setLoadingError(undefined)
    Promise.all([getNodeGroups(), getNodeTags()])
      .then(([groupResponse, tagResponse]) => {
        setGroups(groupResponse.groups)
        setTags(tagResponse.tags)
      })
      .catch((error: unknown) => {
        const message = errorMessage(error)
        setLoadingError(message)
        setToast({ message: `主机组织信息加载失败: ${message}`, type: 'error' })
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadCatalog()
  }, [])

  useEffect(() => {
    if (busy) return undefined
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [busy, onClose])

  const toggleTag = (tagID: string) => {
    setSelectedTagIDs((current) => {
      const next = new Set(current)
      if (next.has(tagID)) {
        next.delete(tagID)
      } else if (next.size < maxTagsPerNode) {
        next.add(tagID)
      }
      return next
    })
  }

  const addInlineTag = async () => {
    const name = newTagName.trim()
    if (!name || busy || selectedTagCount >= maxTagsPerNode) return

    const existing = tags.find((tag) => tag.name.trim().toLocaleLowerCase() === name.toLocaleLowerCase())
    if (existing) {
      if (!selectedTagIDs.has(existing.id)) toggleTag(existing.id)
      setNewTagName('')
      return
    }

    setCreatingTag(true)
    try {
      const created = await createNodeTag(name, newTagColor)
      setTags((current) => [...current, created])
      setSelectedTagIDs((current) => current.size < maxTagsPerNode ? new Set([...current, created.id]) : current)
      setNewTagName('')
      setToast({ message: '主机标签创建成功', type: 'success' })
    } catch (error) {
      setToast({ message: `主机标签创建失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setCreatingTag(false)
    }
  }

  const save = async () => {
    if (!hasChanges || busy) return
    setSaving(true)
    try {
      await updateBatchNodeMetadata(update)
      await onSaved()
      onClose()
    } catch (error) {
      setToast({ message: `主机组织信息调整失败: ${errorMessage(error)}`, type: 'error' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center px-3 py-5">
      <section role="dialog" aria-modal="true" aria-label="调整主机分组与标签" className="soft-modal-shell flex max-h-[86vh] w-full max-w-2xl flex-col">
        <header className="soft-modal-header flex items-start justify-between gap-3 border-b px-5 py-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2"><Tags size={16} aria-hidden="true" className="text-primary" /><h3 className="text-base font-black text-foreground">调整主机分组与标签</h3></div>
            <p className="mt-1 truncate text-xs font-bold text-muted-foreground">当前主机：{displayName}</p>
          </div>
          <button type="button" aria-label="关闭" disabled={busy} onClick={onClose} className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"><X size={16} aria-hidden="true" /></button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
          {loading ? <div className="flex min-h-36 items-center justify-center text-sm font-bold text-muted-foreground"><Loader2 size={18} className="mr-2 animate-spin" />加载分组与标签</div> : null}
          {!loading && loadingError ? <div className="soft-empty-state px-5 py-10 text-center"><p className="text-sm font-black text-foreground">分组与标签加载失败</p><p className="mt-1 text-xs font-bold text-muted-foreground">{loadingError}</p><button type="button" onClick={loadCatalog} className="soft-button mt-4 min-h-9 border border-border bg-card px-3 text-xs font-black text-foreground hover:bg-muted">重试</button></div> : null}
          {!loading && !loadingError ? (
            <div className="space-y-5">
              <div>
                <label htmlFor="single-node-group" className="text-xs font-black text-muted-foreground">主分组</label>
                <select id="single-node-group" value={groupID} onChange={(event) => setGroupID(event.target.value)} disabled={busy} className="soft-input mt-2 min-h-11 w-full px-3 text-sm font-bold disabled:cursor-not-allowed disabled:opacity-60">
                  <option value={ungroupedID}>未分组</option>
                  {groups.map((group) => <option key={group.id} value={group.id}>{group.name} · {group.node_count} 台</option>)}
                </select>
              </div>

              <div>
                <div className="flex items-baseline justify-between gap-3"><p className="text-xs font-black text-muted-foreground">标签（{selectedTagCount} / {maxTagsPerNode}）</p><p className="text-[11px] font-bold text-muted-foreground">最多关联 {maxTagsPerNode} 个标签</p></div>
                <div className="mt-2 flex flex-col gap-2 rounded-2xl border border-border bg-surface/70 p-3 sm:flex-row">
                  <input aria-label="创建或选择标签" list="single-node-tag-options" value={newTagName} maxLength={32} disabled={busy || selectedTagCount >= maxTagsPerNode} onChange={(event) => setNewTagName(event.target.value)} placeholder={selectedTagCount >= maxTagsPerNode ? '已达到标签上限' : '输入标签名称'} className="soft-input min-h-10 min-w-0 flex-1 px-3 text-sm font-bold disabled:cursor-not-allowed disabled:opacity-60" />
                  <datalist id="single-node-tag-options">{tags.map((tag) => <option key={tag.id} value={tag.name} />)}</datalist>
                  <select aria-label="新标签颜色" value={newTagColor} disabled={busy || selectedTagCount >= maxTagsPerNode} onChange={(event) => setNewTagColor(event.target.value as NodeTag['color'])} className="soft-input min-h-10 px-3 text-xs font-black disabled:cursor-not-allowed disabled:opacity-60">{tagColors.map((color) => <option key={color} value={color}>{color}</option>)}</select>
                  <button type="button" onClick={() => void addInlineTag()} disabled={busy || selectedTagCount >= maxTagsPerNode || !newTagName.trim()} className="soft-button min-h-10 bg-primary px-4 text-xs font-black text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50">{creatingTag ? '正在加入...' : '加入标签'}</button>
                </div>
                <div className="mt-3 divide-y divide-border overflow-hidden rounded-2xl border border-border bg-card">
                  {tags.map((tag) => {
                    const selected = selectedTagIDs.has(tag.id)
                    const disabled = busy || (!selected && selectedTagCount >= maxTagsPerNode)
                    return <label key={tag.id} className={`flex cursor-pointer items-center gap-3 px-4 py-3 ${disabled ? 'cursor-not-allowed opacity-55' : 'hover:bg-surface/70'}`}><input aria-label={tag.name} type="checkbox" checked={selected} disabled={disabled} onChange={() => toggleTag(tag.id)} className="h-4 w-4 accent-primary" /><NodeTagChip tag={tag} /><span className="ml-auto text-[11px] font-bold text-muted-foreground">{tag.node_count} 台节点</span></label>
                  })}
                  {tags.length === 0 ? <div className="px-4 py-8 text-center text-sm font-bold text-muted-foreground">还没有标签，可在上方直接创建。</div> : null}
                </div>
              </div>
            </div>
          ) : null}
        </div>

        <footer className="soft-modal-footer flex justify-end gap-2 border-t px-5 py-4">
          <button type="button" onClick={onClose} disabled={busy} className="soft-button min-h-10 border border-border bg-card px-4 text-xs font-black text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50">取消</button>
          <button type="button" onClick={() => void save()} disabled={loading || Boolean(loadingError) || busy || !hasChanges} className="soft-button min-h-10 bg-primary px-4 text-xs font-black text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50">{saving ? '正在保存...' : '保存更改'}</button>
        </footer>
      </section>
      {toast ? <Toast message={toast.message} type={toast.type} onClose={() => setToast(undefined)} /> : null}
    </div>
  )
}

function metadataUpdate(nodeID: string, initialGroupID: string, initialTagIDs: Set<string>, groupID: string, selectedTagIDs: Set<string>): BatchNodeMetadataUpdate {
  const update: BatchNodeMetadataUpdate = { node_ids: [nodeID] }
  if (groupID !== initialGroupID) update.group_id = groupID === ungroupedID ? null : groupID

  const addTagIDs = [...selectedTagIDs].filter((tagID) => !initialTagIDs.has(tagID))
  const removeTagIDs = [...initialTagIDs].filter((tagID) => !selectedTagIDs.has(tagID))
  if (addTagIDs.length > 0) update.add_tag_ids = addTagIDs
  if (removeTagIDs.length > 0) update.remove_tag_ids = removeTagIDs
  return update
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}
