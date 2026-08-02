import { useCallback, useEffect, useRef, useState } from 'react'
import { CircleAlert, KeyRound, LoaderCircle, Pencil, Plus, RadioTower, Search, Star, Trash2, X } from 'lucide-react'

import { createAIProvider, deleteAIProvider, getAIProviders, listAIProviderModels, setDefaultAIProvider, testAIProvider, updateAIProvider } from '../../api/client'
import type { AIProvider, AIProviderInput } from '../../types'
import { Toast } from '../Toast'
import { aiProvidersChangedEvent } from './useAIAssistantState'

type ProviderDraft = {
  name: string
  baseURL: string
  model: string
  apiKey: string
  clearAPIKey: boolean
}

const emptyDraft: ProviderDraft = { name: '', baseURL: '', model: '', apiKey: '', clearAPIKey: false }

function errorText(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}

export function AIProviderSettings() {
  const [providers, setProviders] = useState<AIProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<AIProvider | 'new'>()
  const [draft, setDraft] = useState<ProviderDraft>(emptyDraft)
  const [deleteCandidate, setDeleteCandidate] = useState<AIProvider>()
  const [busyID, setBusyID] = useState<string>()
  const [toast, setToast] = useState<{ type: 'success' | 'error', message: string }>()
  const [detectedModels, setDetectedModels] = useState<string[]>()
  const [detecting, setDetecting] = useState(false)
  const addButtonRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await getAIProviders(signal)
    setProviders(response.providers)
    return response.providers
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    load(controller.signal)
      .catch((error) => {
        if (!controller.signal.aborted) setToast({ type: 'error', message: `模型列表加载失败: ${errorText(error)}` })
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [load])

  useEffect(() => {
    if (editing || deleteCandidate) dialogRef.current?.focus()
  }, [deleteCandidate, editing])

  const announceChanged = () => window.dispatchEvent(new Event(aiProvidersChangedEvent))

  const openCreate = () => {
    setDraft(emptyDraft)
    setDetectedModels(undefined)
    setEditing('new')
  }

  const openEdit = (provider: AIProvider) => {
    setDraft({ name: provider.name, baseURL: provider.base_url, model: provider.model, apiKey: '', clearAPIKey: false })
    setDetectedModels(undefined)
    setEditing(provider)
  }

  const closeDialog = () => {
    setEditing(undefined)
    setDeleteCandidate(undefined)
    setDetectedModels(undefined)
    requestAnimationFrame(() => addButtonRef.current?.focus())
  }

  const detectModels = async () => {
    if (detecting) return
    setDetecting(true)
    setDetectedModels(undefined)
    try {
      const models = await listAIProviderModels(draft.baseURL, draft.apiKey, editing === 'new' ? undefined : editing?.id)
      setDetectedModels(models)
      if (models.length > 0 && !models.includes(draft.model)) setDraft((current) => ({ ...current, model: models[0] }))
    } catch (error) {
      setToast({ type: 'error', message: `模型自动检测失败: ${errorText(error)}` })
    } finally {
      setDetecting(false)
    }
  }

  const save = async () => {
    if (!editing) return
    const id = editing === 'new' ? 'new' : editing.id
    setBusyID(id)
    const input: AIProviderInput = {
      name: draft.name,
      protocol: 'openai_chat_completions',
      base_url: draft.baseURL,
      model: draft.model
    }
    if (editing === 'new' || draft.apiKey) input.api_key = draft.apiKey
    if (editing !== 'new' && draft.clearAPIKey) input.clear_api_key = true
    try {
      if (editing === 'new') await createAIProvider(input)
      else await updateAIProvider(editing.id, input)
      await load()
      announceChanged()
      setToast({ type: 'success', message: editing === 'new' ? '模型配置创建成功' : '模型配置更新成功，请重新检测能力' })
      closeDialog()
    } catch (error) {
      setToast({ type: 'error', message: `模型配置保存失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const probe = async (provider: AIProvider) => {
    setBusyID(provider.id)
    try {
      const updated = await testAIProvider(provider.id)
      setProviders((current) => current.map((item) => item.id === updated.id ? updated : item))
      announceChanged()
      setToast({ type: updated.chat_capable && updated.tools_capable ? 'success' : 'error', message: updated.chat_capable && updated.tools_capable ? '模型能力检测成功' : `模型能力检测失败: ${updated.probe_error || '不支持工具调用'}` })
    } catch (error) {
      await load().catch(() => undefined)
      announceChanged()
      setToast({ type: 'error', message: `模型能力检测失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const makeDefault = async (provider: AIProvider) => {
    setBusyID(provider.id)
    try {
      await setDefaultAIProvider(provider.id)
      await load()
      announceChanged()
      setToast({ type: 'success', message: '默认模型设置成功' })
    } catch (error) {
      setToast({ type: 'error', message: `默认模型设置失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const remove = async () => {
    if (!deleteCandidate) return
    setBusyID(deleteCandidate.id)
    try {
      await deleteAIProvider(deleteCandidate.id)
      await load()
      announceChanged()
      setToast({ type: 'success', message: '模型配置删除成功' })
      closeDialog()
    } catch (error) {
      setToast({ type: 'error', message: `模型配置删除失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  return (
    <section className="border-t border-border px-4 py-5 sm:px-5" aria-labelledby="ai-provider-settings-title">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-black text-primary">AI Providers</p>
          <h3 id="ai-provider-settings-title" className="mt-1 text-xl font-black text-foreground">AI 模型配置</h3>
          <p className="mt-1 max-w-3xl text-sm font-semibold leading-6 text-muted-foreground">模型请求会把当前对话和单次查询所需的有界运维数据发送到你配置的服务。API Key 只在 Server 端加密保存。</p>
        </div>
        <button ref={addButtonRef} type="button" onClick={openCreate} className="soft-button inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
          <Plus size={16} aria-hidden="true" />
          添加模型
        </button>
      </div>

      <div className="mt-4 overflow-hidden border border-border bg-card">
        {loading ? (
          <div className="flex min-h-28 items-center justify-center text-sm font-bold text-muted-foreground"><LoaderCircle className="mr-2 h-4 w-4 animate-spin" aria-hidden="true" />正在加载模型配置</div>
        ) : providers.length === 0 ? (
          <div className="px-5 py-8 text-center text-sm font-semibold text-muted-foreground">尚未配置模型。</div>
        ) : providers.map((provider) => {
          const capable = provider.chat_capable && provider.tools_capable
          const busy = busyID === provider.id
          return (
            <div key={provider.id} className="flex min-w-0 flex-wrap items-center gap-3 border-b border-border px-4 py-3 last:border-b-0">
              <div className="min-w-[220px] flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <p className="truncate text-sm font-black text-foreground">{provider.name}</p>
                  {provider.is_default ? <span className="shrink-0 bg-primary/10 px-2 py-0.5 text-[11px] font-black text-primary">默认</span> : null}
                </div>
                <p className="mt-1 truncate text-xs font-semibold text-muted-foreground">{provider.model} · {provider.base_url}</p>
              </div>
              <div className="flex shrink-0 items-center gap-2 text-xs font-black">
                <span className={provider.chat_capable ? 'text-success' : 'text-muted-foreground'}>Chat {provider.chat_capable ? '可用' : '未验证'}</span>
                <span className={provider.tools_capable ? 'text-success' : 'text-muted-foreground'}>Tools {provider.tools_capable ? '可用' : '未验证'}</span>
                <span className="text-muted-foreground">Key {provider.has_api_key ? '已保存' : '无'}</span>
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <button type="button" disabled={busy} onClick={() => void probe(provider)} title="检测聊天与工具能力" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:text-foreground disabled:opacity-50">{busy ? <LoaderCircle size={15} className="animate-spin" aria-hidden="true" /> : <RadioTower size={15} aria-hidden="true" />}<span className="sr-only">检测能力</span></button>
                <button type="button" disabled={busy || !capable || provider.is_default} onClick={() => void makeDefault(provider)} title="设为默认模型" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:text-primary disabled:opacity-40"><Star size={15} fill={provider.is_default ? 'currentColor' : 'none'} aria-hidden="true" /><span className="sr-only">设为默认模型</span></button>
                <button type="button" disabled={busy} onClick={() => openEdit(provider)} title="编辑模型配置" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:text-foreground disabled:opacity-50"><Pencil size={15} aria-hidden="true" /><span className="sr-only">编辑模型配置</span></button>
                <button type="button" disabled={busy} onClick={() => setDeleteCandidate(provider)} title="删除模型配置" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:border-danger/40 hover:text-danger disabled:opacity-50"><Trash2 size={15} aria-hidden="true" /><span className="sr-only">删除模型配置</span></button>
              </div>
            </div>
          )
        })}
      </div>

      {editing ? (
        <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-provider-dialog-title" tabIndex={-1} className="soft-modal-shell w-full max-w-xl outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeDialog() }}>
            <div className="soft-modal-header flex items-start justify-between gap-3 border-b px-4 py-3">
              <div>
                <p id="ai-provider-dialog-title" className="text-base font-black text-foreground">{editing === 'new' ? '添加模型配置' : '编辑模型配置'}</p>
                <p className="mt-1 text-xs font-semibold text-muted-foreground">OpenAI Chat Completions 兼容协议</p>
              </div>
              <button type="button" onClick={closeDialog} title="关闭" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground"><X size={16} aria-hidden="true" /><span className="sr-only">关闭</span></button>
            </div>
            <div className="grid gap-4 px-4 py-4 sm:grid-cols-2">
              <label className="text-sm font-black text-foreground sm:col-span-2">配置名称<input autoFocus value={draft.name} maxLength={191} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 text-sm font-semibold" placeholder="内部模型" /></label>
              <label className="text-sm font-black text-foreground sm:col-span-2">Base URL<input value={draft.baseURL} maxLength={2048} onChange={(event) => setDraft((current) => ({ ...current, baseURL: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 font-mono text-sm font-semibold" placeholder="https://api.example.com/v1" /></label>
              <label className="text-sm font-black text-foreground sm:col-span-2">API Key<input type="password" autoComplete="new-password" value={draft.apiKey} maxLength={16 * 1024} disabled={draft.clearAPIKey} onChange={(event) => setDraft((current) => ({ ...current, apiKey: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 font-mono text-sm font-semibold disabled:opacity-50" placeholder={editing === 'new' ? '可留空' : '留空保留现有 Key'} /></label>
              {editing !== 'new' && editing.has_api_key ? (
                <label className="flex items-center gap-2 text-sm font-bold text-muted-foreground sm:col-span-2"><input type="checkbox" checked={draft.clearAPIKey} onChange={(event) => setDraft((current) => ({ ...current, clearAPIKey: event.target.checked, apiKey: event.target.checked ? '' : current.apiKey }))} className="h-4 w-4 accent-primary" />清除当前 API Key</label>
              ) : null}
              <div className="flex flex-wrap items-end gap-3 sm:col-span-2">
                <div className="min-w-[220px] flex-1">
                  {detectedModels?.length ? (
                    <label className="text-sm font-black text-foreground">模型名称<select aria-label="模型名称" value={draft.model} onChange={(event) => setDraft((current) => ({ ...current, model: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 text-sm font-semibold">
                      {detectedModels.map((model) => <option key={model} value={model}>{model}</option>)}
                    </select></label>
                  ) : (
                    <label className="text-sm font-black text-foreground">模型名称<input value={draft.model} maxLength={255} onChange={(event) => setDraft((current) => ({ ...current, model: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 text-sm font-semibold" placeholder="gpt-4.1-mini" /></label>
                  )}
                </div>
                <button type="button" onClick={() => void detectModels()} disabled={!draft.baseURL.trim() || detecting || draft.clearAPIKey} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-4 text-sm font-black text-foreground disabled:cursor-not-allowed disabled:opacity-50">
                  {detecting ? <LoaderCircle size={16} className="animate-spin" aria-hidden="true" /> : <Search size={16} aria-hidden="true" />}
                  {detecting ? '检测中...' : '检测模型'}
                </button>
              </div>
              {detectedModels && detectedModels.length === 0 ? <p className="text-xs font-semibold text-muted-foreground sm:col-span-2">服务没有返回可选模型，可继续手动填写模型名称。</p> : null}
              {editing !== 'new' && editing.has_api_key && !draft.apiKey ? <p className="text-xs font-semibold text-muted-foreground sm:col-span-2">未输入新 Key 时，检测会由 Server 安全复用已保存的凭据。</p> : null}
              <div className="flex gap-3 border border-info/30 bg-info/10 px-3 py-3 text-xs font-semibold leading-5 text-info sm:col-span-2"><KeyRound className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><p>保存后 API Key 不会再次显示。修改 Base URL、模型或 Key 会清除旧能力状态，需要重新检测。</p></div>
            </div>
            <div className="soft-modal-footer flex justify-end gap-2 border-t px-4 py-3">
              <button type="button" onClick={closeDialog} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button>
              <button type="button" onClick={() => void save()} disabled={!draft.name.trim() || !draft.baseURL.trim() || !draft.model.trim() || Boolean(busyID)} className="soft-button min-h-10 bg-primary px-4 text-sm font-black text-primary-foreground disabled:opacity-50">{busyID ? '保存中...' : '保存配置'}</button>
            </div>
          </div>
        </div>
      ) : null}

      {deleteCandidate ? (
        <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-provider-delete-title" tabIndex={-1} className="soft-modal-shell w-full max-w-md outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeDialog() }}>
            <div className="soft-modal-header flex items-start gap-3 border-b px-4 py-3"><CircleAlert className="mt-0.5 h-5 w-5 shrink-0 text-danger" aria-hidden="true" /><div><p id="ai-provider-delete-title" className="font-black text-foreground">删除模型配置</p><p className="mt-1 break-words text-xs font-semibold text-muted-foreground">将删除“{deleteCandidate.name}”的配置和加密凭据，历史会话仍会保留模型快照。</p></div></div>
            <div className="soft-modal-footer flex justify-end gap-2 px-4 py-3">
              <button type="button" onClick={closeDialog} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button>
              <button type="button" onClick={() => void remove()} disabled={Boolean(busyID)} className="soft-button min-h-10 bg-danger px-4 text-sm font-black text-white disabled:opacity-50">{busyID ? '删除中...' : '确认删除'}</button>
            </div>
          </div>
        </div>
      ) : null}

      {toast ? <Toast {...toast} onClose={() => setToast(undefined)} /> : null}
    </section>
  )
}
