import { useEffect, useRef, useState } from 'react'
import { RefreshCw, X } from 'lucide-react'
import type { ContainerLogsData, ContainerLogsError, ContainerLogsExit, ContainerLogsRequest, ContainerLogsResponse, ContainerLogsStop } from '../types'
import { splitLiteralMatches } from '../lib/highlight'

interface ContainerLogsModalProps {
  nodeId: string
  containerId: string
  containerName: string
  open: boolean
  onClose: () => void
}

export default function ContainerLogsModal({
  nodeId,
  containerId,
  containerName,
  open,
  onClose,
}: ContainerLogsModalProps) {
  const [lines, setLines] = useState(100)
  const [timestamps, setTimestamps] = useState(false)
  const [logContent, setLogContent] = useState<Array<{ text: string; stream: string }>>([])
  const [isConnected, setIsConnected] = useState(false)
  const [isTailing, setIsTailing] = useState(false)
  const [error, setError] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)

  const wsRef = useRef<WebSocket | null>(null)
  const sessionIdRef = useRef<string>('')
  const logContainerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    connectLogs()

    return () => {
      disconnectLogs()
    }
  }, [open, nodeId, containerId])

  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
    }
  }, [logContent, autoScroll])

  const connectLogs = () => {
    if (wsRef.current) {
      wsRef.current.close()
    }

    setError('')
    setLogContent([])
    setIsConnected(false)
    setIsTailing(false)

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/nodes/${nodeId}/containers/${containerId}/logs/stream`

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      setIsConnected(true)
      sessionIdRef.current = Math.random().toString(36).substring(2, 15)

      // Send initial request
      const request: ContainerLogsRequest = {
        type: 'container_logs_request',
        session_id: sessionIdRef.current,
        node_id: nodeId,
        container_id: containerId,
        lines: lines,
        follow: true,
        timestamps: timestamps,
      }
      ws.send(JSON.stringify(request))
    }

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data)

        switch (message.type) {
          case 'container_logs_response': {
            const response = message as ContainerLogsResponse
            if (response.started) {
              setIsTailing(true)
            } else if (response.error) {
              setError(response.error)
              setIsConnected(false)
            }
            break
          }

          case 'container_logs_data': {
            const data = message as ContainerLogsData
            setLogContent((prev) => {
              const newLines = data.data.split('\n').filter((line) => line).map((line) => ({
                text: line,
                stream: data.stream,
              }))
              const updated = [...prev, ...newLines]
              // Keep only last 10000 lines
              return updated.slice(-10000)
            })
            break
          }

          case 'container_logs_exit': {
            const exit = message as ContainerLogsExit
            setIsTailing(false)
            if (exit.error) {
              setError(exit.error)
            }
            break
          }

          case 'container_logs_error': {
            const err = message as ContainerLogsError
            setError(err.error)
            setIsTailing(false)
            break
          }
        }
      } catch (e) {
        console.error('[ContainerLogs] Failed to parse message:', e)
      }
    }

    ws.onerror = (e) => {
      console.error('[ContainerLogs] WebSocket error:', e)
      setError('WebSocket 连接错误')
      setIsConnected(false)
      setIsTailing(false)
    }

    ws.onclose = () => {
      setIsConnected(false)
      setIsTailing(false)
    }
  }

  const disconnectLogs = () => {
    if (wsRef.current && sessionIdRef.current) {
      const stop: ContainerLogsStop = {
        type: 'container_logs_stop',
        session_id: sessionIdRef.current,
        node_id: nodeId,
      }
      wsRef.current.send(JSON.stringify(stop))
      wsRef.current.close()
      wsRef.current = null
    }
    setIsConnected(false)
    setIsTailing(false)
  }

  const handleClose = () => {
    disconnectLogs()
    onClose()
  }

  const handleClear = () => {
    setLogContent([])
  }

  const highlightText = (text: string, query: string) => splitLiteralMatches(text, query).map((part, index) =>
    part.highlighted ? (
      <span key={index} className="bg-yellow-300 text-black">
        {part.text}
      </span>
    ) : (
      <span key={index}>{part.text}</span>
    )
  )

  const filteredLogs = searchQuery.trim()
    ? logContent.filter((line) => line.text.toLowerCase().includes(searchQuery.toLowerCase()))
    : logContent

  if (!open) return null

  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4" onClick={handleClose}>
      <div
        className="soft-modal-shell flex h-[80vh] w-[90vw] max-w-6xl flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="soft-modal-header flex items-center justify-between border-b px-4 py-3">
          <h3 className="text-lg font-bold text-foreground">
            容器日志: <span className="font-mono">{containerName}</span>
          </h3>
          <button
            onClick={handleClose}
            className="soft-button inline-flex h-9 w-9 items-center justify-center text-muted-foreground hover:bg-muted hover:text-foreground"
            title="关闭"
            aria-label="关闭"
          >
            <X size={18} aria-hidden="true" />
          </button>
        </div>

        <div className="px-4 py-3">
          <div className="soft-toolbar flex flex-wrap items-center gap-3 p-3">
            <button
              onClick={connectLogs}
              className="soft-button inline-flex min-h-9 items-center gap-1.5 bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
              disabled={isConnected}
            >
              <RefreshCw size={15} aria-hidden="true" />
              刷新
            </button>
            <select
              value={lines}
              onChange={(e) => setLines(parseInt(e.target.value))}
              className="soft-input min-h-9 px-2 text-sm disabled:opacity-50"
              disabled={isConnected}
            >
              <option value={50}>50 行</option>
              <option value={100}>100 行</option>
              <option value={200}>200 行</option>
              <option value={500}>500 行</option>
              <option value={1000}>1000 行</option>
            </select>
            <label className="flex items-center gap-1.5 text-sm text-foreground">
              <input
                type="checkbox"
                checked={timestamps}
                onChange={(e) => setTimestamps(e.target.checked)}
                className="rounded border-border"
                disabled={isConnected}
              />
              时间戳
            </label>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索日志..."
              className="soft-input min-h-9 min-w-[220px] flex-1 px-3 text-sm placeholder:text-muted-foreground"
            />
            <label className="flex items-center gap-1.5 text-sm text-foreground">
              <input
                type="checkbox"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
                className="rounded border-border"
              />
              自动滚动
            </label>
            <button
              onClick={handleClear}
              className="soft-button min-h-9 border border-border bg-card px-3 text-sm font-medium text-foreground hover:bg-muted"
            >
              清空
            </button>
            {isTailing && (
              <div className="flex items-center gap-1.5 text-sm text-green-600">
                <div className="h-2 w-2 animate-pulse rounded-full bg-green-600"></div>
                监听中
              </div>
            )}
          </div>
        </div>

        {/* Error display */}
        {error && (
          <div className="mx-4 mt-1 rounded-2xl border border-danger/50 bg-danger/10 p-3 text-sm text-danger">
            {error}
          </div>
        )}

        <div
          ref={logContainerRef}
          className="mx-4 min-h-0 flex-1 overflow-auto rounded-2xl border border-border bg-code px-4 py-3 font-mono text-xs text-code-foreground"
        >
          {filteredLogs.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              {isConnected ? '等待日志数据...' : '点击刷新开始查看日志'}
            </div>
          ) : (
            <div className="space-y-0.5">
              {filteredLogs.map((line, index) => (
                <div
                  key={index}
                  className={line.stream === 'stderr' ? 'text-danger' : 'text-code-foreground'}
                >
                  {highlightText(line.text, searchQuery)}
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="soft-modal-footer mt-4 border-t px-4 py-2 text-xs text-muted-foreground">
          共 {logContent.length} 行
          {searchQuery && ` · 筛选后 ${filteredLogs.length} 行`}
        </div>
      </div>
    </div>
  )
}
