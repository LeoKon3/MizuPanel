import { afterEach, describe, expect, test, vi } from 'vitest'

import { createContainerExecSession, createInstallCommand, createNodeGroup, createNodeTag, createTerminalSession, deleteAlertHistories, deleteAlertHistory, deleteNode, deleteNodeGroup, deleteNodePath, deleteNodeTag, getAgentLogs, getAgentStatus, getAuthSession, getNodeDocker, getNodeDockerResources, getNodeFiles, getNodeGroups, getNodeMetrics, getNodeProcesses, getNodeTags, getNodes, getSettings, getSystemAbout, login, logout, readNodeFile, rebootNode, resolveAlertHistory, restartAgent, runNodeDockerComposeDeployment, runNodeDockerResourceAction, startSSHInstall, startSSHUninstall, updateBatchNodeMetadata, updateNodeGroup, updateNodeTag, updateSettings, uploadNodeFile, writeNodeFile } from './client'
import { checkUptimeMonitor, createUptimeMonitor, deleteUptimeMonitor, getUptimeIncidents, getUptimeMonitors, getUptimeResults, toggleUptimeMonitor, updateUptimeMonitor } from './client'
import { createAutomationScript, createScheduledTask, deleteAutomationScript, deleteScheduledTask, getAutomationRun, getAutomationRuns, getAutomationScripts, getScheduledTasks, runAutomationScript, runScheduledTask, toggleScheduledTask, updateAutomationScript, updateScheduledTask } from './client'
import { createApplicationService, deleteApplicationService, getApplicationService, getApplicationServices, updateApplicationService } from './client'
import { confirmAIToolCall, createAIConversation, createAIProvider, deleteAIConversation, deleteAIModel, deleteAIProvider, discoverAIProvider, getAIConversation, getAIConversations, getAIModel, getAIProviderModels, getAIProviders, getAIRouting, importAIProviderModels, listAIProviderModels, rejectAIToolCall, renameAIConversation, sendAIMessage, sendAIMessageStream, setDefaultAIProvider, testAIModel, testAIProvider, updateAIConversationModel, updateAIModel, updateAIProvider, updateAIRouting } from './client'

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

  test('manages node groups, tags, and batch metadata', async () => {
    const group = { id: 'group-1', name: 'Production', node_count: 0, created_at: '', updated_at: '' }
    const tag = { id: 'tag-1', name: 'Database', color: 'blue', node_count: 0, created_at: '', updated_at: '' }
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ groups: [group] })))
      .mockResolvedValueOnce(new Response(JSON.stringify(group)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...group, name: 'Primary' })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ tags: [tag] })))
      .mockResolvedValueOnce(new Response(JSON.stringify(tag)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...tag, name: 'Critical DB', color: 'red' })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ nodes: { 'node-1': { group: null, tags: [] } } })))

    await getNodeGroups()
    await createNodeGroup('Production')
    await updateNodeGroup('group 1', 'Primary')
    await deleteNodeGroup('group 1')
    await getNodeTags()
    await createNodeTag('Database', 'blue')
    await updateNodeTag('tag 1', 'Critical DB', 'red')
    await deleteNodeTag('tag 1')
    await updateBatchNodeMetadata({ node_ids: ['node-1'], group_id: null, add_tag_ids: [], remove_tag_ids: [] })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/node-groups')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/node-groups', expect.objectContaining({ method: 'POST', body: JSON.stringify({ name: 'Production' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/node-groups/group%201', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ name: 'Primary' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/node-groups/group%201', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/node-tags')
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/node-tags', expect.objectContaining({ method: 'POST', body: JSON.stringify({ name: 'Database', color: 'blue' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/node-tags/tag%201', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ name: 'Critical DB', color: 'red' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/node-tags/tag%201', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/nodes/batch/metadata', expect.objectContaining({ method: 'PATCH' }))
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

  test('lists Docker resources and sends fixed resource actions', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ success: true, supported: true, usage: {}, images: [], volumes: [], networks: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ success: true, supported: true })))

    await getNodeDockerResources('node 1')
    await runNodeDockerResourceAction('node 1', 'image', 'nginx:latest', 'pull')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/node%201/docker/resources')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/node%201/docker/resources/action', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resource_type: 'image', resource_id: 'nginx:latest', action: 'pull' })
    })
  })

  test('sends managed Compose deployment drafts to the bounded deployment endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ success: true, supported: true, action: 'preview', risks: [] })))
    const request = {
      action: 'preview' as const,
      project_id: 'managed-1',
      display_name: '网站前台',
      compose_yaml: 'services:\n  web:\n    image: nginx:alpine\n',
      env_file: 'API_TOKEN=temporary-secret\n',
      pull_images: false,
      confirmation_token: 'preview-token',
    }

    await runNodeDockerComposeDeployment('node 1', request)

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/node%201/docker/compose/deployment', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request)
    })
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

  test('manages Uptime monitors and history through the typed endpoints', async () => {
    const monitor = { id: 7, name: 'Website', type: 'http', target: 'https://example.com' }
    const input = {
      name: 'Website', type: 'http' as const, target: 'https://example.com', interval_seconds: 60,
      timeout_seconds: 5, failure_threshold: 3, expected_status_min: 200, expected_status_max: 399,
      tls_expiry_threshold_days: 30, notification_channels: []
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ monitors: [monitor] })))
      .mockResolvedValueOnce(new Response(JSON.stringify(monitor)))
      .mockResolvedValueOnce(new Response(JSON.stringify(monitor)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...monitor, enabled: false })))
      .mockResolvedValueOnce(new Response(JSON.stringify(monitor)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ results: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ incidents: [] })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))

    await getUptimeMonitors()
    await createUptimeMonitor(input)
    await updateUptimeMonitor(7, input)
    await toggleUptimeMonitor(7, false)
    await checkUptimeMonitor(7)
    await getUptimeResults(7, 25)
    await getUptimeIncidents(7, 10)
    await deleteUptimeMonitor(7)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/uptime/monitors')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/uptime/monitors', expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }))
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/uptime/monitors/7', expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/uptime/monitors/7/toggle', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ enabled: false }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/uptime/monitors/7/check', { method: 'POST' })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/uptime/monitors/7/results?limit=25')
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/uptime/monitors/7/incidents?limit=10')
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/uptime/monitors/7', { method: 'DELETE' })
  })

  test('manages automation scripts, schedules, and keyset run history through typed endpoints', async () => {
    const scriptInput = { name: 'Cleanup', description: 'Clear cache', content: 'echo done', timeout_seconds: 300 }
    const script = { id: 7, ...scriptInput, revision: 1, created_at: '', updated_at: '' }
    const taskInput = {
      name: 'Nightly cleanup', script_id: 7, node_ids: ['node-1'], cron_expression: '0 2 * * *', timezone: 'Asia/Shanghai',
      timeout_seconds: 300, notification_policy: 'failure' as const, notification_channels: []
    }
    const task = {
      id: 9, ...taskInput, script_name: 'Cleanup', script_revision: 1, enabled: true, next_run_at: null,
      last_scheduled_at: null, created_at: '', updated_at: ''
    }
    const run = {
      id: 11, task_name: '', script_id: 7, script_name: 'Cleanup', script_revision: 1,
      trigger: 'manual', status: 'queued', total_targets: 1, completed_targets: 0, success_targets: 0,
      failed_targets: 0, notification_sent: false, created_at: ''
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ scripts: [script] })))
      .mockResolvedValueOnce(new Response(JSON.stringify(script)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...script, revision: 2 })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(run), { status: 202 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ tasks: [task] })))
      .mockResolvedValueOnce(new Response(JSON.stringify(task)))
      .mockResolvedValueOnce(new Response(JSON.stringify(task)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...task, enabled: false })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...run, task_id: 9, task_name: 'Nightly cleanup' }), { status: 202 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ runs: [run], next_before_id: 10 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...run, targets: [] })))

    await getAutomationScripts()
    await createAutomationScript(scriptInput)
    await updateAutomationScript(7, scriptInput)
    await deleteAutomationScript(7)
    await runAutomationScript(7, ['node-1'])
    await getScheduledTasks()
    await createScheduledTask(taskInput)
    await updateScheduledTask(9, taskInput)
    await toggleScheduledTask(9, false)
    await runScheduledTask(9)
    await deleteScheduledTask(9)
    await getAutomationRuns({ before_id: 42, limit: 25, status: 'failed', node_id: 'node/1' })
    await getAutomationRun(11)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/automation/scripts')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/automation/scripts', expect.objectContaining({ method: 'POST', body: JSON.stringify(scriptInput) }))
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/automation/scripts/7', expect.objectContaining({ method: 'PUT', body: JSON.stringify(scriptInput) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/automation/scripts/7', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/automation/scripts/7/runs', expect.objectContaining({ method: 'POST', body: JSON.stringify({ node_ids: ['node-1'] }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/automation/tasks')
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/automation/tasks', expect.objectContaining({ method: 'POST', body: JSON.stringify(taskInput) }))
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/automation/tasks/9', expect.objectContaining({ method: 'PUT', body: JSON.stringify(taskInput) }))
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/automation/tasks/9/toggle', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ enabled: false }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/automation/tasks/9/runs', { method: 'POST' })
    expect(fetchMock).toHaveBeenNthCalledWith(11, '/api/automation/tasks/9', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(12, '/api/automation/runs?before_id=42&limit=25&status=failed&node_id=node%2F1')
    expect(fetchMock).toHaveBeenNthCalledWith(13, '/api/automation/runs/11')
  })

  test('manages application services through encoded typed endpoints', async () => {
    const input = {
      name: 'Panel',
      description: 'Internal panel',
      resources: [{ resource_type: 'node' as const, scope_id: '', resource_kind: '', namespace: '', resource_key: 'node-1', display_name: 'Node One' }]
    }
    const service = { id: 'service/1', ...input, health: 'healthy', resources: [] }
    const controller = new AbortController()
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify([service])))
      .mockResolvedValueOnce(new Response(JSON.stringify(service)))
      .mockResolvedValueOnce(new Response(JSON.stringify(service)))
      .mockResolvedValueOnce(new Response(JSON.stringify(service)))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))

    await getApplicationServices(controller.signal)
    await getApplicationService('service/1', controller.signal)
    await createApplicationService(input)
    await updateApplicationService('service/1', input)
    await deleteApplicationService('service/1')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/services', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/services/service%2F1', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/services', expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/services/service%2F1', expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) }))
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/services/service%2F1', { method: 'DELETE' })
  })

  test('manages AI providers and conversations through bounded typed endpoints', async () => {
    const providerInput = {
      name: 'Internal model',
      protocol: 'openai_chat_completions' as const,
      base_url: 'http://model.internal/v1',
      model: 'ops-model',
      api_key: 'temporary-secret'
    }
    const controller = new AbortController()
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ providers: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'provider/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'provider/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'provider/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'provider/1' })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ conversations: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'conversation/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'conversation/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ conversation: { id: 'conversation/1' }, messages: [], tool_calls: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ turn: { id: 'turn-1' } })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ turn: { id: 'turn-1' } })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ turn: { id: 'turn-1' } })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))

    await getAIProviders(controller.signal)
    await createAIProvider(providerInput)
    await updateAIProvider('provider/1', { ...providerInput, api_key: undefined })
    await testAIProvider('provider/1', controller.signal)
    await setDefaultAIProvider('provider/1')
    await deleteAIProvider('provider/1')
    await getAIConversations(25, controller.signal)
    await createAIConversation('Production checks')
    await renameAIConversation('conversation/1', 'Renamed checks')
    await getAIConversation('conversation/1', 100, controller.signal)
    await sendAIMessage('conversation/1', 'Check active alerts', controller.signal)
    await confirmAIToolCall('tool/1')
    await rejectAIToolCall('tool/1')
    await deleteAIConversation('conversation/1')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/ai/providers', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/ai/providers', expect.objectContaining({ method: 'POST', body: JSON.stringify(providerInput) }))
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/ai/providers/provider%2F1', expect.objectContaining({ method: 'PUT', body: JSON.stringify({ ...providerInput, api_key: undefined }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/ai/providers/provider%2F1/test', { method: 'POST', signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/ai/providers/provider%2F1/default', { method: 'POST' })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/ai/providers/provider%2F1', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/ai/conversations?limit=25', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/ai/conversations', expect.objectContaining({ method: 'POST', body: JSON.stringify({ title: 'Production checks' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/ai/conversations/conversation%2F1', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ title: 'Renamed checks' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/ai/conversations/conversation%2F1?limit=100', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(11, '/api/ai/conversations/conversation%2F1/messages', expect.objectContaining({ method: 'POST', body: JSON.stringify({ content: 'Check active alerts' }), signal: controller.signal }))
    expect(fetchMock).toHaveBeenNthCalledWith(12, '/api/ai/tool-calls/tool%2F1/confirm', { method: 'POST' })
    expect(fetchMock).toHaveBeenNthCalledWith(13, '/api/ai/tool-calls/tool%2F1/reject', { method: 'POST' })
    expect(fetchMock).toHaveBeenNthCalledWith(14, '/api/ai/conversations/conversation%2F1', { method: 'DELETE' })
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


  function sseResponse(chunks: string[]): Response {
    const encoder = new TextEncoder()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
        controller.close()
      }
    })
    return new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } })
  }

  test('sendAIMessageStream parses status events and returns the result', async () => {
    const events = [
      'event: status\ndata: {"phase":"model"}\n\n',
      'event: delta\ndata: {"turn_id":"turn-1","content":"first"}\n\n',
      'event: reset\ndata: {"turn_id":"turn-1","reason":"fallback"}\n\n',
      'event: delta\ndata: {"turn_id":"turn-1","content":"second"}\n\n',
      'event: status\ndata: {"phase":"fallback","provider_name":"Backup","model":"model-b"}\n\n',
      'event: status\ndata: {"phase":"tool","tool_name":"reboot_node","target_name":"node-1"}\n\n',
      'event: result\ndata: {"turn":{"id":"turn-1"}}\n\n'
    ]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse(events))
    const onProgress = vi.fn()
    const onDelta = vi.fn()
    const onReset = vi.fn()
    const result = await sendAIMessageStream('conv/1', 'reboot', undefined, onProgress, {
      context: { page: 'hosts', resource_type: 'node', resource_id: 'node-1' },
      onDelta,
      onReset
    })
    expect(result.turn.id).toBe('turn-1')
    expect(onProgress).toHaveBeenCalledTimes(3)
    expect(onProgress).toHaveBeenNthCalledWith(1, { phase: 'model' })
    expect(onProgress).toHaveBeenNthCalledWith(2, { phase: 'fallback', provider_name: 'Backup', model: 'model-b' })
    expect(onProgress).toHaveBeenNthCalledWith(3, { phase: 'tool', tool_name: 'reboot_node', target_name: 'node-1' })
    expect(onDelta).toHaveBeenNthCalledWith(1, { turn_id: 'turn-1', content: 'first' })
    expect(onDelta).toHaveBeenNthCalledWith(2, { turn_id: 'turn-1', content: 'second' })
    expect(onReset).toHaveBeenCalledWith({ turn_id: 'turn-1', reason: 'fallback' })
    expect(fetch).toHaveBeenCalledWith('/api/ai/conversations/conv%2F1/messages/stream', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ content: 'reboot', context: { page: 'hosts', resource_type: 'node', resource_id: 'node-1' } })
    }))
  })

  test('sendAIMessageStream throws on error events', async () => {
    const events = ['event: error\ndata: {"error":"provider unavailable"}\n\n']
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse(events))
    await expect(sendAIMessageStream('conv/1', 'hi', undefined, () => {}))
      .rejects.toThrow('provider unavailable')
  })

  test('sendAIMessageStream throws when no result event is returned', async () => {
    const events = ['event: status\ndata: {"phase":"model"}\n\n']
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse(events))
    await expect(sendAIMessageStream('conv/1', 'hi', undefined, () => {}))
      .rejects.toThrow('流式响应未返回结果')
  })

  test('sendAIMessageStream throws on non-ok response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: 'forbidden' }), { status: 403 }))
    await expect(sendAIMessageStream('conv/1', 'hi', undefined, () => {}))
      .rejects.toThrow('forbidden')
  })

  test('lists provider models using the entered Base URL and API key', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ models: ['model-a', 'model-b'] })))

    await expect(listAIProviderModels('https://model.test/v1', 'key-marker', 'provider-1')).resolves.toEqual(['model-a', 'model-b'])
    expect(fetchMock).toHaveBeenCalledWith('/api/ai/providers/models', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider_id: 'provider-1', base_url: 'https://model.test/v1', api_key: 'key-marker' })
    })
  })

  test('manages saved provider models, routing, and conversation selection with exact payloads', async () => {
    const controller = new AbortController()
    const modelInput = { model_id: 'model-a', display_name: 'Primary' }
    const modelUpdate = { model_id: 'model-a', display_name: 'Primary', enabled: false }
    const routing = { default_model_id: 'model/1', fallback_model_id: null }
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ provider: { id: 'provider/1' }, models: ['model-a'] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ models: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ models: [{ id: 'model/1' }] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'model/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'model/1' })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'model/1' })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(routing)))
      .mockResolvedValueOnce(new Response(JSON.stringify(routing)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'conversation/1', model_id: 'model/1' })))

    await discoverAIProvider('provider/1', controller.signal)
    await getAIProviderModels('provider/1', controller.signal)
    await importAIProviderModels('provider/1', [modelInput], false)
    await getAIModel('model/1', controller.signal)
    await updateAIModel('model/1', modelUpdate)
    await testAIModel('model/1', controller.signal)
    await deleteAIModel('model/1')
    await getAIRouting(controller.signal)
    await updateAIRouting(routing)
    await updateAIConversationModel('conversation/1', 'model/1', controller.signal)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/ai/providers/provider%2F1/discover', { method: 'POST', signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/ai/providers/provider%2F1/models', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/ai/providers/provider%2F1/models', expect.objectContaining({ method: 'POST', body: JSON.stringify({ models: [modelInput], enabled: false }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/ai/models/model%2F1', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/ai/models/model%2F1', expect.objectContaining({ method: 'PUT', body: JSON.stringify(modelUpdate) }))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/ai/models/model%2F1/test', { method: 'POST', signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/ai/models/model%2F1', { method: 'DELETE' })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/ai/routing', { signal: controller.signal })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/ai/routing', expect.objectContaining({ method: 'PUT', body: JSON.stringify(routing) }))
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/ai/conversations/conversation%2F1', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ model_id: 'model/1' }), signal: controller.signal }))
  })
})
