import type { ApplicationServiceHealth } from '../../types'

const healthCopy: Record<ApplicationServiceHealth, string> = { healthy: '健康', degraded: '降级', unhealthy: '异常', unknown: '未知' }
export function ServiceHealthBadge({ health, compact = false }: { health: ApplicationServiceHealth; compact?: boolean }) {
  return <span aria-label={`服务状态：${healthLabel(health)}`} className={`service-health health-${health} ${compact ? 'is-compact' : ''}`}><i aria-hidden="true" />{healthLabel(health)}</span>
}
export function healthLabel(health: ApplicationServiceHealth) { return healthCopy[health] || '未知' }
