import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { formatPercent, formatSpeed } from '../lib/format'
import type { Metric } from '../types'

type MetricKey = keyof Pick<Metric, 'cpu_usage' | 'memory_usage' | 'disk_usage' | 'disk_read_speed' | 'disk_write_speed' | 'rx_speed' | 'tx_speed' | 'load1' | 'load5' | 'load15'>
type ChartRange = '1h' | '6h'

type ChartSeries = {
  dataKey: MetricKey
  label: string
  color: string
  unitLabel?: string
}

type ChartSummaryItem = {
  label?: string
  value: string
  color?: string
}

type MetricsChartProps = {
  metrics: Metric[]
  dataKey?: MetricKey
  series?: ChartSeries[]
  summaryItems?: ChartSummaryItem[]
  title: string
  color: string
  unitLabel?: string
  domain?: [number, number | 'auto']
  range?: ChartRange
  onRangeChange?: (range: ChartRange) => void
  emptyText?: string
}

type ChartRow = Record<string, number | string | undefined>

function cssVar(name: string, fallback: string) {
  if (typeof window === 'undefined') return fallback
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value ? `rgb(${value})` : fallback
}

const chartVarByKey: Partial<Record<MetricKey, string>> = {
  cpu_usage: '--chart-cpu',
  memory_usage: '--chart-memory',
  disk_usage: '--chart-disk',
  disk_read_speed: '--chart-network-out',
  disk_write_speed: '--chart-network-in',
  rx_speed: '--chart-network-in',
  tx_speed: '--chart-network-out',
  load1: '--chart-load',
  load5: '--chart-memory',
  load15: '--chart-network-out'
}

export function MetricsChart({ metrics, dataKey, series, summaryItems = [], title, color, unitLabel = '使用率 %', domain, range, onRangeChange, emptyText = '等待指标数据' }: MetricsChartProps) {
  const chartSeries = series?.length ? series : dataKey ? [{ dataKey, label: title, color, unitLabel }] : []
  const latestMetric = metrics.length > 0 ? metrics[metrics.length - 1] : undefined
  const defaultSummaryItems = summaryItems.length > 0
    ? summaryItems
    : chartSeries.length === 1 && latestMetric
      ? [{ value: formatChartValue(latestMetric[chartSeries[0].dataKey], chartSeries[0].unitLabel || unitLabel) }]
      : []

  const data = buildSampledRows(metrics, chartSeries, range)
  const values = collectChartValues(data, chartSeries)
  const hasChartData = values.length > 0
  const isPercentChart = chartSeries.every((item) => (item.unitLabel || unitLabel).includes('%'))
  const yDomain = getYAxisDomain(values, { isPercentChart, domain })
  const yTicks = buildYAxisTicks(yDomain)
  const gridColor = cssVar('--chart-grid', '#e2e8f0')
  const tooltipBackground = cssVar('--chart-tooltip', '#ffffff')
  const tooltipText = cssVar('--foreground', '#0f172a')
  const tooltipBorder = cssVar('--border', '#e2e8f0')
  const tickColor = cssVar('--muted-foreground', '#64748b')

  return (
    <section aria-label={title} className="rounded-[14px] border border-border bg-card p-3 shadow-sm">
      <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="font-display text-base font-black text-foreground">{title}</h3>
        </div>
        {range && onRangeChange ? (
          <div className="flex rounded-xl border border-border bg-surface p-1" aria-label={`${title} 时间范围`}>
            {(['1h', '6h'] as const).map((item) => (
              <button
                key={item}
                type="button"
                aria-pressed={range === item}
                onClick={() => onRangeChange(item)}
                className={`min-h-7 cursor-pointer rounded-lg px-2.5 text-[11px] font-black transition focus:outline-none focus:ring-4 focus:ring-primary/20 ${range === item ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-card hover:text-foreground'}`}
              >
                {item}
              </button>
            ))}
          </div>
        ) : null}
      </div>
      <div className="mb-2 flex min-h-10 flex-wrap items-end gap-x-4 gap-y-1">
        {defaultSummaryItems.length > 0 ? defaultSummaryItems.map((item, index) => (
          <div key={`${item.label || 'summary'}-${index}`} className="min-w-0">
            {item.label ? <p className="text-[11px] font-black text-muted-foreground">{item.label}</p> : null}
            <p className={`${defaultSummaryItems.length === 1 ? 'text-[32px]' : 'text-sm'} font-black leading-none text-foreground`} style={item.color ? { color: item.color } : undefined}>{item.value}</p>
          </div>
        )) : chartSeries.length === 1 ? (
          <span className="rounded-full border border-border bg-muted px-2.5 py-1 text-xs font-black text-muted-foreground">{chartSeries[0].unitLabel || unitLabel}</span>
        ) : chartSeries.map((item) => (
          <span key={item.dataKey} className="inline-flex items-center gap-2 rounded-full border border-border bg-muted px-2.5 py-1 text-xs font-black text-muted-foreground">
            <span className="h-2 w-2 rounded-full" style={{ backgroundColor: cssVar(chartVarByKey[item.dataKey] ?? '', item.color) }} />
            {item.label}
          </span>
        ))}
      </div>
      <div className="h-40">
        {data.length === 0 || chartSeries.length === 0 || !hasChartData ? (
          <div className="flex h-full items-center justify-center rounded-xl border border-dashed border-border bg-surface px-4 text-center text-sm font-black text-muted-foreground">
            {emptyText}
          </div>
        ) : (
          <ResponsiveContainer
            width="100%"
            height="100%"
            initialDimension={{ width: 320, height: 160 }}
          >
            <AreaChart data={data} margin={{ left: 0, right: 8, top: 8, bottom: 0 }}>
              <defs>
                {chartSeries.map((item) => {
                  const strokeColor = cssVar(chartVarByKey[item.dataKey] ?? '', item.color)
                  const gradientId = getGradientId(title, item.dataKey)
                  return (
                    <linearGradient key={item.dataKey} id={gradientId} x1="0" x2="0" y1="0" y2="1">
                      <stop offset="5%" stopColor={strokeColor} stopOpacity={0.3} />
                      <stop offset="95%" stopColor={strokeColor} stopOpacity={0.04} />
                    </linearGradient>
                  )
                })}
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke={gridColor} vertical={false} />
              <XAxis
                dataKey="time"
                stroke={tickColor}
                style={{ fontSize: '11px', fontWeight: 600 }}
                tickLine={false}
                minTickGap={40}
              />
              <YAxis
                stroke={tickColor}
                style={{ fontSize: '11px', fontWeight: 600 }}
                tickLine={false}
                axisLine={false}
                domain={yDomain}
                ticks={yTicks}
                tickFormatter={(value) => formatYAxisTick(Number(value), unitLabel, isPercentChart)}
                width={40}
              />
              <Tooltip
                contentStyle={{
                  borderRadius: 8,
                  border: `1px solid ${tooltipBorder}`,
                  background: tooltipBackground,
                  color: tooltipText,
                  boxShadow: '0 4px 12px rgb(0 0 0 / 0.1)',
                  fontSize: 12,
                  padding: '6px 10px',
                  fontWeight: 600
                }}
                formatter={(value, name) => {
                  const numericValue = typeof value === 'number' ? value : Number(value ?? 0)
                  const label = String(name)
                  return [formatTooltipValue(numericValue, chartSeries.find((item) => item.label === label)?.unitLabel || unitLabel), label]
                }}
              />
              {chartSeries.map((item) => {
                const strokeColor = cssVar(chartVarByKey[item.dataKey] ?? '', item.color)
                return (
                  <Area
                    key={item.dataKey}
                    type="monotone"
                    dataKey={item.dataKey}
                    name={item.label}
                    stroke={strokeColor}
                    strokeWidth={2}
                    fill={`url(#${getGradientId(title, item.dataKey)})`}
                    fillOpacity={1}
                    dot={false}
                    activeDot={{ r: 4 }}
                    isAnimationActive={false}
                  />
                )
              })}
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </section>
  )
}

function buildSampledRows(metrics: Metric[], chartSeries: ChartSeries[], range?: ChartRange) {
  const sortedMetrics = [...metrics].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
  const bucketMs = getBucketMs(sortedMetrics, range)
  if (!bucketMs) return sortedMetrics.map((metric) => buildRow(metric, chartSeries))

  const bucketMap = new Map<number, Partial<Record<MetricKey, { sum: number; count: number }>>>()
  sortedMetrics.forEach((metric) => {
    const timestamp = new Date(metric.created_at).getTime()
    if (!Number.isFinite(timestamp)) return
    const bucket = Math.floor(timestamp / bucketMs) * bucketMs
    if (!bucketMap.has(bucket)) bucketMap.set(bucket, {})
    const entry = bucketMap.get(bucket)!
    chartSeries.forEach((item) => {
      const value = metric[item.dataKey]
      if (typeof value !== 'number' || !Number.isFinite(value)) return
      const stat = entry[item.dataKey] || { sum: 0, count: 0 }
      stat.sum += value
      stat.count += 1
      entry[item.dataKey] = stat
    })
  })

  return Array.from(bucketMap.entries())
    .sort(([a], [b]) => a - b)
    .map(([bucket, entry]) => {
      const row: ChartRow = {
        time: new Date(bucket).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      }
      chartSeries.forEach((item) => {
        const stat = entry[item.dataKey]
        row[item.dataKey] = stat && stat.count > 0 ? stat.sum / stat.count : undefined
      })
      return row
    })
}

function buildRow(metric: Metric, chartSeries: ChartSeries[]) {
  const row: ChartRow = {
    time: new Date(metric.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  chartSeries.forEach((item) => {
    const value = metric[item.dataKey]
    row[item.dataKey] = typeof value === 'number' && Number.isFinite(value) ? value : undefined
  })
  return row
}

function getBucketMs(metrics: Metric[], range?: ChartRange) {
  if (range === '1h') return 10 * 60 * 1000
  if (range === '6h') return 60 * 60 * 1000
  if (metrics.length < 2) return undefined

  const start = new Date(metrics[0].created_at).getTime()
  const end = new Date(metrics[metrics.length - 1].created_at).getTime()
  const span = end - start
  if (!Number.isFinite(span) || span <= 0) return undefined
  if (span <= 90 * 60 * 1000) return 10 * 60 * 1000
  if (span <= 7 * 60 * 60 * 1000) return 60 * 60 * 1000
  return undefined
}

function collectChartValues(data: ChartRow[], chartSeries: ChartSeries[]) {
  const values: number[] = []
  data.forEach((row) => {
    chartSeries.forEach((item) => {
      const value = row[item.dataKey]
      if (typeof value === 'number' && Number.isFinite(value)) values.push(value)
    })
  })
  return values
}

function getYAxisDomain(values: number[], { isPercentChart, domain }: { isPercentChart: boolean; domain?: [number, number | 'auto'] }): [number, number] {
  if (values.length === 0) return isPercentChart ? [0, 100] : [0, 1]

  const minValue = Math.min(...values)
  const maxValue = Math.max(...values)
  const valueRange = maxValue - minValue
  const clampMin = domain?.[0] ?? 0
  const explicitMax = typeof domain?.[1] === 'number' ? domain[1] : undefined

  if (explicitMax !== undefined) return [clampMin, explicitMax]

  if (isPercentChart && valueRange < 5) {
    const center = (minValue + maxValue) / 2
    return [Math.max(0, Math.floor(center - 10)), Math.min(100, Math.ceil(center + 10))]
  }

  const padding = Math.max(isPercentChart ? 5 : valueRange * 0.1, valueRange * 0.1)
  const lower = Math.max(clampMin, Math.floor(minValue - padding))
  let upper = Math.ceil(maxValue + padding)
  if (isPercentChart) upper = Math.min(100, upper)
  if (upper <= lower) upper = lower + (isPercentChart ? 10 : Math.max(1, Math.ceil(maxValue || 1)))
  return [lower, upper]
}

function buildYAxisTicks(domain: [number, number]) {
  const [min, max] = domain
  const span = max - min
  return [
    min,
    min + span * 0.25,
    min + span * 0.5,
    min + span * 0.75,
    max
  ].map((value) => Math.round(value * 100) / 100)
}

function formatYAxisTick(value: number, unitLabel: string, isPercentChart: boolean) {
  if (isPercentChart) return `${Math.round(value)}%`
  if (unitLabel.includes('bytes/s')) return formatSpeed(value)
  return Number.isInteger(value) ? String(value) : value.toFixed(1)
}

function formatTooltipValue(value: number, unitLabel: string) {
  if (unitLabel.includes('bytes/s')) return formatSpeed(value)
  if (unitLabel.includes('%')) return formatPercent(value)
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(2) : '—'
}

function getGradientId(title: string, dataKey: MetricKey) {
  return `${title}-${String(dataKey)}-gradient`.replace(/[^a-zA-Z0-9_-]/g, '-')
}

function formatChartValue(value: number | undefined, unitLabel: string) {
  if (unitLabel.includes('bytes/s')) return formatSpeed(value)
  if (unitLabel.includes('%')) return formatPercent(value)
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(2) : '—'
}
