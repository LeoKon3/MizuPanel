import type { RangeOption, SettingsResponse, SystemAboutResponse } from '../types'

const retentionOptions: Array<{ value: RangeOption, label: string, detail: string }> = [
  { value: '6h', label: '6 小时', detail: '轻量测试环境，数据占用最少。' },
  { value: '24h', label: '24 小时', detail: '适合观察一天内的波动。' },
  { value: '3d', label: '3 天', detail: '适合排查短期异常。' },
  { value: '7d', label: '7 天', detail: '当前上限，保留一周趋势。' }
]

export function SystemSettingsPage({ settings, about, selectedRetention, saving, message, error, onSelectRetention, onSave }: { settings?: SettingsResponse, about?: SystemAboutResponse, selectedRetention: RangeOption, saving: boolean, message?: string, error?: string, onSelectRetention: (retention: RangeOption) => void, onSave: () => void }) {
  return (
    <section aria-label="系统设置" className="soft-panel">
      <div className="soft-panel-header px-5 py-5">
        <p className="text-[11px] font-black uppercase tracking-[0.26em] text-primary">Settings</p>
        <h2 className="mt-1 font-display text-3xl font-black tracking-tight text-foreground">系统设置</h2>
        <p className="mt-2 max-w-3xl text-sm font-semibold leading-6 text-muted-foreground">先放真正会影响当前使用的设置：指标保留时间。保存后不需要重启 Server，会立即用于历史查询和后续清理。</p>
      </div>

      <div className="grid gap-4 p-4 xl:grid-cols-[0.66fr_0.34fr]">
        <section className="soft-card p-4">
          <div className="mb-4 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <p className="text-xs font-black tracking-[0.18em] text-primary">Metrics Retention</p>
              <h3 className="mt-1 text-xl font-black text-foreground">指标保留时间</h3>
            </div>
            <span className="soft-chip w-fit px-3 py-1 text-xs font-black text-muted-foreground">当前：{settings ? retentionLabel(settings.metrics_retention) : '加载中'}</span>
          </div>

          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {retentionOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                aria-label={option.label}
                aria-pressed={selectedRetention === option.value}
                onClick={() => onSelectRetention(option.value)}
                className={`soft-button min-h-36 border p-4 text-left focus:outline-none focus:ring-4 focus:ring-primary/20 ${selectedRetention === option.value ? 'border-primary/40 bg-primary text-primary-foreground shadow-sm' : 'border-border bg-surface/80 text-muted-foreground hover:bg-card hover:text-foreground'}`}
              >
                <span className="block text-lg font-black">{option.label}</span>
                <span className={`mt-2 block text-xs font-bold leading-5 ${selectedRetention === option.value ? 'text-primary-foreground/75' : 'text-muted-foreground'}`}>{option.detail}</span>
              </button>
            ))}
          </div>

          <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-bold leading-6 text-warning">
            <p>从短保留改成长保留不会恢复已经清理的数据；从长保留改短保留后，下一轮清理会删除超出范围的数据。</p>
            <button type="button" onClick={onSave} disabled={saving} className="soft-button min-h-11 shrink-0 bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60">
              {saving ? '保存中...' : '保存设置'}
            </button>
          </div>

          {message ? <p className="mt-3 rounded-xl border border-success/30 bg-success/10 px-4 py-3 text-sm font-black text-success">{message}</p> : null}
          {error ? <p className="mt-3 rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">{error}</p> : null}
        </section>

        <div className="space-y-4">
          <aside className="soft-card p-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="text-xs font-black tracking-[0.18em] text-primary">About</p>
                <h3 className="mt-1 text-lg font-black text-foreground">关于 MizuPanel</h3>
                <p className="mt-2 text-sm font-bold text-muted-foreground">当前版本</p>
                <p className="mt-1 font-mono text-2xl font-black text-foreground">{about ? `v${about.version}` : '加载中'}</p>
              </div>
              <a
                href={about?.github_url || 'https://github.com/LeoKon3/MizuPanel'}
                target="_blank"
                rel="noreferrer"
                aria-label="打开 GitHub 仓库"
                className="soft-button inline-flex h-11 w-11 shrink-0 items-center justify-center border border-border bg-card text-foreground hover:bg-surface focus:outline-none focus:ring-4 focus:ring-primary/20"
              >
                <GitHubMark className="h-5 w-5" />
              </a>
            </div>
          </aside>

          <aside className="soft-card p-4 text-sm font-bold leading-6 text-muted-foreground">
            <p className="text-xs font-black tracking-[0.18em] text-muted-foreground">说明</p>
            <ul className="mt-3 space-y-3">
              <li className="rounded-2xl border border-border/80 bg-surface/70 px-4 py-3">最大保留时间目前限制为 7 天，避免 SQLite 指标表无限增长。</li>
              <li className="rounded-2xl border border-border/80 bg-surface/70 px-4 py-3">历史记录页的可查询范围会跟随这个设置。</li>
              <li className="rounded-2xl border border-border/80 bg-surface/70 px-4 py-3">配置文件仍作为首次启动默认值；后台保存后以数据库设置为准。</li>
            </ul>
          </aside>
        </div>
      </div>
    </section>
  )
}

function retentionLabel(value: RangeOption) {
  return retentionOptions.find((option) => option.value === value)?.label ?? value
}

function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true" fill="currentColor">
      <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.09 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-2.14c-3.2.7-3.87-1.36-3.87-1.36-.52-1.32-1.28-1.67-1.28-1.67-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.75 2.69 1.25 3.34.95.1-.74.4-1.25.73-1.54-2.55-.29-5.23-1.28-5.23-5.68 0-1.25.45-2.28 1.18-3.08-.12-.29-.51-1.46.11-3.04 0 0 .96-.31 3.16 1.18.92-.25 1.9-.38 2.87-.39.97.01 1.95.14 2.87.39 2.19-1.49 3.15-1.18 3.15-1.18.63 1.58.24 2.75.12 3.04.74.8 1.18 1.83 1.18 3.08 0 4.42-2.69 5.39-5.25 5.67.41.36.78 1.06.78 2.13v3.16c0 .31.21.68.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z" />
    </svg>
  )
}
