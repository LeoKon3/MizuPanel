import { LoaderCircle } from 'lucide-react'

import type { AIAssistantState } from './useAIAssistantState'
import { findAIModel, isAIModelUsable } from './useAIAssistantState'

export function AIModelSelector({ assistant, idPrefix }: { assistant: AIAssistantState; idPrefix: string }) {
  const selected = findAIModel(assistant.providers, assistant.selectedModelID)
  const selectedProvider = assistant.providers.find((provider) => provider.id === assistant.selectedProviderID)
  const selectedModelVisible = selected?.provider.id === selectedProvider?.id
  const selectedUsable = selected ? isAIModelUsable(selected.provider, selected.model) : false
  const providerID = `${idPrefix}-provider`
  const modelID = `${idPrefix}-model`

  return (
    <div className="grid min-w-0 flex-1 grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)_16px] items-center gap-2">
      <label htmlFor={providerID} className="sr-only">选择供应商</label>
      <select
        id={providerID}
        value={assistant.selectedProviderID}
        disabled={assistant.providers.length === 0 || assistant.selectingModel}
        onChange={(event) => assistant.selectProvider(event.target.value)}
        className="soft-input h-9 min-w-0 truncate px-2 text-xs font-bold disabled:cursor-not-allowed disabled:opacity-60"
      >
        {assistant.providers.length === 0 ? <option value="">未配置供应商</option> : null}
        {assistant.providers.map((provider) => (
          <option key={provider.id} value={provider.id}>
            {provider.name}{provider.enabled ? '' : ' · 已停用'}{provider.models.length === 0 ? ' · 无模型' : ''}
          </option>
        ))}
      </select>
      <label htmlFor={modelID} className="sr-only">选择模型</label>
      <select
        id={modelID}
        value={selectedModelVisible ? assistant.selectedModelID : ''}
        disabled={!selectedProvider || selectedProvider.models.length === 0 || assistant.selectingModel}
        onChange={(event) => void assistant.selectModel(event.target.value)}
        className="soft-input h-9 min-w-0 truncate px-2 text-xs font-bold disabled:cursor-not-allowed disabled:opacity-60"
      >
        {!selectedProvider || selectedProvider.models.length === 0 ? <option value="">未配置模型</option> : null}
        {selectedProvider?.models.length && !selectedModelVisible ? <option value="">请选择模型</option> : null}
        {selectedProvider?.models.map((model) => {
          const usable = isAIModelUsable(selectedProvider, model)
          const markers = [model.is_default ? '默认' : '', model.is_fallback ? '备用' : ''].filter(Boolean).join(' / ')
          const unavailable = selectedProvider.enabled && model.enabled ? '未通过检测' : '已停用'
          return (
            <option key={model.id} value={model.id} disabled={!usable}>
              {model.display_name || model.model_id}{markers ? ` · ${markers}` : ''}{usable ? '' : ` · ${unavailable}`}
            </option>
          )
        })}
      </select>
      <span className="flex h-4 w-4 shrink-0 items-center justify-center">
        {assistant.selectingModel
          ? <LoaderCircle className="h-4 w-4 animate-spin text-muted-foreground" aria-label="正在切换模型" />
          : <span className={`h-2.5 w-2.5 rounded-full ${selectedUsable ? 'bg-success' : 'bg-warning'}`} title={selectedUsable ? '当前模型可用' : '当前模型不可用'} aria-label={selectedUsable ? '当前模型可用' : '当前模型不可用'} />}
      </span>
    </div>
  )
}
