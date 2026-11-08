import { useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'

import { createContainerExecSession, createTerminalSession } from '../api/client'
import type { DockerContainer, Node } from '../types'

type TerminalKind = 'node' | 'container'

type TerminalPageProps = {
  kind: TerminalKind
  node?: Node
  nodeID: string
  container?: DockerContainer
  containerID?: string
}

type TerminalMessage = {
  type: string
  data?: string
  error?: string
  exit_code?: number
}

const messageTypes = {
  node: {
    data: 'terminal_data',
    resize: 'terminal_resize',
    close: 'terminal_close',
    started: 'terminal_started',
    error: 'terminal_error',
    exit: 'terminal_exit'
  },
  container: {
    data: 'container_exec_data',
    resize: 'container_exec_resize',
    close: 'container_exec_close',
    started: 'container_exec_started',
    error: 'container_exec_error',
    exit: 'container_exec_exit'
  }
} as const

function cssVar(name: string, fallback: string) {
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value ? `rgb(${value})` : fallback
}

export function TerminalPage({ kind, node, nodeID, container, containerID }: TerminalPageProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [status, setStatus] = useState('正在连接...')
  const [closed, setClosed] = useState(false)

  useEffect(() => {
    const target = containerRef.current
    if (!target) return undefined
    if (kind === 'container' && !containerID) {
      setStatus('容器 ID 无效')
      return undefined
    }
    const types = messageTypes[kind]
    const terminal = new XTerm({
      cursorBlink: true,
      convertEol: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      fontSize: 13,
      theme: {
        background: cssVar('--terminal-background', '#020617'),
        foreground: cssVar('--terminal-foreground', '#d1fae5'),
        cursor: cssVar('--terminal-cursor', '#34d399')
      }
    })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(target)
    fitAddon.fit()
    terminal.writeln('MizuPanel 终端会话')
    terminal.writeln('当前命令在 Agent 服务用户权限下运行；如需提权，请在目标系统中使用 su / sudo 并输入系统密码。')
    if (kind === 'container') {
      terminal.writeln('Docker exec 权限来自 Agent 对 Docker socket 的访问权限，MizuPanel 不保存 Docker 或 root 凭据。')
    }
    terminal.writeln('')

    let socket: WebSocket | undefined
    let connected = false
    let disposed = false
    let ended = false

    const sendResize = () => {
      fitAddon.fit()
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: types.resize, cols: terminal.cols, rows: terminal.rows }))
      }
    }
    const dataSubscription = terminal.onData((data) => {
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: types.data, data: encodeTerminalData(data) }))
      }
    })
    const resizeHandler = () => sendResize()
    window.addEventListener('resize', resizeHandler)

    const openTerminal = async () => {
      try {
        const session = kind === 'node'
          ? await createTerminalSession(nodeID)
          : await createContainerExecSession(nodeID, containerID ?? '')
        if (disposed) return
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const path = kind === 'node'
          ? `/api/nodes/${encodeURIComponent(nodeID)}/terminal/ws?token=${encodeURIComponent(session.token)}`
          : `/api/nodes/${encodeURIComponent(nodeID)}/containers/${encodeURIComponent(containerID ?? '')}/exec/ws?token=${encodeURIComponent(session.token)}`
        socket = new WebSocket(`${wsProtocol}//${window.location.host}${path}`)
        socket.addEventListener('open', () => {
          connected = true
          setStatus('已连接')
          terminal.focus()
          sendResize()
        })
        socket.addEventListener('message', (event) => {
          const message = JSON.parse(event.data) as TerminalMessage
          if (message.type === types.data && message.data) {
            terminal.write(decodeTerminalData(message.data))
          }
          if (message.type === types.started) {
            setStatus('终端已启动')
            terminal.focus()
          }
          if (message.type === types.error) {
            ended = true
            setStatus(message.error || '终端连接失败')
            terminal.writeln(`\r\n${message.error || '终端连接失败'}`)
          }
          if (message.type === types.exit) {
            ended = true
            setStatus(`终端已退出，退出码 ${message.exit_code ?? 0}`)
            terminal.writeln(`\r\n终端已退出，退出码 ${message.exit_code ?? 0}`)
          }
        })
        socket.addEventListener('close', () => {
          setClosed(true)
          if (connected && !ended) {
            const closeMessage = '终端连接已关闭，Agent 可能已重启'
            setStatus(closeMessage)
            terminal.writeln('\r\n' + closeMessage)
            terminal.writeln('如果你刚刚在终端中重启或重装 Agent，这是正常现象；请等待 Agent 重新在线后打开新的终端页面。')
          }
        })
        socket.addEventListener('error', () => setStatus('终端连接异常'))
      } catch (err) {
        const message = err instanceof Error ? err.message : '终端连接异常'
        setStatus(message)
        terminal.writeln(`\r\n${message}`)
      }
    }
    void openTerminal()

    return () => {
      disposed = true
      window.removeEventListener('resize', resizeHandler)
      dataSubscription.dispose()
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: types.close }))
      }
      socket?.close()
      terminal.dispose()
    }
  }, [containerID, kind, nodeID])

  const title = kind === 'node'
    ? (node?.name || node?.hostname || nodeID)
    : (container?.name || containerID || '容器终端')
  const subtitle = kind === 'node'
    ? `${node?.hostname || '未知主机'} · ${node?.ip || '未知 IP'}`
    : `${container?.image || '未知镜像'} · 节点 ${node?.name || nodeID}`

  return (
    <main className="min-h-screen bg-background text-foreground">
      <div className="flex min-h-screen flex-col p-3 sm:p-5">
        <header className="mb-3 rounded-2xl border border-border bg-card px-4 py-3 shadow-glass">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="min-w-0">
              <h1 className="truncate font-display text-2xl font-black tracking-tight text-foreground">{title}</h1>
              <p className="mt-1 truncate text-xs font-bold text-muted-foreground">{subtitle}</p>
              <p className="mt-2 text-xs font-bold leading-5 text-warning">终端运行在 Agent 服务用户权限下；MizuPanel 不保存 root、sudo、SSH 或 Docker 凭据，也不会绕过系统权限。</p>
            </div>
            <div className="flex items-center gap-2">
              <span className="rounded-xl border border-success/30 bg-success/10 px-3 py-2 text-xs font-black text-success">{status}</span>
              <button
                type="button"
                onClick={() => window.close()}
                className="min-h-10 cursor-pointer rounded-xl border border-border bg-card px-4 text-xs font-black text-foreground transition hover:bg-muted focus:outline-none focus:ring-4 focus:ring-primary/20"
              >
                {closed ? '关闭页面' : '断开/关闭'}
              </button>
            </div>
          </div>
        </header>
        <section className="min-h-0 flex-1 overflow-hidden rounded-2xl border border-border bg-terminal shadow-glass" aria-label={kind === 'node' ? '节点终端' : '容器终端'}>
          <div ref={containerRef} className="h-full min-h-[70vh] bg-terminal p-3" />
        </section>
      </div>
    </main>
  )
}

function encodeTerminalData(value: string) {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte)
  })
  return btoa(binary)
}

function decodeTerminalData(value: string) {
  const binary = atob(value)
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}
