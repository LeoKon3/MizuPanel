import { AIProviderSettings } from '../components/ai/AIProviderSettings'
import type { RangeOption, SettingsResponse, SystemAboutResponse } from '../types'

const retentionOptions: Array<{ value: RangeOption, label: string }> = [
  { value: '6h', label: '6 小时' },
  { value: '24h', label: '24 小时' },
  { value: '3d', label: '3 天' },
  { value: '7d', label: '7 天' }
]

export function SystemSettingsPage({ settings, about, selectedRetention, saving, message, error, onSelectRetention, onSave }: { settings?: SettingsResponse, about?: SystemAboutResponse, selectedRetention: RangeOption, saving: boolean, message?: string, error?: string, onSelectRetention: (retention: RangeOption) => void, onSave: () => void }) {
  return (
    <section aria-label="系统设置" className="soft-panel">
      <div className="soft-panel-header flex flex-wrap items-center justify-between gap-4 px-5 py-4">
        <div className="min-w-0">
          <h2 className="font-display text-2xl font-black text-foreground">系统设置</h2>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <span className="text-xs font-semibold text-muted-foreground">版本 <strong className="font-mono text-foreground">{about ? `v${about.version}` : '加载中'}</strong></span>
          <a
            href={about?.github_url || 'https://github.com/LeoKon3/MizuPanel'}
            target="_blank"
            rel="noreferrer"
            aria-label="打开 GitHub 仓库"
            className="soft-button inline-flex h-9 w-9 items-center justify-center border border-border bg-card text-foreground hover:bg-surface focus:outline-none focus:ring-4 focus:ring-primary/20"
          >
            <GitHubMark className="h-4 w-4" />
          </a>
        </div>
      </div>

      <section className="border-b border-border bg-surface/35 px-4 py-4 sm:px-5" aria-labelledby="metrics-retention-title">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <h3 id="metrics-retention-title" className="text-base font-black text-foreground">指标保留时间</h3>
            <span className="soft-chip px-2.5 py-1 text-xs font-black text-muted-foreground">当前 {settings ? retentionLabel(settings.metrics_retention) : '加载中'}</span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex flex-wrap gap-1 border border-border bg-card p-1" role="group" aria-label="指标保留时间">
              {retentionOptions.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  aria-label={option.label}
                  aria-pressed={selectedRetention === option.value}
                  onClick={() => onSelectRetention(option.value)}
                  className={`soft-button min-h-10 px-3 text-sm font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${selectedRetention === option.value ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-card hover:text-foreground'}`}
                >
                  {option.label}
                </button>
              ))}
            </div>
            <button type="button" onClick={onSave} disabled={saving} className="soft-button min-h-11 shrink-0 bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60">
              {saving ? '保存中...' : '保存设置'}
            </button>
          </div>
        </div>
        {message ? <p className="mt-3 text-sm font-black text-success">{message}</p> : null}
        {error ? <p className="mt-3 text-sm font-black text-danger">{error}</p> : null}
      </section>
      <AIProviderSettings />
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
