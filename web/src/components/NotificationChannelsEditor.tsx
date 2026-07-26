import type { NotificationChannel } from '../types'

type SupportedChannelType = 'webhook' | 'dingtalk' | 'feishu'

type NotificationChannelsEditorProps = {
  value: NotificationChannel[]
  onChange: (channels: NotificationChannel[]) => void
  disabled?: boolean
  maxChannels?: number
}

const channelOptions: Array<{ type: SupportedChannelType, label: string }> = [
  { type: 'webhook', label: 'Webhook' },
  { type: 'dingtalk', label: 'DingTalk' },
  { type: 'feishu', label: '飞书' }
]

function channelLabel(type: NotificationChannel['type']) {
  if (type === 'webhook') return 'Webhook'
  if (type === 'dingtalk') return 'DingTalk'
  if (type === 'feishu') return '飞书'
  return type
}

function webhookPlaceholder(type: NotificationChannel['type']) {
  if (type === 'dingtalk') return 'https://oapi.dingtalk.com/robot/send?access_token=...'
  if (type === 'feishu') return 'https://open.feishu.cn/open-apis/bot/v2/hook/...'
  return 'https://...'
}

export function NotificationChannelsEditor({ value, onChange, disabled = false, maxChannels = Number.POSITIVE_INFINITY }: NotificationChannelsEditorProps) {
  const addChannel = (type: SupportedChannelType) => {
    if (disabled || value.length >= maxChannels) return
    onChange([...value, { type, webhook_url: '' }])
  }

  const updateChannel = (index: number, updates: Partial<NotificationChannel>) => {
    onChange(value.map((channel, channelIndex) => channelIndex === index ? { ...channel, ...updates } : channel))
  }

  const removeChannel = (index: number) => {
    onChange(value.filter((_, channelIndex) => channelIndex !== index))
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-black text-foreground">通知渠道</p>
          <p className="mt-0.5 text-xs font-semibold text-muted-foreground">支持 Webhook、钉钉和飞书；留空则仅记录事件。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {channelOptions.map((option) => (
            <button
              key={option.type}
              type="button"
              onClick={() => addChannel(option.type)}
              disabled={disabled || value.length >= maxChannels}
              className="soft-button min-h-8 cursor-pointer border border-border bg-card px-3 text-xs font-black text-foreground hover:border-primary/50 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              + {option.label}
            </button>
          ))}
        </div>
      </div>

      {value.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-border bg-surface/60 px-3 py-3 text-xs font-semibold text-muted-foreground">
          暂无通知渠道
        </div>
      ) : (
        <div className="space-y-3">
          {value.map((channel, index) => (
            <div key={`${channel.type}-${index}`} className="rounded-2xl border border-border/85 bg-surface/80 p-3">
              <div className="mb-2 flex items-center justify-between gap-3">
                <span className="text-xs font-black uppercase text-primary">{channelLabel(channel.type)}</span>
                <button
                  type="button"
                  onClick={() => removeChannel(index)}
                  disabled={disabled}
                  aria-label={`删除${channelLabel(channel.type)}通知渠道 ${index + 1}`}
                  className="cursor-pointer text-xs font-black text-danger hover:underline disabled:cursor-not-allowed disabled:opacity-50"
                >
                  删除
                </button>
              </div>
              <label className="block text-xs font-bold text-muted-foreground">
                Webhook 地址
                <input
                  type="url"
                  aria-label={`${channelLabel(channel.type)} Webhook 地址 ${index + 1}`}
                  value={channel.webhook_url || ''}
                  onChange={(event) => updateChannel(index, { webhook_url: event.target.value })}
                  disabled={disabled}
                  placeholder={webhookPlaceholder(channel.type)}
                  className="soft-input mt-1 min-h-9 w-full px-3 text-xs font-bold disabled:cursor-not-allowed disabled:opacity-60"
                />
              </label>
              {channel.type === 'dingtalk' || channel.type === 'feishu' ? (
                <label className="mt-2 block text-xs font-bold text-muted-foreground">
                  签名 Secret（可选）
                  <input
                    type="password"
                    aria-label={`${channelLabel(channel.type)} Secret ${index + 1}`}
                    value={channel.secret || ''}
                    onChange={(event) => updateChannel(index, { secret: event.target.value })}
                    disabled={disabled}
                    autoComplete="off"
                    className="soft-input mt-1 min-h-9 w-full px-3 text-xs font-bold disabled:cursor-not-allowed disabled:opacity-60"
                  />
                </label>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
