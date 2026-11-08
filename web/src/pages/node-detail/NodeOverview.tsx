import { useEffect, useState } from 'react'
import { MetricsChart } from '../../components/MetricsChart'
import { formatBytes, formatPercent, formatSpeed } from '../../lib/format'
import { nodeDisplayStatus } from '../../lib/nodeStatus'
import type { Metric, Node, RangeOption } from '../../types'

type Resource = 'cpu' | 'memory' | 'disk'
const resources = [
  { key: 'cpu', label: 'CPU', field: 'cpu_usage' },
  { key: 'memory', label: '内存', field: 'memory_usage' },
  { key: 'disk', label: '磁盘', field: 'disk_usage' }
] as const

export function NodeOverview({ node, metrics, range, onRangeChange, uptimeText, bootTimeText }: {
  node: Node; metrics: Metric[]; range: RangeOption; onRangeChange: (range: RangeOption) => void; uptimeText: string; bootTimeText: string
}) {
  const [resource, setResource] = useState<Resource>('cpu')
  const current = nodeDisplayStatus(node) === 'online' ? node.latest_metric : undefined
  const selected = resources.find(item => item.key === resource)!
  const chartRange = range === '6h' ? '6h' : '1h'
  const chartMetric = current ?? (metrics.length ? metrics[metrics.length - 1] : node.latest_metric)
  useEffect(() => { if (range !== '1h' && range !== '6h') onRangeChange('1h') }, [range, onRangeChange])
  const load = current ? `1m ${current.load1.toFixed(2)} · 5m ${current.load5.toFixed(2)} · 15m ${current.load15.toFixed(2)}` : '—'
  return <>
    {nodeDisplayStatus(node) !== 'online' && <p className="console-notice" role="status">{node.status !== 'online' ? '主机离线，当前指标不可用。下方曲线为历史记录。' : '当前指标已过期，等待 Agent 上报。下方曲线为历史记录。'}</p>}
    <div className="console-overview-grid">
      <section className="min-w-0" aria-label="资源使用">
        <div className="console-section-heading"><h2>资源使用</h2><span className="ml-auto text-xs text-muted-foreground">{chartRange === '6h' ? '1 小时均值' : '10 分钟均值'}</span><div className="console-range" aria-label="资源时间范围">{(['1h', '6h'] as const).map(value => <button key={value} type="button" aria-pressed={chartRange === value} onClick={() => onRangeChange(value)}>{value}</button>)}</div></div>
        <div className="console-resource-strip">{resources.map(item => <button type="button" key={item.key} aria-pressed={resource === item.key} aria-label={`${item.label} 使用率`} onClick={() => setResource(item.key)}><span>{item.label}</span><strong>{current ? formatPercent(current[item.field]) : '—'}</strong><small>{item.key === 'cpu' ? current?.cpu_cores ? `${current.cpu_cores} 核` : '核心数未知' : item.key === 'memory' ? current ? `${formatBytes(current.memory_used)} / ${formatBytes(current.memory_total)}` : '容量暂无数据' : current ? `${formatBytes(current.disk_used)} / ${formatBytes(current.disk_total)}` : '容量暂无数据'}</small></button>)}</div>
        <MetricsChart bare metrics={current ? [...metrics.filter(metric => metric.created_at !== current.created_at), current] : metrics} dataKey={selected.field} title={`${selected.label} 使用率`} color="rgb(var(--chart-cpu))" range={chartRange} />
      </section>
      <section className="console-system-info" aria-label="基础信息">
        <div className="console-section-heading"><h2>基础信息</h2></div>
        <dl>{[['操作系统', node.os || '—'], ['内核版本', node.kernel || '—'], ['架构', node.arch || '—'], ['启动时间', bootTimeText], ['运行时间', uptimeText], ['系统负载', load], ['Agent 版本', node.agent_version || '—'], ['分组', node.group?.name || '未分组']].map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
      </section>
    </div>
    <div data-testid="node-detail-charts" className="console-secondary-charts">
      <MetricsChart metrics={metrics} title="网络 I/O" color="rgb(var(--chart-network-in))" unitLabel="bytes/s" domain={[0, 'auto']} summaryItems={[{ label: '上行', value: chartMetric ? formatSpeed(chartMetric.tx_speed) : '—' }, { label: '下行', value: chartMetric ? formatSpeed(chartMetric.rx_speed) : '—' }]} range={chartRange} series={[{ dataKey: 'rx_speed', label: '下行', color: 'rgb(var(--chart-network-in))', unitLabel: 'bytes/s' }, { dataKey: 'tx_speed', label: '上行', color: 'rgb(var(--chart-network-out))', unitLabel: 'bytes/s' }]} />
      <MetricsChart metrics={metrics} title="磁盘 I/O" color="rgb(var(--chart-disk))" unitLabel="bytes/s" domain={[0, 'auto']} summaryItems={[{ label: '读', value: chartMetric ? formatSpeed(chartMetric.disk_read_speed) : '—' }, { label: '写', value: chartMetric ? formatSpeed(chartMetric.disk_write_speed) : '—' }]} range={chartRange} series={[{ dataKey: 'disk_read_speed', label: '读', color: 'rgb(var(--chart-network-in))', unitLabel: 'bytes/s' }, { dataKey: 'disk_write_speed', label: '写', color: 'rgb(var(--chart-network-out))', unitLabel: 'bytes/s' }]} emptyText="当前 Agent 暂未上报磁盘 I/O 指标" />
      <MetricsChart metrics={metrics} title="系统负载" color="rgb(var(--chart-load))" unitLabel="load" domain={[0, 'auto']} summaryItems={[{ label: '1m', value: chartMetric ? chartMetric.load1.toFixed(2) : '—' }, { label: '5m', value: chartMetric ? chartMetric.load5.toFixed(2) : '—' }, { label: '15m', value: chartMetric ? chartMetric.load15.toFixed(2) : '—' }]} range={chartRange} series={[{ dataKey: 'load1', label: '1m', color: 'rgb(var(--chart-load))' }, { dataKey: 'load5', label: '5m', color: 'rgb(var(--chart-memory))' }, { dataKey: 'load15', label: '15m', color: 'rgb(var(--chart-network-out))' }]} />
    </div>
  </>
}
