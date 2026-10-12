import { useEffect, useRef, useState } from 'react'

import type { AgentUpgradeResponse, AgentUpgradeStatus } from '../../types'

type AgentUpgradeToast = {
  message: string
  type: 'success' | 'error'
}

type UseAgentUpgradeOptions = {
  nodeID?: string
  onUpgradeAgent?: (nodeID: string) => Promise<AgentUpgradeResponse>
  onGetAgentUpgradeStatus?: (nodeID: string) => Promise<AgentUpgradeStatus>
  onAccepted: () => void
  onToast: (toast: AgentUpgradeToast) => void
}

const POLL_INTERVAL_MS = 3_000
const MAX_POLL_ATTEMPTS = 40

export function useAgentUpgrade({ nodeID, onUpgradeAgent, onGetAgentUpgradeStatus, onAccepted, onToast }: UseAgentUpgradeOptions) {
  const [upgradeLoading, setUpgradeLoading] = useState(false)
  const pollGeneration = useRef(0)
  const pollTimeout = useRef<number | undefined>(undefined)
  const upgradeInFlight = useRef(false)
  const currentNodeID = useRef(nodeID)
  currentNodeID.current = nodeID

  useEffect(() => {
    pollGeneration.current += 1
    upgradeInFlight.current = false
    if (pollTimeout.current !== undefined) {
      window.clearTimeout(pollTimeout.current)
      pollTimeout.current = undefined
    }
    setUpgradeLoading(false)

    return () => {
      pollGeneration.current += 1
      upgradeInFlight.current = false
      if (pollTimeout.current !== undefined) {
        window.clearTimeout(pollTimeout.current)
        pollTimeout.current = undefined
      }
    }
  }, [nodeID])

  const confirmAgentUpgrade = () => {
    if (!nodeID || !onUpgradeAgent || upgradeInFlight.current) return

    const upgradeNodeID = nodeID
    const generation = ++pollGeneration.current
    upgradeInFlight.current = true
    if (pollTimeout.current !== undefined) {
      window.clearTimeout(pollTimeout.current)
      pollTimeout.current = undefined
    }
    setUpgradeLoading(true)

    onUpgradeAgent(upgradeNodeID)
      .then((response) => {
        if (generation !== pollGeneration.current || currentNodeID.current !== upgradeNodeID) return
        if (!response.accepted) throw new Error(response.error || '升级请求未被接受')

        onAccepted()
        onToast({ message: 'Agent升级已开始，正在等待重新连接', type: 'success' })
        if (!onGetAgentUpgradeStatus) {
          upgradeInFlight.current = false
          setUpgradeLoading(false)
          return
        }

        let attempts = 0
        let pollRequestInFlight = false
        const finish = (toast?: AgentUpgradeToast) => {
          if (generation !== pollGeneration.current || currentNodeID.current !== upgradeNodeID) return
          pollGeneration.current += 1
          pollTimeout.current = undefined
          upgradeInFlight.current = false
          setUpgradeLoading(false)
          if (toast) onToast(toast)
        }
        const poll = () => {
          pollTimeout.current = undefined
          if (generation !== pollGeneration.current || currentNodeID.current !== upgradeNodeID || pollRequestInFlight) return

          pollRequestInFlight = true
          attempts += 1
          onGetAgentUpgradeStatus(upgradeNodeID)
            .then((status) => {
              if (generation !== pollGeneration.current || currentNodeID.current !== upgradeNodeID) return
              if (status.stage === 'completed') {
                finish({ message: 'Agent升级成功', type: 'success' })
                return
              }
              if (status.stage === 'failed') {
                finish({ message: `Agent升级失败: ${status.error || '未知错误'}`, type: 'error' })
                return
              }
              if (attempts >= MAX_POLL_ATTEMPTS) {
                finish({ message: 'Agent升级失败: 等待重新连接超时', type: 'error' })
                return
              }
              pollTimeout.current = window.setTimeout(poll, POLL_INTERVAL_MS)
            })
            .catch(() => {
              if (generation !== pollGeneration.current || currentNodeID.current !== upgradeNodeID) return
              if (attempts >= MAX_POLL_ATTEMPTS) {
                finish({ message: 'Agent升级失败: 等待重新连接超时', type: 'error' })
                return
              }
              pollTimeout.current = window.setTimeout(poll, POLL_INTERVAL_MS)
            })
            .finally(() => {
              pollRequestInFlight = false
            })
        }

        pollTimeout.current = window.setTimeout(poll, POLL_INTERVAL_MS)
      })
      .catch((err: unknown) => {
        if (generation === pollGeneration.current && currentNodeID.current === upgradeNodeID) {
          upgradeInFlight.current = false
          setUpgradeLoading(false)
          onToast({ message: `Agent升级失败: ${err instanceof Error ? err.message : '未知错误'}`, type: 'error' })
        }
      })
  }

  return { confirmAgentUpgrade, upgradeLoading }
}
