import { useState } from 'react'
import { Bot, Database, ExternalLink, LoaderCircle, Save } from 'lucide-react'

import { AIProviderSettings } from '../components/ai/AIProviderSettings'
import type { RangeOption, SettingsResponse, SystemAboutResponse } from '../types'

const retentionOptions: Array<{ value: RangeOption, label: string }> = [
  { value: '6h', label: '6 小时' },
  { value: '24h', label: '24 小时' },
  { value: '3d', label: '3 天' },
  { value: '7d', label: '7 天' }
]

const settingsPages = [
  { id: 'settings-general', label: '常规设置', icon: Database },
  { id: 'settings-ai', label: 'AI 模型', icon: Bot }
] as const

type SettingsPageID = typeof settingsPages[number]['id']

type SystemSettingsPageProps = {
  settings?: SettingsResponse
  about?: SystemAboutResponse
  selectedRetention: RangeOption
  saving: boolean
  message?: string
  error?: string
  onSelectRetention: (retention: RangeOption) => void
  onSave: () => void
}

export function SystemSettingsPage({ settings, about, selectedRetention, saving, message, error, onSelectRetention, onSave }: SystemSettingsPageProps) {
  const [activePage, setActivePage] = useState<SettingsPageID>('settings-general')

  return (
    <section aria-label="系统设置" className="mx-auto w-full max-w-[1380px]">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-5 px-1">
        <h2 className="font-display text-3xl font-black tracking-tight text-foreground">系统设置</h2>
        <div className="flex shrink-0 items-center gap-2">
          <span className="inline-flex min-h-10 items-center rounded-full border border-border bg-card px-4 text-xs font-semibold text-muted-foreground shadow-sm">
            版本 <strong className="ml-1.5 font-mono text-foreground">{about ? `v${about.version}` : '加载中'}</strong>
          </span>
          <a
            href={about?.github_url || 'https://github.com/LeoKon3/MizuPanel'}
            target="_blank"
            rel="noreferrer"
            aria-label="打开 GitHub 仓库"
            className="soft-button inline-flex min-h-10 items-center gap-2 rounded-full border border-border bg-card px-4 text-xs font-black text-foreground shadow-sm hover:bg-surface focus:outline-none focus:ring-4 focus:ring-primary/20"
          >
            <GitHubMark className="h-4 w-4" />
            GitHub
            <ExternalLink className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
          </a>
        </div>
      </header>

      <div data-testid="settings-layout" className="grid min-w-0 gap-5 xl:grid-cols-[210px_minmax(0,1fr)] xl:items-start">
        <aside className="xl:sticky xl:top-[76px] xl:self-start">
          <nav aria-label="设置分类" className="soft-panel flex gap-2 overflow-x-auto p-2 xl:flex-col xl:overflow-visible">
            {settingsPages.map((page) => {
              const Icon = page.icon
              const selected = activePage === page.id
              return (
                <button
                  key={page.id}
                  type="button"
                  aria-current={selected ? 'page' : undefined}
                  aria-controls={`${page.id}-page`}
                  onClick={() => setActivePage(page.id)}
                  className={`soft-button flex min-h-12 min-w-[180px] items-center gap-3 rounded-[18px] px-3.5 text-left transition-colors xl:min-w-0 ${selected ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-surface hover:text-foreground'}`}
                >
                  <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl ${selected ? 'bg-primary/15' : 'bg-surface'}`}>
                    <Icon className="h-[18px] w-[18px]" strokeWidth={2} aria-hidden="true" />
                  </span>
                  <span className="truncate text-sm font-black">{page.label}</span>
                </button>
              )
            })}
          </nav>
        </aside>

        <div className="min-w-0">
          <section
            id="settings-general-page"
            data-testid="settings-general-page"
            aria-labelledby="settings-general-title"
            hidden={activePage !== 'settings-general'}
            className="overflow-hidden rounded-[20px] border border-border bg-card shadow-sm"
          >
            <div className="flex items-start gap-3 border-b border-border px-5 py-5 sm:px-6">
              <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-[14px] bg-primary/10 text-primary">
                <Database className="h-5 w-5" strokeWidth={2} aria-hidden="true" />
              </span>
              <h3 id="settings-general-title" className="text-lg font-black text-foreground">常规设置</h3>
            </div>

            <div className="grid gap-5 px-5 py-5 sm:px-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h4 className="text-sm font-black text-foreground">指标保留时间</h4>
                <span className="rounded-full bg-primary/10 px-2.5 py-1 text-[11px] font-black text-primary">当前 {settings ? retentionLabel(settings.metrics_retention) : '加载中'}</span>
              </div>
              <div className="inline-flex w-fit max-w-full flex-wrap gap-1 rounded-full border border-border bg-surface p-1" role="group" aria-label="指标保留时间">
                {retentionOptions.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    aria-label={option.label}
                    aria-pressed={selectedRetention === option.value}
                    onClick={() => onSelectRetention(option.value)}
                    className={`soft-button min-h-10 rounded-full px-4 text-sm font-black focus:outline-none focus:ring-4 focus:ring-primary/20 ${selectedRetention === option.value ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:bg-card hover:text-foreground'}`}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="flex min-h-[72px] flex-wrap items-center justify-between gap-3 border-t border-border bg-surface/45 px-5 py-3.5 sm:px-6">
              <div className="min-w-0" aria-live="polite">
                {message ? <p role="status" className="text-sm font-black text-success">{message}</p> : null}
                {error ? <p role="alert" className="text-sm font-black text-danger">{error}</p> : null}
              </div>
              <button type="button" onClick={onSave} disabled={saving} className="soft-button inline-flex min-h-11 shrink-0 items-center gap-2 rounded-full bg-primary px-5 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60">
                {saving ? <LoaderCircle className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Save className="h-4 w-4" aria-hidden="true" />}
                {saving ? '保存中...' : '保存设置'}
              </button>
            </div>
          </section>

          <section
            id="settings-ai-page"
            data-testid="settings-ai-page"
            aria-label="AI 模型设置"
            hidden={activePage !== 'settings-ai'}
            className="min-w-0"
          >
            <AIProviderSettings />
          </section>
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
