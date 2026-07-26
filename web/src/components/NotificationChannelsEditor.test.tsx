import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, test } from 'vitest'

import { NotificationChannelsEditor } from './NotificationChannelsEditor'
import type { NotificationChannel } from '../types'

function Harness({ initial = [], maxChannels }: { initial?: NotificationChannel[], maxChannels?: number }) {
  const [channels, setChannels] = useState(initial)
  return <NotificationChannelsEditor value={channels} onChange={setChannels} maxChannels={maxChannels} />
}

describe('NotificationChannelsEditor', () => {
  test('adds, edits, and removes supported channels', () => {
    render(<Harness />)

    expect(screen.getByText('暂无通知渠道')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '+ DingTalk' }))

    fireEvent.change(screen.getByLabelText('DingTalk Webhook 地址 1'), { target: { value: 'https://hooks.example.com/dingtalk' } })
    fireEvent.change(screen.getByLabelText('DingTalk Secret 1'), { target: { value: 'signing-secret' } })
    expect(screen.getByLabelText('DingTalk Webhook 地址 1')).toHaveValue('https://hooks.example.com/dingtalk')
    expect(screen.getByLabelText('DingTalk Secret 1')).toHaveValue('signing-secret')

    fireEvent.click(screen.getByRole('button', { name: '删除DingTalk通知渠道 1' }))
    expect(screen.getByText('暂无通知渠道')).toBeInTheDocument()
  })

  test('enforces the configured channel count', () => {
    render(<Harness initial={[{ type: 'webhook', webhook_url: 'https://hooks.example.com' }]} maxChannels={1} />)

    expect(screen.getByRole('button', { name: '+ Webhook' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '+ DingTalk' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '+ 飞书' })).toBeDisabled()
  })

  test('does not impose the uptime-only limit when no maximum is configured', () => {
    const initial = Array.from({ length: 10 }, (_, index) => ({
      type: 'webhook' as const,
      webhook_url: `https://hooks.example.com/${index}`
    }))
    render(<Harness initial={initial} />)

    expect(screen.getByRole('button', { name: '+ Webhook' })).toBeEnabled()
  })
})
