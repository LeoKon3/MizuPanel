import { useEffect, useState } from 'react'
import { AlertRulesTab } from './AlertRulesTab'
import { AlertHistoryTab } from './AlertHistoryTab'
import type { Node } from '../types'

type AlertsPageProps = {
  nodes: Node[]
  navigationVersion?: number
}

type TabType = 'history' | 'rules'

export function AlertsPage({ nodes, navigationVersion }: AlertsPageProps) {
  const [activeTab, setActiveTab] = useState<TabType>('history')


  return (
    <div className="space-y-4">
      {/* 页面标题和操作按钮 */}
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-black text-foreground">告警</h1>
      </div>

      {/* Tab 标签切换 */}
      <div className="border-b border-border">
        <div className="flex gap-6">
          <button
            type="button"
            onClick={() => setActiveTab('history')}
            className={`relative pb-3 text-sm font-semibold transition ${
              activeTab === 'history'
                ? 'text-primary'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            告警列表
            {activeTab === 'history' && (
              <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-primary" />
            )}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('rules')}
            className={`relative pb-3 text-sm font-semibold transition ${
              activeTab === 'rules'
                ? 'text-primary'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            告警规则
            {activeTab === 'rules' && (
              <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-primary" />
            )}
          </button>
        </div>
      </div>

      {/* 标签页内容 */}
      <div>
        {activeTab === 'history' && <AlertHistoryTab nodes={nodes} navigationVersion={navigationVersion} />}
        {activeTab === 'rules' && <AlertRulesTab nodes={nodes} />}
      </div>
    </div>
  )
}
