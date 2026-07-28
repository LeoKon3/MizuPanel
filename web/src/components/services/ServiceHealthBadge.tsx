import { CircleAlert, CircleCheck, CircleDashed, TriangleAlert } from 'lucide-react'

import type { ApplicationServiceHealth } from '../../types'

const healthCopy: Record<ApplicationServiceHealth, { label: string, className: string }> = {
  healthy: { label: '健康', className: 'border-success/30 bg-success/10 text-success' },
  degraded: { label: '降级', className: 'border-warning/30 bg-warning/10 text-warning' },
  unhealthy: { label: '异常', className: 'border-danger/30 bg-danger/10 text-danger' },
  unknown: { label: '未知', className: 'border-border bg-muted text-muted-foreground' }
}

export function ServiceHealthBadge({ health, compact = false }: { health: ApplicationServiceHealth, compact?: boolean }) {
  const copy = healthCopy[health]
  const Icon = health === 'healthy' ? CircleCheck : health === 'degraded' ? TriangleAlert : health === 'unhealthy' ? CircleAlert : CircleDashed
  return (
    <span aria-label={`服务状态：${copy.label}`} className={`inline-flex shrink-0 items-center gap-1.5 rounded-full border font-black ${compact ? 'px-2 py-1 text-[11px]' : 'px-2.5 py-1 text-xs'} ${copy.className}`}>
      <Icon size={compact ? 12 : 14} aria-hidden="true" />
      {copy.label}
    </span>
  )
}

export function healthLabel(health: ApplicationServiceHealth) {
  return healthCopy[health].label
}
