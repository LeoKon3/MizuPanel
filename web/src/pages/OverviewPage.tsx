import { useEffect, useState } from 'react'
import type { AlertHistory, K8sCluster, Metric, Node, RangeOption } from '../types'
import { formatBytes, formatPercent, formatUptime } from '../lib/format'
import { getAlertHistory, getNodeMetrics, getK8sClusters, getAlertRules } from '../api/client'
import { Plus, Bell, Settings, Server, Box, Cpu, MemoryStick, HardDrive, Monitor, Zap, Home, Clock } from 'lucide-react'
import { LineChart, Line, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid, Area, AreaChart } from 'recharts'

type OverviewPageProps = {
  nodes: Node[]
  onlineNodes: number
  onAddServer: () => void
}

type MetricType = 'cpu' | 'memory' | 'disk'

export function OverviewPage({ nodes, onlineNodes, onAddServer }: OverviewPageProps) {
  const [k8sClusters, setK8sClusters] = useState<K8sCluster[]>([])
  const [alertHistory, setAlertHistory] = useState<AlertHistory[]>([])
  const [alertRulesCount, setAlertRulesCount] = useState(0)
  const [selectedNodeID, setSelectedNodeID] = useState<string>()
  const [selectedMetric, setSelectedMetric] = useState<MetricType>('cpu')
  const [timeRange, setTimeRange] = useState<RangeOption>('1h')
  const [avgMetricsHistory, setAvgMetricsHistory] = useState<Array<{
    timestamp: string
    cpu: number
    memory: number
    disk: number
  }>>([])
  const [metricsLoading, setMetricsLoading] = useState(false)

  // 加载 K8s 集群数据
  useEffect(() => {
    getK8sClusters()
      .then(response => setK8sClusters(response.clusters))
      .catch(() => setK8sClusters([]))
  }, [])

  // 加载告警规则数量
  useEffect(() => {
    getAlertRules()
      .then(response => setAlertRulesCount((response.rules || []).length))
      .catch(() => setAlertRulesCount(0))
  }, [])

  // 自动选择第一个在线节点
  useEffect(() => {
    if (!selectedNodeID && nodes.length > 0) {
      const firstOnline = nodes.find((n) => n.status === 'online')
      setSelectedNodeID(firstOnline?.id || nodes[0].id)
    }
  }, [nodes, selectedNodeID])

  // 加载告警历史（最近的未解决告警）
  const loadAlertHistory = () => {
    if (nodes.length === 0) return

    // 获取所有节点的告警历史
    Promise.all(
      nodes.map((node) =>
        getAlertHistory(node.id, 10).catch(() => ({ history: [] }))
      )
    ).then((results) => {
      const allHistory = results.flatMap((r) => r.history)
      // 只显示未解决的告警，按触发时间倒序
      const unresolved = allHistory
        .filter((h) => !h.resolved_at)
        .sort((a, b) => new Date(b.triggered_at).getTime() - new Date(a.triggered_at).getTime())
        .slice(0, 10)
      setAlertHistory(unresolved)
    })
  }

  // 初始加载告警
  useEffect(() => {
    loadAlertHistory()
  }, [nodes])

  // 定时刷新活跃告警（每30秒）
  useEffect(() => {
    const interval = setInterval(() => {
      loadAlertHistory()
    }, 30000)

    return () => clearInterval(interval)
  }, [nodes])

  // 加载所有在线节点的历史指标并计算平均值
  useEffect(() => {
    const onlineNodes = nodes.filter(n => n.status === 'online')
    if (onlineNodes.length === 0) {
      setAvgMetricsHistory([])
      return
    }

    setMetricsLoading(true)

    // 获取所有在线节点的历史数据
    Promise.all(
      onlineNodes.map(node =>
        getNodeMetrics(node.id, timeRange).catch(() => ({ metrics: [] }))
      )
    ).then((results) => {
      // 创建时间戳到指标的映射
      const timestampMap = new Map<string, { cpu: number[], memory: number[], disk: number[] }>()

      results.forEach(result => {
        result.metrics.forEach(metric => {
          const ts = metric.created_at
          if (!timestampMap.has(ts)) {
            timestampMap.set(ts, { cpu: [], memory: [], disk: [] })
          }
          const entry = timestampMap.get(ts)!
          entry.cpu.push(metric.cpu_usage)
          entry.memory.push(metric.memory_usage)
          entry.disk.push(metric.disk_usage)
        })
      })

      // 计算每个时间点的平均值
      let avgHistory = Array.from(timestampMap.entries())
        .map(([timestamp, values]) => ({
          timestamp,
          cpu: values.cpu.reduce((a, b) => a + b, 0) / values.cpu.length,
          memory: values.memory.reduce((a, b) => a + b, 0) / values.memory.length,
          disk: values.disk.reduce((a, b) => a + b, 0) / values.disk.length,
        }))
        .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())

      // 对于 1 小时范围，按 10 分钟间隔分组并保留每组的真实值
      if (timeRange === '1h' && avgHistory.length > 0) {
        const sampledHistory: typeof avgHistory = []
        const tenMinutes = 10 * 60 * 1000

        // 按 10 分钟区间分组
        const groups = new Map<number, typeof avgHistory>()

        avgHistory.forEach(item => {
          const time = new Date(item.timestamp).getTime()
          const bucketKey = Math.floor(time / tenMinutes) * tenMinutes

          if (!groups.has(bucketKey)) {
            groups.set(bucketKey, [])
          }
          groups.get(bucketKey)!.push(item)
        })

        // 从每组中选择中间的数据点，保留真实波动
        Array.from(groups.entries())
          .sort((a, b) => a[0] - b[0])
          .forEach(([bucketKey, items]) => {
            // 使用组内中位数索引的数据点
            const midIndex = Math.floor(items.length / 2)
            const selectedItem = items[midIndex]

            // 使用原始时间戳，但格式化为显示用的整点时间
            const displayTime = new Date(bucketKey)
            displayTime.setMinutes(Math.floor(displayTime.getMinutes() / 10) * 10, 0, 0)

            sampledHistory.push({
              timestamp: displayTime.toISOString(),
              cpu: selectedItem.cpu,
              memory: selectedItem.memory,
              disk: selectedItem.disk,
            })
          })

        avgHistory = sampledHistory
      }

      setAvgMetricsHistory(avgHistory)
      setMetricsLoading(false)
    }).catch(() => {
      setMetricsLoading(false)
    })
  }, [nodes, timeRange])

  const onlineClustersCount = k8sClusters.filter(c => c.status === 'online').length
  const totalClustersCount = k8sClusters.length
  const activeAlertsCount = alertHistory.length

  // 计算所有在线节点的平均资源使用率
  const onlineNodesWithMetrics = nodes.filter(n => n.status === 'online' && n.latest_metric)

  const selectedNode = nodes.find((n) => n.id === selectedNodeID)
  const selectedMetrics = selectedNode?.latest_metric

  // 服务器信息（显示第一个节点作为示例）
  const serverNode = nodes[0]

  return (
    <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4">
      {/* 顶部统计卡片 */}
      <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={<Server size={20} />}
          title="节点状态"
          value={`${onlineNodes}/${nodes.length}`}
          subtitle={`${onlineNodes} 在线`}
          tone="blue"
        />
        <StatCard
          icon={<Box size={20} />}
          title="K8s 集群"
          value={`${onlineClustersCount}/${totalClustersCount}`}
          subtitle={totalClustersCount === 0 ? '暂无集群' : `${onlineClustersCount} 在线`}
          tone="green"
        />
        <StatCard
          icon={<Bell size={20} />}
          title="活跃告警"
          value={String(activeAlertsCount)}
          subtitle={activeAlertsCount === 0 ? '无告警' : '未解决'}
          tone={activeAlertsCount > 0 ? 'red' : 'gray'}
          iconTone={activeAlertsCount > 0 ? 'red' : 'gray'}
        />
        <StatCard
          icon={<Settings size={20} />}
          title="告警规则"
          value={String(alertRulesCount)}
          subtitle={alertRulesCount === 0 ? '未配置' : '已配置'}
          tone="purple"
        />
      </section>

      {/* 中间区域：资源趋势图 + 服务器状态 */}
      <section className="grid gap-3 lg:grid-cols-[1.5fr_1fr]">
        {/* 左侧：资源使用趋势（折线图） */}
        <div className="soft-panel p-4">
          <p className="mb-2 text-xs font-semibold text-muted-foreground">资源使用趋势</p>
          <div className="flex items-start justify-between gap-3">
            {/* 左上角：三个指标按钮 */}
            <div className="flex gap-2">
              {(['cpu', 'memory', 'disk'] as MetricType[]).map((type) => (
                <button
                  key={type}
                  type="button"
                  onClick={() => setSelectedMetric(type)}
                  className={`min-h-7 cursor-pointer rounded-md px-3 text-xs font-semibold transition focus:outline-none focus:ring-2 focus:ring-primary/20 ${
                    selectedMetric === type
                      ? 'bg-primary text-primary-foreground shadow-sm'
                      : 'border border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground'
                  }`}
                >
                  {type === 'cpu' ? 'CPU' : type === 'memory' ? '内存' : '磁盘'}
                </button>
              ))}
            </div>

            {/* 右上角：时间范围选择 */}
            <div className="flex shrink-0 gap-2">
              <select
                value={timeRange}
                onChange={(e) => setTimeRange(e.target.value as RangeOption)}
                className="soft-input rounded-md px-2 py-1 text-xs font-semibold"
              >
                <option value="1h">1 小时</option>
                <option value="6h">6 小时</option>
                <option value="24h">24 小时</option>
                <option value="3d">3 天</option>
                <option value="7d">7 天</option>
              </select>
            </div>
          </div>

          {/* 趋势图区域 */}
          <div className="mt-3">
            {onlineNodesWithMetrics.length > 0 ? (
              <TrendChart
                nodes={onlineNodesWithMetrics}
                type={selectedMetric}
                range={timeRange}
                history={avgMetricsHistory}
                loading={metricsLoading}
              />
            ) : (
              <div className="soft-empty-state flex h-48 items-center justify-center">
                <p className="text-sm font-semibold text-muted-foreground">暂无在线节点数据</p>
              </div>
            )}
          </div>
        </div>

        {/* 右侧：服务器状态 */}
        <div className="soft-panel p-4">
          <h2 className="mb-3 text-sm font-black uppercase tracking-[0.1em] text-muted-foreground">服务器状态</h2>
          <div className="space-y-2">
            {nodes.length > 0 ? (
              nodes.slice(0, 5).map((node) => (
                <ServerStatusCard key={node.id} node={node} />
              ))
            ) : (
              <div className="soft-empty-state p-8 text-center">
                <p className="text-sm font-black text-muted-foreground">暂无节点</p>
              </div>
            )}
          </div>
        </div>
      </section>

      {/* 底部区域：活跃告警 + 系统信息 + 快捷操作 */}
      <section className="grid gap-3 lg:grid-cols-[40%_30%_30%]">
        {/* 左侧：活跃告警 */}
        <div className="soft-panel p-4">
          <div className="mb-3 flex items-center gap-2">
            <div className={`flex h-8 w-8 items-center justify-center rounded-lg ${
              alertHistory.length > 0 ? 'bg-red-50 text-red-600' : 'bg-muted text-muted-foreground'
            }`}>
              <Bell size={18} />
            </div>
            <h2 className="text-sm font-black uppercase tracking-[0.1em] text-muted-foreground">活跃告警</h2>
          </div>
          <div className="space-y-2">
            {alertHistory.length > 0 ? (
              alertHistory.slice(0, 3).map((alert) => (
                <AlertCard key={alert.id} alert={alert} />
              ))
            ) : (
              <div className="soft-empty-state border-success/30 bg-success/5 p-6 text-center">
                <p className="text-sm font-black text-success">无活跃告警</p>
                <p className="mt-1 text-xs font-semibold text-muted-foreground">所有监控指标正常</p>
              </div>
            )}
          </div>
        </div>

        {/* 中间：系统信息 */}
        <div className="soft-panel p-4">
          <h2 className="mb-3 text-sm font-black uppercase tracking-[0.1em] text-muted-foreground">系统信息</h2>
          {serverNode ? (
            <div className="space-y-2">
              <InfoRow icon={<Monitor size={16} />} label="操作系统" value={`${serverNode.os} ${serverNode.arch}`} />
              <InfoRow icon={<Zap size={16} />} label="内核版本" value={serverNode.kernel || '—'} />
              <InfoRow icon={<Home size={16} />} label="主机名" value={serverNode.hostname || '—'} />
              <InfoRow
                icon={<Clock size={16} />}
                label="运行时间"
                value={
                  serverNode.latest_metric
                    ? formatUptime(serverNode.latest_metric.uptime)
                    : '—'
                }
              />
              <InfoRow
                icon={<MemoryStick size={16} />}
                label="总内存"
                value={
                  serverNode.latest_metric
                    ? formatBytes(serverNode.latest_metric.memory_total)
                    : '—'
                }
              />
            </div>
          ) : (
            <div className="soft-empty-state p-6 text-center">
              <p className="text-sm font-black text-muted-foreground">暂无节点信息</p>
            </div>
          )}
        </div>

        {/* 右侧：快捷操作 */}
        <div className="soft-panel p-4">
          <h2 className="mb-3 text-sm font-black uppercase tracking-[0.1em] text-muted-foreground">快捷操作</h2>
          <div className="grid grid-cols-2 gap-3">
            <QuickActionButton icon={<Plus size={24} />} label="添加服务器" color="blue" onClick={onAddServer} />
            <QuickActionButton icon={<Bell size={24} />} label="告警规则" color="orange" onClick={() => window.location.href = '/alerts'} />
            <QuickActionButton icon={<Settings size={24} />} label="系统设置" color="green" onClick={() => window.location.href = '/settings'} />
            <QuickActionButton icon={<Box size={24} />} label="K8s 集群" color="cyan" onClick={() => window.location.href = '/k8s/clusters'} />
          </div>
        </div>
      </section>
    </div>
  )
}

function StatCard({
  icon,
  title,
  value,
  subtitle,
  tone,
  sparklineData,
  iconTone,
}: {
  icon?: React.ReactNode
  title: string
  value: string
  subtitle: string
  tone: 'blue' | 'green' | 'red' | 'gray' | 'purple' | 'orange' | 'cyan'
  sparklineData?: number[]
  iconTone?: 'blue' | 'green' | 'red' | 'gray' | 'purple' | 'orange' | 'cyan'
}) {
  const actualIconTone = iconTone || tone

  const iconBgClasses = {
    blue: 'bg-primary/10 text-primary',
    green: 'bg-success/10 text-success',
    red: 'bg-danger/10 text-danger',
    gray: 'bg-muted text-muted-foreground',
    purple: 'bg-purple-500/10 text-purple-600',
    orange: 'bg-orange-500/10 text-orange-600',
    cyan: 'bg-cyan-500/10 text-cyan-600',
  }

  const sparklineColors = {
    blue: '#93c5fd',    // blue-300 更柔和
    green: '#6ee7b7',   // emerald-300 更柔和
    red: '#fca5a5',     // red-300 更柔和
    gray: '#d1d5db',    // gray-300
    purple: '#c4b5fd',  // purple-300 更柔和
    orange: '#fdba74',  // orange-300 更柔和
    cyan: '#67e8f9',    // cyan-300 更柔和
  }

  const valueToneClasses = {
    blue: 'text-foreground',
    green: 'text-foreground',
    red: 'text-foreground',
    gray: 'text-foreground',
    purple: 'text-foreground',
    orange: 'text-foreground',
    cyan: 'text-foreground',
  }

  // 准备 sparkline 数据
  const chartData = sparklineData ? sparklineData.map((v, i) => ({ index: i, value: v })) : []

  // 渐变定义
  const gradientId = `gradient-${tone}`

  return (
    <div className="soft-stat-card p-4">
      <div className="flex items-center gap-3">
        <div className={`flex h-12 w-12 shrink-0 items-center justify-center rounded-xl ${iconBgClasses[actualIconTone]}`}>
          {icon}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-xs font-semibold text-muted-foreground">{title}</p>
          <p className={`text-2xl font-black ${valueToneClasses[tone]}`}>{value}</p>
          <p className="text-xs font-medium text-muted-foreground">{subtitle}</p>
        </div>
      </div>
      {sparklineData && sparklineData.length > 0 && (
        <div className="mt-3 h-8">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={chartData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={sparklineColors[tone]} stopOpacity={0.15}/>
                  <stop offset="95%" stopColor={sparklineColors[tone]} stopOpacity={0}/>
                </linearGradient>
              </defs>
              <Area
                type="natural"
                dataKey="value"
                stroke={sparklineColors[tone]}
                strokeWidth={1.2}
                fill={`url(#${gradientId})`}
                dot={false}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}

function MetricDisplay({
  node,
  metric,
  type,
}: {
  node: Node
  metric?: Metric
  type: MetricType
}) {
  if (!metric) {
    return (
      <div className="soft-empty-state p-8 text-center">
        <p className="text-sm font-black text-muted-foreground">暂无指标数据</p>
      </div>
    )
  }

  const getValue = () => {
    switch (type) {
      case 'cpu':
        return formatPercent(metric.cpu_usage)
      case 'memory':
        return formatPercent(metric.memory_usage)
      case 'disk':
        return formatPercent(metric.disk_usage)
    }
  }

  const getDetail = () => {
    switch (type) {
      case 'cpu':
        return `${metric.cpu_cores} 核心 · Load: ${metric.load1.toFixed(2)}`
      case 'memory':
        return `${formatBytes(metric.memory_used)} / ${formatBytes(metric.memory_total)}`
      case 'disk':
        return `${formatBytes(metric.disk_used)} / ${formatBytes(metric.disk_total)}`
    }
  }

  const getColor = () => {
    const value =
      type === 'cpu'
        ? metric.cpu_usage
        : type === 'memory'
        ? metric.memory_usage
        : metric.disk_usage

    if (value >= 90) return 'text-danger'
    if (value >= 70) return 'text-warning'
    return 'text-success'
  }

  return (
    <div className="soft-card p-4">
      <div className="flex items-end justify-between">
        <div>
          <p className="text-xs font-black uppercase tracking-[0.1em] text-muted-foreground">
            当前使用率
          </p>
          <p className={`mt-2 text-4xl font-black ${getColor()}`}>{getValue()}</p>
        </div>
        <div className="text-right">
          <p className="text-xs font-semibold text-muted-foreground">{getDetail()}</p>
        </div>
      </div>

      {/* 简单的使用率进度条 */}
      <div className="mt-4 h-2 overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full transition-all ${
            type === 'cpu'
              ? 'bg-primary'
              : type === 'memory'
              ? 'bg-success'
              : 'bg-warning'
          }`}
          style={{
            width: `${Math.min(
              100,
              type === 'cpu'
                ? metric.cpu_usage
                : type === 'memory'
                ? metric.memory_usage
                : metric.disk_usage
            )}%`,
          }}
        />
      </div>

      <p className="mt-3 text-xs font-semibold text-muted-foreground">
        注：实时趋势图功能待实现，当前显示最新采样值
      </p>
    </div>
  )
}

function TrendChart({
  nodes,
  type,
  range,
  history,
  loading,
}: {
  nodes: Node[]
  type: MetricType
  range: RangeOption
  history: Array<{ timestamp: string; cpu: number; memory: number; disk: number }>
  loading: boolean
}) {
  // 计算所有节点的平均值
  const avgMetric = nodes.length > 0 ? {
    cpu_usage: nodes.reduce((sum, n) => sum + (n.latest_metric?.cpu_usage || 0), 0) / nodes.length,
    cpu_cores: Math.round(nodes.reduce((sum, n) => sum + (n.latest_metric?.cpu_cores || 0), 0) / nodes.length),
    load1: nodes.reduce((sum, n) => sum + (n.latest_metric?.load1 || 0), 0) / nodes.length,
    memory_usage: nodes.reduce((sum, n) => sum + (n.latest_metric?.memory_usage || 0), 0) / nodes.length,
    memory_used: nodes.reduce((sum, n) => sum + (n.latest_metric?.memory_used || 0), 0),
    memory_total: nodes.reduce((sum, n) => sum + (n.latest_metric?.memory_total || 0), 0),
    disk_usage: nodes.reduce((sum, n) => sum + (n.latest_metric?.disk_usage || 0), 0) / nodes.length,
    disk_used: nodes.reduce((sum, n) => sum + (n.latest_metric?.disk_used || 0), 0),
    disk_total: nodes.reduce((sum, n) => sum + (n.latest_metric?.disk_total || 0), 0),
  } : null

  if (!avgMetric) {
    return (
      <div className="soft-empty-state flex h-64 items-center justify-center">
        <p className="text-sm font-black text-muted-foreground">暂无指标数据</p>
      </div>
    )
  }

  const getValue = () => {
    switch (type) {
      case 'cpu':
        return formatPercent(avgMetric.cpu_usage)
      case 'memory':
        return formatPercent(avgMetric.memory_usage)
      case 'disk':
        return formatPercent(avgMetric.disk_usage)
    }
  }

  const getDetail = () => {
    switch (type) {
      case 'cpu':
        return `${avgMetric.cpu_cores} 核心（平均） · Load: ${avgMetric.load1.toFixed(2)}`
      case 'memory':
        return `${formatBytes(avgMetric.memory_used)} / ${formatBytes(avgMetric.memory_total)} （总计）`
      case 'disk':
        return `${formatBytes(avgMetric.disk_used)} / ${formatBytes(avgMetric.disk_total)} （总计）`
    }
  }

  const getColor = () => {
    const value =
      type === 'cpu'
        ? avgMetric.cpu_usage
        : type === 'memory'
        ? avgMetric.memory_usage
        : avgMetric.disk_usage

    if (value >= 90) return 'text-danger'
    if (value >= 70) return 'text-warning'
    return 'text-success'
  }

  const getLineColor = () => {
    if (type === 'cpu') return '#93c5fd' // blue-300 更柔和
    if (type === 'memory') return '#c4b5fd' // purple-300 更柔和
    return '#fcd34d' // amber-300 更柔和
  }

  const getGradientId = () => `gradient-trend-${type}`

  // 准备图表数据 - 只显示当前选中的指标
  const chartData = history.map(item => ({
    timestamp: item.timestamp,
    value: type === 'cpu' ? item.cpu : type === 'memory' ? item.memory : item.disk,
  }))

  // 计算动态 Y 轴范围
  const getYAxisDomain = () => {
    if (chartData.length === 0) return [0, 100]

    const values = chartData.map(d => d.value)
    const minValue = Math.min(...values)
    const maxValue = Math.max(...values)
    const range = maxValue - minValue

    // 如果波动很小（< 5%），扩展范围以显示细节
    if (range < 5) {
      const center = (minValue + maxValue) / 2
      const expandedMin = Math.max(0, center - 10)
      const expandedMax = Math.min(100, center + 10)
      return [Math.floor(expandedMin), Math.ceil(expandedMax)]
    }

    // 正常情况，上下留 10% 边距
    const padding = Math.max(5, range * 0.1)
    return [
      Math.max(0, Math.floor(minValue - padding)),
      Math.min(100, Math.ceil(maxValue + padding))
    ]
  }

  const yDomain = getYAxisDomain()
  const yTicks = [
    yDomain[0],
    Math.floor(yDomain[0] + (yDomain[1] - yDomain[0]) * 0.25),
    Math.floor(yDomain[0] + (yDomain[1] - yDomain[0]) * 0.5),
    Math.floor(yDomain[0] + (yDomain[1] - yDomain[0]) * 0.75),
    yDomain[1]
  ]

  // 格式化X轴时间标签
  const formatXAxis = (timestamp: string) => {
    const date = new Date(timestamp)
    return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }

  return (
    <div>
      {/* 折线图 */}
      <div className="h-48">
        {loading ? (
          <div className="flex h-full items-center justify-center">
            <p className="text-xs font-semibold text-muted-foreground">加载中...</p>
          </div>
        ) : chartData.length > 0 ? (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={chartData} margin={{ top: 5, right: 5, bottom: 5, left: -5 }}>
              <defs>
                <linearGradient id={getGradientId()} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={getLineColor()} stopOpacity={0.2}/>
                  <stop offset="95%" stopColor={getLineColor()} stopOpacity={0}/>
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" opacity={0.2} vertical={false} />
              <XAxis
                dataKey="timestamp"
                tick={{ fontSize: 10, fill: '#9ca3af' }}
                tickFormatter={formatXAxis}
                axisLine={{ stroke: '#e5e7eb', strokeWidth: 1 }}
                tickLine={{ stroke: '#e5e7eb' }}
                minTickGap={40}
              />
              <YAxis
                domain={yDomain}
                ticks={yTicks}
                tick={{ fontSize: 10, fill: '#9ca3af' }}
                tickFormatter={(value) => `${value}%`}
                axisLine={{ stroke: '#e5e7eb', strokeWidth: 1 }}
                tickLine={{ stroke: '#e5e7eb' }}
                width={38}
              />
              <Tooltip
                content={({ active, payload }) => {
                  if (!active || !payload || !payload.length) return null
                  return (
                    <div className="rounded-lg border border-border bg-card px-2.5 py-1.5 shadow-lg">
                      <p className="text-xs font-semibold text-foreground">
                        {formatPercent(payload[0].value as number)}
                      </p>
                      <p className="text-[10px] font-medium text-muted-foreground">
                        {new Date(payload[0].payload.timestamp).toLocaleString('zh-CN', {
                          month: '2-digit',
                          day: '2-digit',
                          hour: '2-digit',
                          minute: '2-digit'
                        })}
                      </p>
                    </div>
                  )
                }}
              />
              <Area
                type="natural"
                dataKey="value"
                stroke={getLineColor()}
                strokeWidth={1.8}
                fill={`url(#${getGradientId()})`}
                dot={false}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        ) : (
          <div className="soft-empty-state flex h-full items-center justify-center">
            <p className="text-xs font-semibold text-muted-foreground">暂无历史数据</p>
          </div>
        )}
      </div>
    </div>
  )
}

function ServerStatusCard({ node }: { node: Node }) {
  const metric = node.latest_metric
  const statusText = node.status === 'online' ? '在线' : '离线'
  const statusColor = node.status === 'online' ? 'bg-success' : 'bg-muted-foreground/40'
  const statusGlow = node.status === 'online' ? 'shadow-[0_0_14px_rgb(var(--success)/0.45)]' : ''

  return (
    <div className="soft-card p-3 shadow-none">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className={`h-2 w-2 rounded-full ${statusColor} ${statusGlow}`} />
            <p className="truncate text-sm font-black text-foreground">{node.name || node.hostname}</p>
          </div>
          <p className="mt-1 truncate text-xs font-semibold text-muted-foreground">{node.ip || '未知 IP'}</p>
        </div>
        <span
          className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-black ${
            node.status === 'online' ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'
          }`}
        >
          {statusText}
        </span>
      </div>
      {metric && (
        <div className="mt-2 grid grid-cols-3 gap-2">
          <div className="rounded-xl border border-border bg-surface/70 px-2 py-1.5">
            <p className="text-[9px] font-black uppercase tracking-[0.05em] text-muted-foreground">CPU</p>
            <p className="mt-0.5 text-xs font-black text-foreground">{formatPercent(metric.cpu_usage)}</p>
          </div>
          <div className="rounded-xl border border-border bg-surface/70 px-2 py-1.5">
            <p className="text-[9px] font-black uppercase tracking-[0.05em] text-muted-foreground">内存</p>
            <p className="mt-0.5 text-xs font-black text-foreground">{formatPercent(metric.memory_usage)}</p>
          </div>
          <div className="rounded-xl border border-border bg-surface/70 px-2 py-1.5">
            <p className="text-[9px] font-black uppercase tracking-[0.05em] text-muted-foreground">磁盘</p>
            <p className="mt-0.5 text-xs font-black text-foreground">{formatPercent(metric.disk_usage)}</p>
          </div>
        </div>
      )}
    </div>
  )
}

function AlertCard({ alert }: { alert: AlertHistory }) {
  const triggeredAt = new Date(alert.triggered_at)
  const now = new Date()
  const diffMs = now.getTime() - triggeredAt.getTime()
  const diffMins = Math.floor(diffMs / 60000)

  const timeText =
    diffMins < 1
      ? '刚刚'
      : diffMins < 60
      ? `${diffMins} 分钟前`
      : diffMins < 1440
      ? `${Math.floor(diffMins / 60)} 小时前`
      : `${Math.floor(diffMins / 1440)} 天前`

  const getSeverityColor = () => {
    const value = alert.metric_value
    const threshold = alert.threshold
    const ratio = value / threshold

    if (ratio >= 1.2) return 'text-danger'
    if (ratio >= 1.1) return 'text-warning'
    return 'text-muted-foreground'
  }

  return (
    <div className="rounded-2xl border border-danger/30 bg-danger/5 p-3 shadow-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-black text-foreground">{alert.rule_name}</p>
          <p className="mt-1 truncate text-xs font-semibold text-muted-foreground">
            {alert.node_name} · {alert.metric_field}: {alert.metric_value.toFixed(1)}% {'>'} {alert.threshold}%
          </p>
        </div>
        <span className={`shrink-0 text-xs font-black ${getSeverityColor()}`}>
          {alert.metric_value.toFixed(1)}%
        </span>
      </div>
      <p className="mt-2 text-[10px] font-semibold text-muted-foreground">{timeText}</p>
    </div>
  )
}

function InfoRow({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="soft-card flex items-center justify-between gap-3 px-3 py-2 shadow-none">
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground">{icon}</span>
        <span className="text-xs font-black text-muted-foreground">{label}</span>
      </div>
      <span className="text-xs font-bold text-foreground">{value}</span>
    </div>
  )
}

function QuickActionButton({ icon, label, color, onClick }: { icon: React.ReactNode; label: string; color: string; onClick: () => void }) {
  const colorClasses = {
    blue: 'bg-primary/5 hover:bg-primary/10 border-primary/20',
    orange: 'bg-warning/5 hover:bg-warning/10 border-warning/20',
    green: 'bg-success/5 hover:bg-success/10 border-success/20',
    purple: 'bg-info/5 hover:bg-info/10 border-info/20',
    cyan: 'bg-accent/5 hover:bg-accent/10 border-accent/20',
  }

  const iconColorClasses = {
    blue: 'text-primary',
    orange: 'text-warning',
    green: 'text-success',
    purple: 'text-info',
    cyan: 'text-accent',
  }

  return (
    <button
      type="button"
      onClick={onClick}
      className={`soft-button flex flex-col items-center justify-center gap-3 border p-4 focus:outline-none focus:ring-2 focus:ring-primary/20 ${colorClasses[color as keyof typeof colorClasses]}`}
    >
      <div className={`${iconColorClasses[color as keyof typeof iconColorClasses]}`}>
        {icon}
      </div>
      <span className="text-xs font-black text-foreground">{label}</span>
    </button>
  )
}
