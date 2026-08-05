import { useCallback, useEffect, useRef, useState } from 'react'
import { ChevronDown, ChevronRight, CircleAlert, Download, KeyRound, ListChecks, LoaderCircle, Pencil, Plus, RadioTower, ShieldCheck, Star, Trash2, X } from 'lucide-react'

import { createAIProvider, deleteAIModel, deleteAIProvider, discoverAIProvider, getAIProviders, getAIRouting, importAIProviderModels, testAIModel, updateAIModel, updateAIProvider, updateAIRouting } from '../../api/client'
import type { AIProvider, AIProviderInput, AIProviderModel, AIRouting } from '../../types'
import { Toast } from '../Toast'
import { aiProvidersChangedEvent, isAIModelUsable } from './useAIAssistantState'

type ProviderDraft = {
  name: string
  baseURL: string
  apiKey: string
  clearAPIKey: boolean
  enabled: boolean
}

type ModelDraft = {
  provider: AIProvider
  model?: AIProviderModel
  modelID: string
  displayName: string
  enabled: boolean
}

type DiscoveryState = {
  provider: AIProvider
  models: string[]
  selected: string[]
  manualModelID: string
  manualDisplayName: string
}

type DeleteTarget =
  | { kind: 'provider'; provider: AIProvider }
  | { kind: 'model'; provider: AIProvider; model: AIProviderModel }
  | { kind: 'models'; models: Array<{ provider: AIProvider; model: AIProviderModel }> }

const emptyProviderDraft: ProviderDraft = { name: '', baseURL: '', apiKey: '', clearAPIKey: false, enabled: true }
const emptyRouting: AIRouting = { default_model_id: null, fallback_model_id: null }

function errorText(error: unknown) {
  return error instanceof Error ? error.message : '未知错误'
}

function formatCheck(status: AIProvider['discovery_status'], latency: number, checkedAt: string | null) {
  if (status === 'unknown') return '尚未检测'
  const parts = [status === 'success' ? '连接正常' : '连接失败']
  if (latency > 0) parts.push(`${latency} ms`)
  if (checkedAt) parts.push(new Date(checkedAt).toLocaleString())
  return parts.join(' · ')
}

function providerInput(provider: AIProvider, enabled = provider.enabled): AIProviderInput {
  return {
    name: provider.name,
    protocol: provider.protocol,
    base_url: provider.base_url,
    model: provider.model,
    enabled
  }
}

export function AIProviderSettings() {
  const [providers, setProviders] = useState<AIProvider[]>([])
  const [routing, setRouting] = useState<AIRouting>(emptyRouting)
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [editingProvider, setEditingProvider] = useState<AIProvider | 'new'>()
  const [providerDraft, setProviderDraft] = useState<ProviderDraft>(emptyProviderDraft)
  const [editingModel, setEditingModel] = useState<ModelDraft>()
  const [discovery, setDiscovery] = useState<DiscoveryState>()
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>()
  const [selectedModelIDs, setSelectedModelIDs] = useState<Set<string>>(new Set())
  const [busyID, setBusyID] = useState<string>()
  const [toast, setToast] = useState<{ type: 'success' | 'error'; message: string }>()
  const addButtonRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    const [providerResponse, routingResponse] = await Promise.all([getAIProviders(signal), getAIRouting(signal)])
    setProviders(providerResponse.providers)
    setRouting(routingResponse)
    return providerResponse.providers
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    load(controller.signal)
      .catch((error) => {
        if (!controller.signal.aborted) setToast({ type: 'error', message: `AI 配置加载失败: ${errorText(error)}` })
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [load])

  useEffect(() => {
    if (editingProvider || editingModel || discovery || deleteTarget) dialogRef.current?.focus()
  }, [deleteTarget, discovery, editingModel, editingProvider])

  const announceChanged = () => window.dispatchEvent(new Event(aiProvidersChangedEvent))

  const reloadAndAnnounce = async () => {
    const refreshed = await load()
    const availableModelIDs = new Set(refreshed.flatMap((provider) => provider.models.map((model) => model.id)))
    setSelectedModelIDs((current) => new Set([...current].filter((id) => availableModelIDs.has(id))))
    announceChanged()
  }

  const closeDialog = () => {
    setEditingProvider(undefined)
    setEditingModel(undefined)
    setDiscovery(undefined)
    setDeleteTarget(undefined)
    requestAnimationFrame(() => addButtonRef.current?.focus())
  }

  const openCreateProvider = () => {
    setProviderDraft(emptyProviderDraft)
    setEditingProvider('new')
  }

  const openEditProvider = (provider: AIProvider) => {
    setProviderDraft({ name: provider.name, baseURL: provider.base_url, apiKey: '', clearAPIKey: false, enabled: provider.enabled })
    setEditingProvider(provider)
  }

  const saveProvider = async (discoverAfterSave = false) => {
    if (!editingProvider) return
    const id = editingProvider === 'new' ? 'new-provider' : editingProvider.id
    setBusyID(id)
    const input: AIProviderInput = {
      name: providerDraft.name,
      protocol: 'openai_chat_completions',
      base_url: providerDraft.baseURL,
      model: editingProvider === 'new' ? '' : editingProvider.model,
      enabled: providerDraft.enabled
    }
    if (editingProvider === 'new' || providerDraft.apiKey) input.api_key = providerDraft.apiKey
    if (editingProvider !== 'new' && providerDraft.clearAPIKey) input.clear_api_key = true
    try {
      const saved = editingProvider === 'new'
        ? await createAIProvider(input)
        : await updateAIProvider(editingProvider.id, input)
      await reloadAndAnnounce()
      setExpanded((current) => new Set(current).add(saved.id))
      if (discoverAfterSave) {
        setEditingProvider(undefined)
        await discover(saved)
      } else {
        setToast({ type: 'success', message: editingProvider === 'new' ? 'Provider 创建成功' : 'Provider 更新成功' })
        closeDialog()
      }
    } catch (error) {
      setToast({ type: 'error', message: `Provider 保存失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const toggleProvider = async (provider: AIProvider) => {
    setBusyID(provider.id)
    try {
      await updateAIProvider(provider.id, providerInput(provider, !provider.enabled))
      await reloadAndAnnounce()
      setToast({ type: 'success', message: `Provider ${provider.enabled ? '停用' : '启用'}成功` })
    } catch (error) {
      setToast({ type: 'error', message: `Provider ${provider.enabled ? '停用' : '启用'}失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const discover = async (provider: AIProvider) => {
    setBusyID(`discover-${provider.id}`)
    try {
      const response = await discoverAIProvider(provider.id)
      const refreshed = await load()
      const latestProvider = refreshed.find((item) => item.id === provider.id) ?? response.provider
      const existingIDs = new Set(latestProvider.models.map((model) => model.model_id))
      setDiscovery({
        provider: latestProvider,
        models: response.models,
        selected: response.models.filter((modelID) => !existingIDs.has(modelID)),
        manualModelID: '',
        manualDisplayName: ''
      })
      setToast({ type: 'success', message: '模型发现成功' })
    } catch (error) {
      await load().catch(() => undefined)
      setToast({ type: 'error', message: `模型发现失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const toggleDiscoveredModel = (modelID: string) => {
    setDiscovery((current) => {
      if (!current) return current
      const selected = current.selected.includes(modelID)
        ? current.selected.filter((item) => item !== modelID)
        : [...current.selected, modelID]
      return { ...current, selected }
    })
  }

  const importDiscoveredModels = async () => {
    if (!discovery) return
    const existingIDs = new Set(discovery.provider.models.map((model) => model.model_id))
    const modelIDs = discovery.selected.filter((modelID) => !existingIDs.has(modelID))
    const manualModelID = discovery.manualModelID.trim()
    if (manualModelID && !existingIDs.has(manualModelID) && !modelIDs.includes(manualModelID)) modelIDs.push(manualModelID)
    if (modelIDs.length === 0) {
      setToast({ type: 'error', message: '模型导入失败: 请至少选择或手动填写一个新模型' })
      return
    }
    setBusyID(`import-${discovery.provider.id}`)
    try {
      await importAIProviderModels(discovery.provider.id, modelIDs.map((modelID) => ({
        model_id: modelID,
        display_name: modelID === manualModelID ? discovery.manualDisplayName.trim() : ''
      })))
      await reloadAndAnnounce()
      setExpanded((current) => new Set(current).add(discovery.provider.id))
      setToast({ type: 'success', message: '模型导入成功' })
      closeDialog()
    } catch (error) {
      setToast({ type: 'error', message: `模型导入失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const openAddModel = (provider: AIProvider) => {
    setEditingModel({ provider, modelID: '', displayName: '', enabled: true })
  }

  const openEditModel = (provider: AIProvider, model: AIProviderModel) => {
    setEditingModel({ provider, model, modelID: model.model_id, displayName: model.display_name, enabled: model.enabled })
  }

  const saveModel = async () => {
    if (!editingModel) return
    const id = editingModel.model?.id ?? `new-model-${editingModel.provider.id}`
    setBusyID(id)
    try {
      if (editingModel.model) {
        await updateAIModel(editingModel.model.id, {
          model_id: editingModel.modelID,
          display_name: editingModel.displayName,
          enabled: editingModel.enabled
        })
      } else {
        await importAIProviderModels(editingModel.provider.id, [{ model_id: editingModel.modelID, display_name: editingModel.displayName }], editingModel.enabled)
      }
      await reloadAndAnnounce()
      setExpanded((current) => new Set(current).add(editingModel.provider.id))
      setToast({ type: 'success', message: editingModel.model ? '模型更新成功' : '模型添加成功' })
      closeDialog()
    } catch (error) {
      setToast({ type: 'error', message: `模型保存失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const toggleModel = async (model: AIProviderModel) => {
    setBusyID(model.id)
    try {
      await updateAIModel(model.id, { model_id: model.model_id, display_name: model.display_name, enabled: !model.enabled })
      await reloadAndAnnounce()
      setToast({ type: 'success', message: `模型${model.enabled ? '停用' : '启用'}成功` })
    } catch (error) {
      setToast({ type: 'error', message: `模型${model.enabled ? '停用' : '启用'}失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const probeModel = async (model: AIProviderModel) => {
    setBusyID(`probe-${model.id}`)
    try {
      await testAIModel(model.id)
      await reloadAndAnnounce()
      setToast({ type: 'success', message: '模型能力检测成功' })
    } catch (error) {
      await reloadAndAnnounce().catch(() => undefined)
      setToast({ type: 'error', message: `模型能力检测失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const setModelRoute = async (model: AIProviderModel, kind: 'default' | 'fallback') => {
    setBusyID(`route-${kind}-${model.id}`)
    const next: AIRouting = { ...routing }
    if (kind === 'default') {
      next.default_model_id = routing.default_model_id === model.id ? null : model.id
      if (next.default_model_id && next.fallback_model_id === next.default_model_id) next.fallback_model_id = null
    } else {
      next.fallback_model_id = routing.fallback_model_id === model.id ? null : model.id
      if (next.fallback_model_id && next.default_model_id === next.fallback_model_id) next.default_model_id = null
    }
    try {
      await updateAIRouting(next)
      await reloadAndAnnounce()
      setToast({ type: 'success', message: `${kind === 'default' ? '默认' : '备用'}模型设置成功` })
    } catch (error) {
      setToast({ type: 'error', message: `${kind === 'default' ? '默认' : '备用'}模型设置失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const remove = async () => {
    if (!deleteTarget) return
    const targetID = deleteTarget.kind === 'provider'
      ? deleteTarget.provider.id
      : deleteTarget.kind === 'model'
        ? deleteTarget.model.id
        : 'selected-models'
    setBusyID(`delete-${targetID}`)
    try {
      if (deleteTarget.kind === 'provider') await deleteAIProvider(deleteTarget.provider.id)
      else if (deleteTarget.kind === 'model') await deleteAIModel(deleteTarget.model.id)
      else {
        for (const { model } of deleteTarget.models) await deleteAIModel(model.id)
      }
      await reloadAndAnnounce()
      if (deleteTarget.kind === 'models') {
        setSelectedModelIDs(new Set())
        setToast({ type: 'success', message: `${deleteTarget.models.length} 个模型删除成功` })
      } else {
        setToast({ type: 'success', message: `${deleteTarget.kind === 'provider' ? 'Provider' : '模型'}删除成功` })
      }
      closeDialog()
    } catch (error) {
      if (deleteTarget.kind === 'models') {
        await reloadAndAnnounce().catch(() => undefined)
        closeDialog()
      }
      setToast({ type: 'error', message: `${deleteTarget.kind === 'models' ? '批量删除模型' : deleteTarget.kind === 'provider' ? 'Provider' : '模型'}删除失败: ${errorText(error)}` })
    } finally {
      setBusyID(undefined)
    }
  }

  const toggleExpanded = (id: string) => {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const allModels = providers.flatMap((provider) => provider.models.map((model) => ({ provider, model })))
  const selectedModels = allModels.filter(({ model }) => selectedModelIDs.has(model.id))
  const allModelsSelected = allModels.length > 0 && selectedModels.length === allModels.length
  const selectedModelsHaveRoute = selectedModels.some(({ model }) => model.is_default || model.is_fallback)

  const toggleModelSelection = (modelID: string) => {
    setSelectedModelIDs((current) => {
      const next = new Set(current)
      if (next.has(modelID)) next.delete(modelID)
      else next.add(modelID)
      return next
    })
  }

  const toggleAllModels = () => {
    setSelectedModelIDs(allModelsSelected ? new Set() : new Set(allModels.map(({ model }) => model.id)))
  }

  const openDeleteSelected = () => {
    if (selectedModels.length === 0) return
    if (selectedModelsHaveRoute) {
      setToast({ type: 'error', message: '批量删除失败: 请先清除选中模型的默认或备用标记' })
      return
    }
    setDeleteTarget({ kind: 'models', models: selectedModels })
  }

  return (
    <section className="border-t border-border bg-surface/20 px-4 py-5 sm:px-5" aria-labelledby="ai-provider-settings-title">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 id="ai-provider-settings-title" className="text-xl font-black text-foreground">Provider 与模型</h3>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {allModels.length > 0 ? <button type="button" onClick={toggleAllModels} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-3 text-sm font-black text-foreground"><ListChecks size={16} aria-hidden="true" />{allModelsSelected ? '取消全选' : '全选模型'}</button> : null}
          <button ref={addButtonRef} type="button" onClick={openCreateProvider} className="soft-button inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
            <Plus size={16} aria-hidden="true" />添加 Provider
          </button>
        </div>
      </div>

      {selectedModels.length > 0 ? <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border border-border bg-surface px-3 py-2.5 text-sm">
        <div className="flex min-w-0 items-center gap-3">
          <span className="font-black text-foreground">已选择 {selectedModels.length} 个模型</span>
          {selectedModelsHaveRoute ? <span className="text-xs font-semibold text-warning">默认/备用模型需先解除标记</span> : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button type="button" onClick={() => setSelectedModelIDs(new Set())} className="soft-button min-h-9 border border-border bg-card px-3 text-xs font-black text-muted-foreground">取消选择</button>
          <button type="button" onClick={openDeleteSelected} disabled={Boolean(busyID) || selectedModelsHaveRoute} title={selectedModelsHaveRoute ? '请先清除默认或备用标记' : '删除已选择的模型'} className="soft-button inline-flex min-h-9 items-center gap-1.5 bg-danger px-3 text-xs font-black text-white disabled:opacity-40"><Trash2 size={14} aria-hidden="true" />删除已选模型</button>
        </div>
      </div> : null}

      <div className="mt-4 overflow-hidden border border-border bg-card">
        {loading ? (
          <div className="flex min-h-28 items-center justify-center text-sm font-bold text-muted-foreground"><LoaderCircle className="mr-2 h-4 w-4 animate-spin" aria-hidden="true" />正在加载 AI 配置</div>
        ) : providers.length === 0 ? (
          <div className="px-5 py-8 text-center text-sm font-semibold text-muted-foreground">尚未配置 Provider。</div>
        ) : providers.map((provider) => {
          const isExpanded = expanded.has(provider.id)
          const providerBusy = busyID === provider.id
          return (
            <div key={provider.id} className="border-b border-border last:border-b-0">
              <div className="grid min-w-0 grid-cols-[36px_44px_minmax(220px,1fr)_minmax(210px,0.8fr)_auto] items-center gap-2 px-3 py-2.5">
                <button type="button" onClick={() => toggleExpanded(provider.id)} title={isExpanded ? '收起模型' : '展开模型'} aria-expanded={isExpanded} className="soft-button flex h-9 w-9 items-center justify-center text-muted-foreground hover:bg-surface hover:text-foreground">
                  {isExpanded ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronRight size={16} aria-hidden="true" />}<span className="sr-only">{isExpanded ? '收起模型' : '展开模型'}</span>
                </button>
                <label className="flex items-center justify-center" title={provider.enabled ? '停用 Provider' : '启用 Provider'}>
                  <span className="sr-only">{provider.enabled ? '停用' : '启用'} Provider {provider.name}</span>
                  <input type="checkbox" checked={provider.enabled} disabled={providerBusy} onChange={() => void toggleProvider(provider)} className="h-4 w-4 accent-primary" />
                </label>
                <div className="min-w-0">
                  <div className="flex min-w-0 items-center gap-2">
                    <p className="truncate text-sm font-black text-foreground">{provider.name}</p>
                    <span className="shrink-0 text-[11px] font-black text-muted-foreground">{provider.models.length} 个模型</span>
                  </div>
                  <p className="mt-0.5 truncate text-xs font-semibold text-muted-foreground">{provider.base_url}</p>
                </div>
                <div className="min-w-0 text-xs font-semibold text-muted-foreground">
                  <p className={`truncate font-black ${provider.discovery_status === 'success' ? 'text-success' : provider.discovery_status === 'failure' ? 'text-danger' : ''}`}>{formatCheck(provider.discovery_status, provider.discovery_latency_ms, provider.discovered_at)}</p>
                  {provider.discovery_error ? <p className="mt-0.5 truncate text-danger" title={provider.discovery_error}>{provider.discovery_error}</p> : <p className="mt-0.5 truncate">Key {provider.has_api_key ? '已保存' : '未配置'}</p>}
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <button type="button" disabled={Boolean(busyID)} onClick={() => void discover(provider)} title="从 Provider 获取模型列表" className="soft-button inline-flex h-9 items-center gap-1.5 border border-border px-2.5 text-xs font-black text-muted-foreground hover:text-foreground disabled:opacity-50">{busyID === `discover-${provider.id}` ? <LoaderCircle size={15} className="animate-spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}<span>获取模型</span></button>
                  <button type="button" disabled={Boolean(busyID)} onClick={() => openAddModel(provider)} title="手动添加模型" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:text-foreground disabled:opacity-50"><Plus size={15} aria-hidden="true" /><span className="sr-only">手动添加模型</span></button>
                  <button type="button" disabled={Boolean(busyID)} onClick={() => openEditProvider(provider)} title="编辑 Provider" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:text-foreground disabled:opacity-50"><Pencil size={15} aria-hidden="true" /><span className="sr-only">编辑 Provider</span></button>
                  <button type="button" disabled={Boolean(busyID)} onClick={() => setDeleteTarget({ kind: 'provider', provider })} title="删除 Provider" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground hover:border-danger/40 hover:text-danger disabled:opacity-50"><Trash2 size={15} aria-hidden="true" /><span className="sr-only">删除 Provider</span></button>
                </div>
              </div>

              {isExpanded ? (
                <div className="border-t border-border bg-surface/60">
                  {provider.models.length === 0 ? (
                    <div className="flex items-center justify-between gap-3 px-14 py-4 text-xs font-semibold text-muted-foreground"><span>此 Provider 还没有模型。</span><button type="button" onClick={() => openAddModel(provider)} className="font-black text-primary">手动添加</button></div>
                  ) : provider.models.map((model) => {
                    const capable = isAIModelUsable(provider, model)
                    const modelBusy = busyID === model.id || busyID === `probe-${model.id}` || busyID?.endsWith(`-${model.id}`)
                    const routed = model.is_default || model.is_fallback
                    return (
                      <div key={model.id} className="grid min-w-0 grid-cols-[34px_80px_minmax(220px,1fr)_minmax(230px,0.9fr)_auto] items-center gap-2 border-b border-border/70 py-2 pl-14 pr-3 last:border-b-0">
                        <label className="flex items-center justify-center" title="选择模型">
                          <input type="checkbox" aria-label={`选择模型 ${model.display_name || model.model_id}`} checked={selectedModelIDs.has(model.id)} disabled={Boolean(busyID)} onChange={() => toggleModelSelection(model.id)} className="h-4 w-4 accent-primary" />
                        </label>
                        <label className="flex items-center gap-2 text-xs font-bold text-muted-foreground" title={model.enabled ? '停用模型' : '启用模型'}>
                          <input type="checkbox" checked={model.enabled} disabled={Boolean(busyID)} onChange={() => void toggleModel(model)} className="h-4 w-4 accent-primary" />
                          {model.enabled ? '启用' : '停用'}
                        </label>
                        <div className="min-w-0">
                          <div className="flex min-w-0 items-center gap-2">
                            <p className="truncate text-sm font-black text-foreground" title={model.model_id}>{model.display_name || model.model_id}</p>
                            {model.display_name ? <span className="truncate text-[11px] font-semibold text-muted-foreground">{model.model_id}</span> : null}
                            {model.is_default ? <span className="shrink-0 bg-primary/10 px-1.5 py-0.5 text-[10px] font-black text-primary">默认</span> : null}
                            {model.is_fallback ? <span className="shrink-0 bg-warning/10 px-1.5 py-0.5 text-[10px] font-black text-warning">备用</span> : null}
                          </div>
                          {model.probe_error ? <p className="mt-0.5 truncate text-[11px] font-semibold text-danger" title={model.probe_error}>{model.probe_error}</p> : null}
                        </div>
                        <div className="min-w-0 text-xs font-semibold text-muted-foreground">
                          <p className={`truncate font-black ${capable ? 'text-success' : model.probe_status === 'failure' ? 'text-danger' : ''}`}>Chat {model.chat_capable ? '可用' : '未验证'} · Tools {model.tools_capable ? '可用' : '未验证'}</p>
                          <p className="mt-0.5 truncate">{formatCheck(model.probe_status, model.probe_latency_ms, model.probed_at)}</p>
                        </div>
                        <div className="flex shrink-0 items-center gap-1">
                          <button type="button" disabled={Boolean(busyID) || !provider.enabled || !model.enabled} onClick={() => void probeModel(model)} title="检测 Chat/Tools 能力" className="soft-button flex h-8 w-8 items-center justify-center text-muted-foreground hover:bg-card hover:text-foreground disabled:opacity-40">{busyID === `probe-${model.id}` ? <LoaderCircle size={14} className="animate-spin" aria-hidden="true" /> : <RadioTower size={14} aria-hidden="true" />}<span className="sr-only">检测模型能力</span></button>
                          <button type="button" disabled={Boolean(busyID) || !capable} onClick={() => void setModelRoute(model, 'default')} title={model.is_default ? '清除默认模型' : '设为默认模型'} className="soft-button flex h-8 w-8 items-center justify-center text-muted-foreground hover:bg-card hover:text-primary disabled:opacity-40"><Star size={14} fill={model.is_default ? 'currentColor' : 'none'} aria-hidden="true" /><span className="sr-only">{model.is_default ? '清除默认模型' : '设为默认模型'}</span></button>
                          <button type="button" disabled={Boolean(busyID) || !capable} onClick={() => void setModelRoute(model, 'fallback')} title={model.is_fallback ? '清除备用模型' : '设为备用模型'} className="soft-button flex h-8 w-8 items-center justify-center text-muted-foreground hover:bg-card hover:text-warning disabled:opacity-40"><ShieldCheck size={14} fill={model.is_fallback ? 'currentColor' : 'none'} aria-hidden="true" /><span className="sr-only">{model.is_fallback ? '清除备用模型' : '设为备用模型'}</span></button>
                          <button type="button" disabled={Boolean(busyID)} onClick={() => openEditModel(provider, model)} title="编辑模型" className="soft-button flex h-8 w-8 items-center justify-center text-muted-foreground hover:bg-card hover:text-foreground disabled:opacity-40"><Pencil size={14} aria-hidden="true" /><span className="sr-only">编辑模型</span></button>
                          <button type="button" disabled={Boolean(busyID) || routed} onClick={() => setDeleteTarget({ kind: 'model', provider, model })} title={routed ? '先清除默认或备用设置' : '删除模型'} className="soft-button flex h-8 w-8 items-center justify-center text-muted-foreground hover:bg-danger/10 hover:text-danger disabled:opacity-40"><Trash2 size={14} aria-hidden="true" /><span className="sr-only">删除模型</span></button>
                          {modelBusy ? <span className="sr-only">处理中</span> : null}
                        </div>
                      </div>
                    )
                  })}
                </div>
              ) : null}
            </div>
          )
        })}
      </div>

      {editingProvider ? (
        <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-provider-dialog-title" tabIndex={-1} className="soft-modal-shell w-full max-w-xl outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeDialog() }}>
            <div className="soft-modal-header flex items-start justify-between gap-3 border-b px-4 py-3">
              <div><p id="ai-provider-dialog-title" className="text-base font-black text-foreground">{editingProvider === 'new' ? '添加 Provider' : '编辑 Provider'}</p><p className="mt-1 text-xs font-semibold text-muted-foreground">OpenAI Chat Completions 兼容连接</p></div>
              <button type="button" onClick={closeDialog} title="关闭" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground"><X size={16} aria-hidden="true" /><span className="sr-only">关闭</span></button>
            </div>
            <div className="grid gap-4 px-4 py-4 sm:grid-cols-2">
              <label className="text-sm font-black text-foreground sm:col-span-2">Provider 名称<input autoFocus value={providerDraft.name} maxLength={191} onChange={(event) => setProviderDraft((current) => ({ ...current, name: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 text-sm font-semibold" placeholder="内部模型服务" /></label>
              <label className="text-sm font-black text-foreground sm:col-span-2">Base URL<input value={providerDraft.baseURL} maxLength={2048} onChange={(event) => setProviderDraft((current) => ({ ...current, baseURL: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 font-mono text-sm font-semibold" placeholder="https://api.example.com/v1" /></label>
              <label className="text-sm font-black text-foreground sm:col-span-2">API Key<input type="password" autoComplete="new-password" value={providerDraft.apiKey} maxLength={16 * 1024} disabled={providerDraft.clearAPIKey} onChange={(event) => setProviderDraft((current) => ({ ...current, apiKey: event.target.value }))} className="soft-input mt-2 min-h-10 w-full px-3 font-mono text-sm font-semibold disabled:opacity-50" placeholder={editingProvider === 'new' ? '可留空' : '留空保留现有 Key'} /></label>
              {editingProvider !== 'new' && editingProvider.has_api_key ? <label className="flex items-center gap-2 text-sm font-bold text-muted-foreground sm:col-span-2"><input type="checkbox" checked={providerDraft.clearAPIKey} onChange={(event) => setProviderDraft((current) => ({ ...current, clearAPIKey: event.target.checked, apiKey: event.target.checked ? '' : current.apiKey }))} className="h-4 w-4 accent-primary" />清除当前 API Key</label> : null}
              <label className="flex items-center gap-2 text-sm font-bold text-muted-foreground sm:col-span-2"><input type="checkbox" checked={providerDraft.enabled} onChange={(event) => setProviderDraft((current) => ({ ...current, enabled: event.target.checked }))} className="h-4 w-4 accent-primary" />启用 Provider</label>
              <div className="flex gap-3 border border-info/30 bg-info/10 px-3 py-3 text-xs font-semibold leading-5 text-info sm:col-span-2"><KeyRound className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><p>保存后 API Key 不会再次显示。留空会保留现有 Key；修改连接或凭据会清除发现与模型能力状态。</p></div>
            </div>
            <div className="soft-modal-footer flex flex-wrap justify-end gap-2 border-t px-4 py-3"><button type="button" onClick={closeDialog} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button><button type="button" onClick={() => void saveProvider(true)} disabled={!providerDraft.name.trim() || !providerDraft.baseURL.trim() || Boolean(busyID)} className="soft-button inline-flex min-h-10 items-center gap-2 border border-border bg-card px-4 text-sm font-black text-foreground disabled:opacity-50"><Download size={16} aria-hidden="true" />保存并获取模型</button><button type="button" onClick={() => void saveProvider()} disabled={!providerDraft.name.trim() || !providerDraft.baseURL.trim() || Boolean(busyID)} className="soft-button min-h-10 bg-primary px-4 text-sm font-black text-primary-foreground disabled:opacity-50">{busyID ? '保存中...' : '保存 Provider'}</button></div>
          </div>
        </div>
      ) : null}

      {editingModel ? (
        <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-model-dialog-title" tabIndex={-1} className="soft-modal-shell w-full max-w-lg outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeDialog() }}>
            <div className="soft-modal-header flex items-start justify-between gap-3 border-b px-4 py-3"><div><p id="ai-model-dialog-title" className="text-base font-black text-foreground">{editingModel.model ? '编辑模型' : '手动添加模型'}</p><p className="mt-1 truncate text-xs font-semibold text-muted-foreground">{editingModel.provider.name}</p></div><button type="button" onClick={closeDialog} title="关闭" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground"><X size={16} aria-hidden="true" /><span className="sr-only">关闭</span></button></div>
            <div className="grid gap-4 px-4 py-4">
              <label className="text-sm font-black text-foreground">上游模型 ID<input autoFocus value={editingModel.modelID} maxLength={255} onChange={(event) => setEditingModel((current) => current ? { ...current, modelID: event.target.value } : current)} className="soft-input mt-2 min-h-10 w-full px-3 font-mono text-sm font-semibold" placeholder="gpt-5.1" /></label>
              <label className="text-sm font-black text-foreground">显示名称（可选）<input value={editingModel.displayName} maxLength={191} onChange={(event) => setEditingModel((current) => current ? { ...current, displayName: event.target.value } : current)} className="soft-input mt-2 min-h-10 w-full px-3 text-sm font-semibold" placeholder="例如 GPT-5.1" /></label>
              <label className="flex items-center gap-2 text-sm font-bold text-muted-foreground"><input type="checkbox" checked={editingModel.enabled} onChange={(event) => setEditingModel((current) => current ? { ...current, enabled: event.target.checked } : current)} className="h-4 w-4 accent-primary" />启用模型</label>
              <p className="text-xs font-semibold leading-5 text-muted-foreground">添加或修改后需要单独检测 Chat 与 Tools 能力，模型才可用于会话和路由。</p>
            </div>
            <div className="soft-modal-footer flex justify-end gap-2 border-t px-4 py-3"><button type="button" onClick={closeDialog} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button><button type="button" onClick={() => void saveModel()} disabled={!editingModel.modelID.trim() || Boolean(busyID)} className="soft-button min-h-10 bg-primary px-4 text-sm font-black text-primary-foreground disabled:opacity-50">{busyID ? '保存中...' : '保存模型'}</button></div>
          </div>
        </div>
      ) : null}

      {discovery ? (
        <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-discovery-dialog-title" tabIndex={-1} className="soft-modal-shell flex max-h-[min(720px,calc(100vh-32px))] w-full max-w-2xl flex-col outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeDialog() }}>
            <div className="soft-modal-header flex shrink-0 items-start justify-between gap-3 border-b px-4 py-3"><div><p id="ai-discovery-dialog-title" className="text-base font-black text-foreground">导入发现的模型</p><p className="mt-1 truncate text-xs font-semibold text-muted-foreground">{discovery.provider.name} · 最多导入 100 个</p></div><button type="button" onClick={closeDialog} title="关闭" className="soft-button flex h-9 w-9 items-center justify-center border border-border text-muted-foreground"><X size={16} aria-hidden="true" /><span className="sr-only">关闭</span></button></div>
            <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
              <p className="text-xs font-black text-muted-foreground">发现结果</p>
              <div className="mt-2 max-h-72 overflow-y-auto border border-border">
                {discovery.models.length === 0 ? <p className="px-3 py-5 text-center text-sm font-semibold text-muted-foreground">服务没有返回模型，可在下方手动添加。</p> : discovery.models.map((modelID) => {
                  const imported = discovery.provider.models.some((model) => model.model_id === modelID)
                  return <label key={modelID} className="flex items-center gap-3 border-b border-border px-3 py-2.5 text-sm font-semibold last:border-b-0"><input type="checkbox" checked={imported || discovery.selected.includes(modelID)} disabled={imported} onChange={() => toggleDiscoveredModel(modelID)} className="h-4 w-4 shrink-0 accent-primary" /><span className="min-w-0 flex-1 break-all font-mono">{modelID}</span>{imported ? <span className="shrink-0 text-xs font-black text-muted-foreground">已导入</span> : null}</label>
                })}
              </div>
              <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-2">
                <label className="text-sm font-black text-foreground">手动模型 ID<input value={discovery.manualModelID} maxLength={255} onChange={(event) => setDiscovery((current) => current ? { ...current, manualModelID: event.target.value } : current)} className="soft-input mt-2 min-h-10 w-full px-3 font-mono text-sm font-semibold" placeholder="自定义模型 ID" /></label>
                <label className="text-sm font-black text-foreground">显示名称（可选）<input value={discovery.manualDisplayName} maxLength={191} onChange={(event) => setDiscovery((current) => current ? { ...current, manualDisplayName: event.target.value } : current)} className="soft-input mt-2 min-h-10 w-full px-3 text-sm font-semibold" placeholder="模型显示名称" /></label>
              </div>
            </div>
            <div className="soft-modal-footer flex shrink-0 justify-end gap-2 border-t px-4 py-3"><button type="button" onClick={closeDialog} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button><button type="button" onClick={() => void importDiscoveredModels()} disabled={Boolean(busyID)} className="soft-button inline-flex min-h-10 items-center gap-2 bg-primary px-4 text-sm font-black text-primary-foreground disabled:opacity-50">{busyID ? <LoaderCircle size={16} className="animate-spin" aria-hidden="true" /> : <Download size={16} aria-hidden="true" />}导入模型</button></div>
          </div>
        </div>
      ) : null}

      {deleteTarget ? (
        <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
          <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="ai-delete-title" tabIndex={-1} className="soft-modal-shell w-full max-w-md outline-none" onKeyDown={(event) => { if (event.key === 'Escape') closeDialog() }}>
            <div className="soft-modal-header flex items-start gap-3 border-b px-4 py-3"><CircleAlert className="mt-0.5 h-5 w-5 shrink-0 text-danger" aria-hidden="true" /><div><p id="ai-delete-title" className="font-black text-foreground">{deleteTarget.kind === 'provider' ? '删除 Provider' : deleteTarget.kind === 'models' ? '删除已选模型' : '删除模型'}</p><p className="mt-1 break-words text-xs font-semibold text-muted-foreground">{deleteTarget.kind === 'provider' ? `将删除“${deleteTarget.provider.name}”、全部子模型和加密凭据。` : deleteTarget.kind === 'models' ? `将删除已选择的 ${deleteTarget.models.length} 个模型。默认或备用模型不能批量删除。` : `将从“${deleteTarget.provider.name}”删除模型“${deleteTarget.model.display_name || deleteTarget.model.model_id}”。`} 历史消息仍保留执行快照。</p></div></div>
            <div className="soft-modal-footer flex justify-end gap-2 px-4 py-3"><button type="button" onClick={closeDialog} className="soft-button min-h-10 border border-border bg-card px-4 text-sm font-black text-muted-foreground">取消</button><button type="button" onClick={() => void remove()} disabled={Boolean(busyID)} className="soft-button min-h-10 bg-danger px-4 text-sm font-black text-white disabled:opacity-50">{busyID ? '删除中...' : '确认删除'}</button></div>
          </div>
        </div>
      ) : null}

      {toast ? <Toast {...toast} onClose={() => setToast(undefined)} /> : null}
    </section>
  )
}
