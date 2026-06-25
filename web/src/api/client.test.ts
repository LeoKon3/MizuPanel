import { afterEach, describe, expect, test, vi } from 'vitest'

import { createContainerExecSession, createInstallCommand, createTerminalSession, deleteAlertHistories, deleteAlertHistory, deleteNode, deleteNodePath, getAgentLogs, getAgentStatus, getAuthSession, getNodeDocker, getNodeFiles, getNodeMetrics, getNodeProcesses, getNodes, getSettings, getSystemAbout, login, logout, readNodeFile, rebootNode, resolveAlertHistory, restartAgent, startSSHInstall, startSSHUninstall, updateSettings, uploadNodeFile, writeNodeFile } from './client'

describe('api client', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  test('fetches auth session and sends login/logout requests', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ auth_enabled: true, authenticated: false, username: '' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ authenticated: true, username: 'admin' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true })))

    const session = await getAuthSession()
    const loginResponse = await login('admin', 'secret')
    await logout()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/auth/session')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: 'admin', password: 'secret' })
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/auth/logout', { method: 'POST' })
    expect(session.auth_enabled).toBe(true)
    expect(session.authenticated).toBe(false)
    expect(loginResponse.username).toBe('admin')
  })

  test('fetches nodes from the REST API', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ nodes: [] })))

    const result = await getNodes()

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes')
    expect(result.nodes).toEqual([])
  })

  test('deletes node records with an empty response', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))

    await deleteNode('node 1')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201', { method: 'DELETE' })
  })

  test('fetches node metrics with a supported range', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ metrics: [] })))

    await getNodeMetrics('node-1', '7d')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node-1/metrics?range=7d')
  })

  test('fetches and updates system settings', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ metrics_retention: '6h', metrics_retention_seconds: 21600, max_metrics_retention: '7d' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ metrics_retention: '24h', metrics_retention_seconds: 86400, max_metrics_retention: '7d' })))

    const current = await getSettings()
    const updated = await updateSettings({ metrics_retention: '24h' })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/settings')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ metrics_retention: '24h' })
    })
    expect(current.metrics_retention).toBe('6h')
    expect(updated.metrics_retention).toBe('24h')
  })

  test('fetches system about information', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      version: '0.1.0',
      github_url: 'https://github.com/LeoKon3/MizuPanel'
    })))

    const about = await getSystemAbout()

    expect(fetchMock).toHaveBeenCalledWith('/api/system/about')
    expect(about.version).toBe('0.1.0')
    expect(about.github_url).toBe('https://github.com/LeoKon3/MizuPanel')
  })

  test('creates linux install commands without a session request header', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ command: 'install', install_token: 'token' })))

    const result = await createInstallCommand('linux')

    expect(fetchMock).toHaveBeenCalledWith('/api/install/command?platform=linux', { method: 'POST' })
    expect(result.command).toBe('install')
  })

  test('does not expose linux install strategy options through query params', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ command: 'install --mode ops --enable-docker --enable-terminal', install_token: 'token' })))

    const result = await createInstallCommand('linux', { enableDocker: false, enableTerminal: false, mode: 'normal' })

    expect(fetchMock).toHaveBeenCalledWith('/api/install/command?platform=linux', { method: 'POST' })
    expect(result.command).toBe('install --mode ops --enable-docker --enable-terminal')
  })

  test('creates windows install commands', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ command: 'install-windows', install_token: 'token' })))

    const result = await createInstallCommand('windows')

    expect(fetchMock).toHaveBeenCalledWith('/api/install/command?platform=windows', { method: 'POST' })
    expect(result.command).toBe('install-windows')
  })

  test('does not send linux install strategy options for windows install commands', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ command: 'install-windows', install_token: 'token' })))

    await createInstallCommand('windows', { enableDocker: true, enableTerminal: true, mode: 'ops' })

    expect(fetchMock).toHaveBeenCalledWith('/api/install/command?platform=windows', { method: 'POST' })
  })

  test('fetches node process snapshots', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ node_id: 'node-1', collected_at: 0, error: '', processes: [] })))

    const result = await getNodeProcesses('node 1')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/processes')
    expect(result.processes).toEqual([])
  })

  test('fetches node Docker snapshots', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ node_id: 'node-1', collected_at: 0, available: false, error: '', containers: [] })))

    const result = await getNodeDocker('node 1')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/docker')
    expect(result.available).toBe(false)
  })

  test('fetches node files and mutates file content', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ path: '/etc', entries: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ path: '/etc/app.conf', content: 'a=1\n', editable: true })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ path: '/etc/app.conf', saved: true })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ path: '/etc/upload.bin', uploaded: true })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ path: '/etc/upload.bin', deleted: true })))

    await getNodeFiles('node 1', '/etc')
    await readNodeFile('node 1', '/etc/app.conf')
    await writeNodeFile('node 1', '/etc/app.conf', 'a=2\n')
    await uploadNodeFile('node 1', '/etc/upload.bin', 'AAEC')
    await deleteNodePath('node 1', '/etc/upload.bin')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/node%201/files?path=%2Fetc')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/node%201/files/content?path=%2Fetc%2Fapp.conf')
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/node%201/files/content', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '/etc/app.conf', content: 'a=2\n' })
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/nodes/node%201/files/upload', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '/etc/upload.bin', content_base64: 'AAEC' })
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/node%201/files/content', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '/etc/upload.bin' })
    })
  })

  test('sends node reboot requests', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ accepted: true })))

    const result = await rebootNode('node 1')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/reboot', { method: 'POST' })
    expect(result.accepted).toBe(true)
  })

  test('fetches Agent management status, restart and recent logs', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ version: '0.1.0', user: 'root', mode: 'ops', terminal_enabled: true, docker_available: true, service_name: 'mizupanel-agent', uptime: 3600, collected_at: 1710000000 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ accepted: true, message: '重启命令已下发，等待 Agent 重新连接' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ lines: 100, content: 'mizupanel-agent started', collected_at: 1710000001 })))

    const status = await getAgentStatus('node/1')
    const restart = await restartAgent('node/1')
    const logs = await getAgentLogs('node/1', 100)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/node%2F1/agent/status')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/node%2F1/agent/restart', { method: 'POST' })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/node%2F1/agent/logs?lines=100')
    expect(status.user).toBe('root')
    expect(restart.accepted).toBe(true)
    expect(logs.content).toContain('started')
  })

  test('starts SSH install jobs', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ job_id: 'ssh-install-1' }), { status: 202 }))

    const result = await startSSHInstall({
      host: '192.168.1.10',
      port: 22,
      username: 'root',
      auth_type: 'password',
      password: 'secret',
      node_id: 'node-1',
      name: 'Node 1',
      enable_terminal: true,
      enable_docker: true,
      mode: 'ops'
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/install/ssh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        host: '192.168.1.10',
        port: 22,
        username: 'root',
        auth_type: 'password',
        password: 'secret',
        node_id: 'node-1',
        name: 'Node 1',
        enable_terminal: true,
        enable_docker: true,
        mode: 'ops'
      })
    })
    expect(result.job_id).toBe('ssh-install-1')
  })

  test('starts SSH uninstall jobs', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ job_id: 'ssh-uninstall-1' }), { status: 202 }))

    const result = await startSSHUninstall('node 1', {
      host: '192.168.1.10',
      port: 22,
      username: 'root',
      auth_type: 'private_key',
      private_key: 'key',
      remove_node_record: true
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/ssh-uninstall', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        host: '192.168.1.10',
        port: 22,
        username: 'root',
        auth_type: 'private_key',
        private_key: 'key',
        remove_node_record: true
      })
    })
    expect(result.job_id).toBe('ssh-uninstall-1')
  })

  test('creates terminal session tokens', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ token: 'terminal-token' })))

    const result = await createTerminalSession('node 1')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/terminal/session', { method: 'POST' })
    expect(result.token).toBe('terminal-token')
  })

  test('creates container exec session tokens', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ token: 'exec-token' })))

    const result = await createContainerExecSession('node 1', 'container/1')

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/containers/container%2F1/exec/session', { method: 'POST' })
    expect(result.token).toBe('exec-token')
  })

  test('resolves alert history records', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ id: 42, resolved_at: '2026-06-25T10:00:00Z' })))

    const result = await resolveAlertHistory(42)

    expect(fetchMock).toHaveBeenCalledWith('/api/alerts/history/42/resolve', { method: 'PATCH' })
    expect(result.id).toBe(42)
  })

  test('deletes alert history records', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ deleted: 2 })))

    await deleteAlertHistory(42)
    await deleteAlertHistories([42, 43])

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/alerts/history/42', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/alerts/history', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids: [42, 43] })
    })
  })

  test('marks unauthorized API responses', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('unauthorized', { status: 401 }))

    await expect(createInstallCommand()).rejects.toMatchObject({ status: 401 })
  })

  test('uses API error body messages when requests fail', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: 'Agent 离线，无法执行管理操作。' }), { status: 503 }))

    await expect(getAgentStatus('node-1')).rejects.toThrow('Agent 离线，无法执行管理操作。')
  })

  test('throws when the API response is not ok', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('bad', { status: 500 }))

    await expect(getNodes()).rejects.toThrow('Request failed')
  })
})
