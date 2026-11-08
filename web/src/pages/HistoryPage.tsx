import { MetricCard } from '../components/MetricCard'
import { MetricsChart } from '../components/MetricsChart'
import { formatPercent, formatSpeed } from '../lib/format'
import type { Metric, Node, RangeOption, SettingsResponse } from '../types'

const rangeOptions: RangeOption[] = ['1h', '6h', '24h', '3d', '7d']
const rangeSeconds: Record<RangeOption, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
  '3d': 259200,
  '7d': 604800
}

export function HistoryPage({ nodes, selectedNodeID, metrics, range, settings, onSelectNode, onRangeChange }: { nodes: Node[], selectedNodeID?: string, metrics: Metric[], range: RangeOption, settings?: SettingsResponse, onSelectNode: (nodeID: string) => void, onRangeChange: (range: RangeOption) => void }) {
  const selectedNode = nodes.find((node) => node.id === selectedNodeID) ?? nodes[0]
  const latest = metrics.length > 0 ? metrics[metrics.length - 1] : undefined
  const retention = settings?.metrics_retention ?? '6h'
  const retentionSeconds = settings?.metrics_retention_seconds ?? rangeSeconds['6h']

  return (
    <section className="space-y-4" aria-label="历史记录">
      <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-glass">
        <div className="border-b border-border bg-surface px-5 py-5">
          <h2 className="font-display text-3xl font-black tracking-tight text-foreground">指标历史记录</h2>
        </div>

        <div className="grid gap-4 p-4 xl:grid-cols-[0.34fr_0.66fr]">
          <aside className="rounded-2xl border border-border bg-surface p-3">
            <p className="mb-3 text-xs font-black tracking-[0.18em] text-muted-foreground">选择节点</p>
            <div className="space-y-2">
              {nodes.map((node) => (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => onSelectNode(node.id)}
                  className={`w-full rounded-xl border px-4 py-3 text-left transition focus:outline-none focus:ring-4 focus:ring-primary/20 ${selectedNode?.id === node.id ? 'border-primary/40 bg-card shadow-sm' : 'border-transparent bg-transparent hover:bg-card'}`}
                >
                  <span className="block truncate text-sm font-black text-foreground">{node.name || node.hostname}</span>
                  <span className="mt-1 block truncate text-xs font-bold text-muted-foreground">{node.ip || '未知 IP'} · {node.status === 'online' ? '在线' : '离线'}</span>
                </button>
              ))}
            </div>
          </aside>

          <div className="min-w-0 space-y-4">
            <div className="flex flex-col gap-3 rounded-2xl border border-border bg-card p-4 lg:flex-row lg:items-center lg:justify-between">
              <h3 className="text-2xl font-black text-foreground">{selectedNode ? selectedNode.name || selectedNode.hostname : '暂无节点'}</h3>
              <div className="flex flex-wrap gap-2 rounded-xl border border-border bg-surface p-1">
                {rangeOptions.map((option) => {
                  const disabled = rangeSeconds[option] > retentionSeconds
                  return (
                    <button
                      key={option}
                      type="button"
                      disabled={disabled}
                      title={disabled ? `当前保留时间最多支持${rangeLabel(retention)}` : undefined}
                      onClick={() => onRangeChange(option)}
                      className={`min-h-10 rounded-lg px-3 text-xs font-black transition focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:text-muted-foreground/40 ${range === option ? 'bg-primary text-primary-foreground shadow-sm disabled:bg-muted disabled:text-muted-foreground' : 'text-muted-foreground hover:bg-card hover:text-foreground'}`}
                    >
                      {rangeButtonLabel(option)}
                    </button>
                  )
                })}
              </div>
            </div>

            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <MetricCard label="CPU" value={latest ? formatPercent(latest.cpu_usage) : '—'} detail="当前范围最新采样" />
              <MetricCard label="内存" value={latest ? formatPercent(latest.memory_usage) : '—'} tone="green" detail={`${metrics.length} 个采样点`} />
              <MetricCard label="磁盘" value={latest ? formatPercent(latest.disk_usage) : '—'} tone="amber" detail="容量使用率" />
              <MetricCard label="网络" value={latest ? formatSpeed(latest.rx_speed) : '—'} tone="slate" detail={latest ? `上行 ${formatSpeed(latest.tx_speed)}` : '等待指标'} />
            </div>

            {metrics.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-border bg-surface px-5 py-8 text-center text-sm font-bold text-muted-foreground">当前范围暂无指标数据；如果刚把保留时间调长，需要等待新数据继续积累。</div>
            ) : (
              <div className="grid gap-3 xl:grid-cols-2">
                <MetricsChart metrics={metrics} dataKey="cpu_usage" title="CPU 历史" color="#2563eb" />
                <MetricsChart metrics={metrics} dataKey="memory_usage" title="内存历史" color="#059669" />
                <MetricsChart metrics={metrics} dataKey="disk_usage" title="磁盘历史" color="#d97706" />
                <MetricsChart metrics={metrics} dataKey="load1" title="Load 走势" color="#7c3aed" unitLabel="load" domain={[0, 'auto']} />
                <MetricsChart metrics={metrics} dataKey="rx_speed" title="下行速率" color="#0284c7" unitLabel="bytes/s" domain={[0, 'auto']} />
                <MetricsChart metrics={metrics} dataKey="tx_speed" title="上行速率" color="#dc2626" unitLabel="bytes/s" domain={[0, 'auto']} />
              </div>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}

export function rangeButtonLabel(range: RangeOption) {
  if (range === '1h') return '最近 1 小时'
  if (range === '6h') return '最近 6 小时'
  if (range === '24h') return '最近 24 小时'
  if (range === '3d') return '最近 3 天'
  return '最近 7 天'
}

function rangeLabel(range: RangeOption) {
  if (range === '1h') return '1 小时'
  if (range === '6h') return '6 小时'
  if (range === '24h') return '24 小时'
  if (range === '3d') return '3 天'
  return '7 天'
}
