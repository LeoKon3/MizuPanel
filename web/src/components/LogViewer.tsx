import { useEffect, useRef, useState } from 'react'
import { X } from 'lucide-react'
import { splitLiteralMatches } from '../lib/highlight'

interface LogViewerProps {
  nodeId: string
}

interface LogSuggestion {
  category: string
  paths: string[]
}

const LOG_SUGGESTIONS: LogSuggestion[] = [
  {
    category: '系统日志',
    paths: [
      '/var/log/messages',
    ],
  },
]

export default function LogViewer({ nodeId }: LogViewerProps) {
  const [customPath, setCustomPath] = useState('')
  const [lines, setLines] = useState(100)
  const [logContent, setLogContent] = useState<string[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [isTailing, setIsTailing] = useState(false)
  const [error, setError] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [recentPaths, setRecentPaths] = useState<string[]>([])
  const [savedPaths, setSavedPaths] = useState<string[]>([])
  const [isAddingPath, setIsAddingPath] = useState(false)
  const [newPathInput, setNewPathInput] = useState('')

  const wsRef = useRef<WebSocket | null>(null)
  const sessionIdRef = useRef<string>('')
  const logContainerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // Load recent paths from localStorage (global)
    const stored = localStorage.getItem('mizupanel_recent_log_paths')
    if (stored) {
      try {
        setRecentPaths(JSON.parse(stored))
      } catch (e) {
        // ignore
      }
    }

    // Load saved paths for this node
    const savedKey = `mizupanel_saved_log_paths_${nodeId}`
    const savedStored = localStorage.getItem(savedKey)
    if (savedStored) {
      try {
        setSavedPaths(JSON.parse(savedStored))
      } catch (e) {
        // ignore
      }
    }
  }, [nodeId])

  const addRecentPath = (path: string) => {
    setRecentPaths((prev) => {
      const updated = [path, ...prev.filter((p) => p !== path)].slice(0, 5)
      localStorage.setItem('mizupanel_recent_log_paths', JSON.stringify(updated))
      return updated
    })
  }

  const savePathForNode = (path: string) => {
    if (!path.trim()) return
    setSavedPaths((prev) => {
      if (prev.includes(path)) return prev
      const updated = [...prev, path]
      const savedKey = `mizupanel_saved_log_paths_${nodeId}`
      localStorage.setItem(savedKey, JSON.stringify(updated))
      return updated
    })
  }

  const addNewPath = () => {
    if (!newPathInput.trim()) return
    savePathForNode(newPathInput)
    setNewPathInput('')
    setIsAddingPath(false)
  }

  const removeSavedPath = (path: string) => {
    setSavedPaths((prev) => {
      const updated = prev.filter((p) => p !== path)
      const savedKey = `mizupanel_saved_log_paths_${nodeId}`
      localStorage.setItem(savedKey, JSON.stringify(updated))
      return updated
    })
  }

  const connectLogTail = (path: string) => {
    if (!path.trim()) {
      setError('请输入日志文件路径')
      return
    }

    // Disconnect existing connection
    if (wsRef.current) {
      wsRef.current.close()
    }

    setError('')
    setLogContent([])
    setIsConnected(false)
    setIsTailing(false)

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/nodes/${nodeId}/logs/tail`

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      setIsConnected(true)
      sessionIdRef.current = Math.random().toString(36).substring(2, 15)

      // Send initial request
      const request = {
        type: 'log_tail_request',
        session_id: sessionIdRef.current,
        path: path,
        lines: lines,
      }
      ws.send(JSON.stringify(request))
    }

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data)

        switch (message.type) {
          case 'log_tail_response':
            if (message.started) {
              setIsTailing(true)
              addRecentPath(path)
            } else if (message.error) {
              setError(message.error)
              setIsConnected(false)
            }
            break

          case 'log_tail_data':
            setLogContent((prev) => {
              const newLines = [...prev, ...message.data.split('\n').filter((line: string) => line)]
              // Keep only last 10000 lines
              return newLines.slice(-10000)
            })
            break

          case 'log_tail_exit':
            setIsTailing(false)
            if (message.error) {
              setError(message.error)
            }
            break

          case 'log_tail_error':
            setError(message.error)
            setIsTailing(false)
            break
        }
      } catch (e) {
        console.error('[LogViewer] Failed to parse log message:', e)
      }
    }

    ws.onerror = (e) => {
      console.error('[LogViewer] WebSocket error:', e)
      setError('WebSocket 连接错误')
      setIsConnected(false)
      setIsTailing(false)
    }

    ws.onclose = () => {
      setIsConnected(false)
      setIsTailing(false)
    }
  }

  const disconnectLogTail = () => {
    if (wsRef.current && sessionIdRef.current) {
      wsRef.current.send(
        JSON.stringify({
          type: 'log_tail_stop',
          session_id: sessionIdRef.current,
        })
      )
      wsRef.current.close()
      wsRef.current = null
    }
    setIsConnected(false)
    setIsTailing(false)
  }

  useEffect(() => {
    return () => {
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [])

  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
    }
  }, [logContent, autoScroll])

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
    ? logContent.filter((line) => line.toLowerCase().includes(searchQuery.toLowerCase()))
    : logContent

  return (
    <div className="flex h-full gap-4">
      {/* Left sidebar - Quick access */}
      <div className="w-64 flex-shrink-0 space-y-4">
        <div className="rounded-lg border border-border bg-card p-4">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-foreground">已保存路径</h3>
            {!isAddingPath && (
              <button
                onClick={() => setIsAddingPath(true)}
                className="rounded p-1 text-xs text-primary hover:bg-primary/10"
                title="添加新路径"
              >
                + 添加
              </button>
            )}
          </div>

          {isAddingPath && (
            <div className="mb-2 space-y-1">
              <input
                type="text"
                value={newPathInput}
                onChange={(e) => setNewPathInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    addNewPath()
                  } else if (e.key === 'Escape') {
                    setIsAddingPath(false)
                    setNewPathInput('')
                  }
                }}
                placeholder="输入日志路径..."
                className="w-full rounded border border-border bg-background px-2 py-1 text-xs text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
                autoFocus
              />
              <div className="flex gap-1">
                <button
                  onClick={addNewPath}
                  className="flex-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                  disabled={!newPathInput.trim()}
                >
                  确定
                </button>
                <button
                  onClick={() => {
                    setIsAddingPath(false)
                    setNewPathInput('')
                  }}
                  className="flex-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-foreground hover:bg-muted"
                >
                  取消
                </button>
              </div>
            </div>
          )}

          <div className="space-y-1">
            {savedPaths.length === 0 && !isAddingPath ? (
              <p className="text-xs text-muted-foreground">暂无保存的路径</p>
            ) : (
              savedPaths.map((path) => (
                <div key={path} className="flex items-center gap-1">
                  <button
                    onClick={() => {
                      setCustomPath(path)
                      connectLogTail(path)
                    }}
                    className="flex-1 truncate rounded px-2 py-1 text-left text-xs text-foreground hover:bg-muted"
                    title={path}
                  >
                    {path}
                  </button>
                  <button
                    onClick={() => removeSavedPath(path)}
                    className="soft-button inline-flex h-7 w-7 items-center justify-center text-xs text-muted-foreground hover:bg-danger/10 hover:text-danger"
                    title="删除"
                    aria-label="删除路径"
                  >
                    <X size={13} aria-hidden="true" />
                  </button>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="rounded-lg border border-border bg-card p-4">
          <h3 className="mb-3 text-sm font-semibold text-foreground">快速访问</h3>
          <div className="space-y-4">
            {LOG_SUGGESTIONS.map((category) => (
              <div key={category.category}>
                <div className="mb-2 text-xs font-medium text-muted-foreground">{category.category}</div>
                <div className="space-y-1">
                  {category.paths.map((path) => (
                    <button
                      key={path}
                      onClick={() => {
                        setCustomPath(path)
                        connectLogTail(path)
                      }}
                      className="block w-full truncate rounded px-2 py-1 text-left text-xs text-foreground hover:bg-muted"
                      title={path}
                    >
                      {path.split('/').pop()}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>

        {recentPaths.length > 0 && (
          <div className="rounded-lg border border-border bg-card p-4">
            <h3 className="mb-3 text-sm font-semibold text-foreground">最近查看</h3>
            <div className="space-y-1">
              {recentPaths.map((path) => (
                <button
                  key={path}
                  onClick={() => {
                    setCustomPath(path)
                    connectLogTail(path)
                  }}
                  className="block w-full truncate rounded px-2 py-1 text-left text-xs text-foreground hover:bg-muted"
                  title={path}
                >
                  {path}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Main content */}
      <div className="flex flex-1 flex-col">
        {/* Controls */}
        <div className="mb-4 space-y-3 rounded-lg border border-border bg-card p-4">
          <div className="flex gap-2">
            <input
              type="text"
              value={customPath}
              onChange={(e) => setCustomPath(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  connectLogTail(customPath)
                }
              }}
              placeholder="输入日志文件路径，例如：/var/log/messages"
              className="soft-input flex-1 px-3 py-2 text-sm placeholder:text-muted-foreground"
              disabled={isConnected}
            />
            <input
              type="number"
              value={lines}
              onChange={(e) => setLines(parseInt(e.target.value) || 100)}
              min="10"
              max="1000"
              className="w-24 rounded border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
              disabled={isConnected}
              title="初始读取行数"
            />
            {!isConnected ? (
              <>
                <button
                  onClick={() => connectLogTail(customPath)}
                  className="rounded bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                  disabled={!customPath.trim()}
                >
                  开始监听
                </button>
                <button
                  onClick={() => savePathForNode(customPath)}
                  className="rounded border border-border bg-background px-3 py-2 text-sm font-medium text-foreground hover:bg-muted disabled:opacity-50"
                  disabled={!customPath.trim() || savedPaths.includes(customPath)}
                  title="保存此路径到当前节点"
                >
                  保存
                </button>
              </>
            ) : (
              <button
                onClick={disconnectLogTail}
                className="soft-button bg-danger px-4 py-2 text-sm font-medium text-white hover:bg-danger/90"
              >
                停止
              </button>
            )}
          </div>

          <div className="flex items-center gap-4">
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索日志内容..."
              className="soft-input flex-1 px-3 py-1.5 text-sm placeholder:text-muted-foreground"
            />
            <label className="flex items-center gap-2 text-sm text-foreground">
              <input
                type="checkbox"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
                className="rounded border-border"
              />
              自动滚动
            </label>
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
          <div className="mb-4 rounded-2xl border border-danger/50 bg-danger/10 p-3 text-sm text-danger">
            {error}
          </div>
        )}

        {/* Log content */}
        <div
          ref={logContainerRef}
          className="flex-1 overflow-auto rounded-lg border border-border bg-card p-4 font-mono text-xs"
        >
          {filteredLogs.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              {isConnected ? '等待日志数据...' : '请选择或输入日志文件路径开始监听'}
            </div>
          ) : (
            <div className="space-y-0.5">
              {filteredLogs.map((line, index) => (
                <div key={index} className="text-foreground">
                  {highlightText(line, searchQuery)}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Footer info */}
        <div className="mt-2 text-xs text-muted-foreground">
          共 {logContent.length} 行
          {searchQuery && ` · 筛选后 ${filteredLogs.length} 行`}
        </div>
      </div>
    </div>
  )
}
