type K8sStatusBadgeProps = {
  status: string
}

function normalizeStatus(status: string) {
  return status.trim().toLowerCase()
}

export function K8sStatusBadge({ status }: K8sStatusBadgeProps) {
  const normalized = normalizeStatus(status)
  const tone = normalized === 'running' || normalized === 'active' || normalized === 'ready' || normalized === 'true'
    ? 'success'
    : normalized === 'pending' || normalized === 'warning' || normalized === 'containercreating'
      ? 'warning'
      : normalized === 'failed' || normalized === 'error' || normalized === 'crashloopbackoff'
        ? 'danger'
        : normalized === 'unknown' || normalized === 'offline'
          ? 'muted'
          : 'success'

  const className = tone === 'success'
    ? 'border-success/20 bg-success/10 text-success'
    : tone === 'warning'
      ? 'border-warning/20 bg-warning/10 text-warning'
      : tone === 'danger'
        ? 'border-danger/20 bg-danger/10 text-danger'
        : 'border-border bg-muted text-muted-foreground'

  const label = status || 'Unknown'

  return (
    <span className={`inline-flex max-w-full min-w-0 items-center truncate rounded-full border px-2.5 py-1 text-xs font-black ${className}`} title={label}>
      {label}
    </span>
  )
}
