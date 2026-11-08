import { useEffect, useState } from 'react'
import { X } from 'lucide-react'

type RestartPolicy = 'no' | 'always' | 'on-failure' | 'unless-stopped'
type NetworkMode = 'bridge' | 'host' | 'none' | 'container'

type PortMapping = {
  hostPort: string
  containerPort: string
  protocol: 'tcp' | 'udp'
}

type VolumeMount = {
  hostPath: string
  containerPath: string
  readonly: boolean
}

type EnvVar = {
  key: string
  value: string
}

type HostMapping = {
  hostname: string
  ip: string
}

type CreateContainerModalProps = {
  open: boolean
  nodeId: string
  onClose: () => void
  onCreate?: (nodeId: string, command: string) => Promise<void>
}

export default function CreateContainerModal({ open, nodeId, onClose, onCreate }: CreateContainerModalProps) {
  const [image, setImage] = useState('')
  const [containerName, setContainerName] = useState('')
  const [restartPolicy, setRestartPolicy] = useState<RestartPolicy>('always')
  const [networkMode, setNetworkMode] = useState<NetworkMode>('bridge')
  const [portMappings, setPortMappings] = useState<PortMapping[]>([{ hostPort: '', containerPort: '', protocol: 'tcp' }])
  const [volumeMounts, setVolumeMounts] = useState<VolumeMount[]>([{ hostPath: '', containerPath: '', readonly: false }])
  const [envVars, setEnvVars] = useState<EnvVar[]>([{ key: '', value: '' }])
  const [hostMappings, setHostMappings] = useState<HostMapping[]>([{ hostname: '', ip: '' }])
  const [cpuLimit, setCpuLimit] = useState('')
  const [memoryLimit, setMemoryLimit] = useState('')
  const [detached, setDetached] = useState(true)
  const [tty, setTty] = useState(true)
  const [interactive, setInteractive] = useState(true)
  const [privileged, setPrivileged] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string>()
  const [success, setSuccess] = useState<string>()

  const generateCommand = (): string => {
    const parts: string[] = ['docker run']

    // Detached, TTY, Interactive
    if (detached) parts.push('-d')
    if (tty && interactive) parts.push('-it')
    else if (tty) parts.push('-t')
    else if (interactive) parts.push('-i')

    // Container name
    if (containerName.trim()) parts.push(`--name ${containerName.trim()}`)

    // Restart policy
    parts.push(`--restart=${restartPolicy}`)

    // Network mode
    if (networkMode !== 'bridge') parts.push(`--network ${networkMode}`)

    // Port mappings
    portMappings.forEach((pm) => {
      if (pm.hostPort.trim() && pm.containerPort.trim()) {
        parts.push(`-p ${pm.hostPort.trim()}:${pm.containerPort.trim()}/${pm.protocol}`)
      }
    })

    // Volume mounts
    volumeMounts.forEach((vm) => {
      if (vm.hostPath.trim() && vm.containerPath.trim()) {
        const mount = vm.readonly ? `${vm.hostPath.trim()}:${vm.containerPath.trim()}:ro` : `${vm.hostPath.trim()}:${vm.containerPath.trim()}`
        parts.push(`-v ${mount}`)
      }
    })

    // Environment variables
    envVars.forEach((ev) => {
      if (ev.key.trim()) {
        parts.push(`-e ${ev.key.trim()}=${ev.value}`)
      }
    })

    // Host mappings
    hostMappings.forEach((hm) => {
      if (hm.hostname.trim() && hm.ip.trim()) {
        parts.push(`--add-host ${hm.hostname.trim()}:${hm.ip.trim()}`)
      }
    })

    // Resource limits
    if (cpuLimit.trim()) parts.push(`--cpus=${cpuLimit.trim()}`)
    if (memoryLimit.trim()) parts.push(`--memory=${memoryLimit.trim()}`)

    // Privileged
    if (privileged) parts.push('--privileged')

    // Image
    parts.push(image.trim() || '<镜像名称>')

    return parts.join(' \\\n  ')
  }

  const handleCreate = async () => {
    if (!image.trim()) {
      setError('请输入镜像名称')
      return
    }

    const command = generateCommand()
    setCreating(true)
    setError(undefined)
    setSuccess(undefined)

    try {
      if (onCreate) {
        await onCreate(nodeId, command)
        setSuccess('容器创建命令已发送')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    } finally {
      setCreating(false)
    }
  }

  useEffect(() => {
    if (!open) {
      // Reset form when closed
      setImage('')
      setContainerName('')
      setRestartPolicy('always')
      setNetworkMode('bridge')
      setPortMappings([{ hostPort: '', containerPort: '', protocol: 'tcp' }])
      setVolumeMounts([{ hostPath: '', containerPath: '', readonly: false }])
      setEnvVars([{ key: '', value: '' }])
      setHostMappings([{ hostname: '', ip: '' }])
      setCpuLimit('')
      setMemoryLimit('')
      setDetached(true)
      setTty(true)
      setInteractive(true)
      setPrivileged(false)
      setCreating(false)
      setError(undefined)
      setSuccess(undefined)
    }
  }, [open])

  if (!open) return null

  return (
    <div className="soft-modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4">
      <section role="dialog" aria-modal="true" aria-label="创建容器" className="soft-modal-shell flex max-h-[90vh] w-full max-w-4xl flex-col outline-none">
        <div className="soft-modal-header flex shrink-0 items-start justify-between gap-3 border-b px-5 py-4">
          <h3 className="font-display text-2xl font-black tracking-tight text-foreground">创建容器</h3>
          <button type="button" aria-label="关闭" onClick={onClose} className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-primary/30 bg-primary/5 text-primary hover:bg-primary/10 focus:outline-none focus:ring-4 focus:ring-primary/20">
            <X size={16} aria-hidden="true" />
          </button>
        </div>

        <div className="overflow-y-auto px-5 py-4">
          {/* 1. 基础配置 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <h4 className="mb-3 text-sm font-black text-foreground">基础配置</h4>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="text-xs font-black text-foreground">
                镜像名称 *
                <input
                  type="text"
                  value={image}
                  onChange={(e) => setImage(e.target.value)}
                  placeholder="nginx:latest"
                  className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                />
              </label>
              <label className="text-xs font-black text-foreground">
                容器名称
                <input
                  type="text"
                  value={containerName}
                  onChange={(e) => setContainerName(e.target.value)}
                  placeholder="my-nginx"
                  className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                />
              </label>
              <label className="text-xs font-black text-foreground">
                重启策略
                <select
                  value={restartPolicy}
                  onChange={(e) => setRestartPolicy(e.target.value as RestartPolicy)}
                  className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                >
                  <option value="no">no - 不自动重启</option>
                  <option value="always">always - 始终重启</option>
                  <option value="on-failure">on-failure - 失败时重启</option>
                  <option value="unless-stopped">unless-stopped - 除非手动停止</option>
                </select>
              </label>
              <label className="text-xs font-black text-foreground">
                网络模式
                <select
                  value={networkMode}
                  onChange={(e) => setNetworkMode(e.target.value as NetworkMode)}
                  className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                >
                  <option value="bridge">bridge - 桥接网络</option>
                  <option value="host">host - 主机网络</option>
                  <option value="none">none - 无网络</option>
                </select>
              </label>
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              <label className="flex cursor-pointer items-center gap-2 text-xs font-bold text-foreground">
                <input type="checkbox" checked={detached} onChange={(e) => setDetached(e.target.checked)} className="h-4 w-4" />
                后台运行 (-d)
              </label>
              <label className="flex cursor-pointer items-center gap-2 text-xs font-bold text-foreground">
                <input type="checkbox" checked={tty} onChange={(e) => setTty(e.target.checked)} className="h-4 w-4" />
                分配TTY (-t)
              </label>
              <label className="flex cursor-pointer items-center gap-2 text-xs font-bold text-foreground">
                <input type="checkbox" checked={interactive} onChange={(e) => setInteractive(e.target.checked)} className="h-4 w-4" />
                交互模式 (-i)
              </label>
              <label className="flex cursor-pointer items-center gap-2 text-xs font-bold text-foreground">
                <input type="checkbox" checked={privileged} onChange={(e) => setPrivileged(e.target.checked)} className="h-4 w-4" />
                特权模式 (--privileged)
              </label>
            </div>
          </section>

          {/* 2. 端口映射 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <div className="mb-3 flex items-center justify-between">
              <h4 className="text-sm font-black text-foreground">端口映射</h4>
              <button
                type="button"
                onClick={() => setPortMappings([...portMappings, { hostPort: '', containerPort: '', protocol: 'tcp' }])}
                className="min-h-8 rounded-xl bg-primary px-3 text-xs font-black text-primary-foreground transition hover:brightness-110"
              >
                + 添加端口
              </button>
            </div>
            <div className="space-y-2">
              {portMappings.map((pm, idx) => (
                <div key={idx} className="flex gap-2">
                  <input
                    type="text"
                    value={pm.hostPort}
                    onChange={(e) => {
                      const updated = [...portMappings]
                      updated[idx].hostPort = e.target.value
                      setPortMappings(updated)
                    }}
                    placeholder="宿主端口"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  <span className="flex items-center text-sm font-black text-muted-foreground">:</span>
                  <input
                    type="text"
                    value={pm.containerPort}
                    onChange={(e) => {
                      const updated = [...portMappings]
                      updated[idx].containerPort = e.target.value
                      setPortMappings(updated)
                    }}
                    placeholder="容器端口"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  <select
                    value={pm.protocol}
                    onChange={(e) => {
                      const updated = [...portMappings]
                      updated[idx].protocol = e.target.value as 'tcp' | 'udp'
                      setPortMappings(updated)
                    }}
                    className="min-h-10 w-20 rounded-2xl border border-border bg-card px-2 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  >
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                  </select>
                  {portMappings.length > 1 && (
                    <button
                      type="button"
                      onClick={() => setPortMappings(portMappings.filter((_, i) => i !== idx))}
                      className="min-h-10 rounded-2xl border border-danger/30 bg-danger/10 px-3 text-xs font-black text-danger transition hover:bg-danger/15"
                    >
                      删除
                    </button>
                  )}
                </div>
              ))}
            </div>
          </section>

          {/* 3. 卷挂载 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <div className="mb-3 flex items-center justify-between">
              <h4 className="text-sm font-black text-foreground">卷挂载</h4>
              <button
                type="button"
                onClick={() => setVolumeMounts([...volumeMounts, { hostPath: '', containerPath: '', readonly: false }])}
                className="min-h-8 rounded-xl bg-primary px-3 text-xs font-black text-primary-foreground transition hover:brightness-110"
              >
                + 添加挂载
              </button>
            </div>
            <div className="space-y-2">
              {volumeMounts.map((vm, idx) => (
                <div key={idx} className="flex gap-2">
                  <input
                    type="text"
                    value={vm.hostPath}
                    onChange={(e) => {
                      const updated = [...volumeMounts]
                      updated[idx].hostPath = e.target.value
                      setVolumeMounts(updated)
                    }}
                    placeholder="宿主路径 /path/on/host"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  <span className="flex items-center text-sm font-black text-muted-foreground">:</span>
                  <input
                    type="text"
                    value={vm.containerPath}
                    onChange={(e) => {
                      const updated = [...volumeMounts]
                      updated[idx].containerPath = e.target.value
                      setVolumeMounts(updated)
                    }}
                    placeholder="容器路径 /path/in/container"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  <label className="flex cursor-pointer items-center gap-2 whitespace-nowrap rounded-2xl border border-border bg-card px-3 text-xs font-bold text-foreground">
                    <input
                      type="checkbox"
                      checked={vm.readonly}
                      onChange={(e) => {
                        const updated = [...volumeMounts]
                        updated[idx].readonly = e.target.checked
                        setVolumeMounts(updated)
                      }}
                      className="h-4 w-4"
                    />
                    只读
                  </label>
                  {volumeMounts.length > 1 && (
                    <button
                      type="button"
                      onClick={() => setVolumeMounts(volumeMounts.filter((_, i) => i !== idx))}
                      className="min-h-10 rounded-2xl border border-danger/30 bg-danger/10 px-3 text-xs font-black text-danger transition hover:bg-danger/15"
                    >
                      删除
                    </button>
                  )}
                </div>
              ))}
            </div>
          </section>

          {/* 4. 环境变量 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <div className="mb-3 flex items-center justify-between">
              <h4 className="text-sm font-black text-foreground">环境变量</h4>
              <button
                type="button"
                onClick={() => setEnvVars([...envVars, { key: '', value: '' }])}
                className="min-h-8 rounded-xl bg-primary px-3 text-xs font-black text-primary-foreground transition hover:brightness-110"
              >
                + 添加变量
              </button>
            </div>
            <div className="space-y-2">
              {envVars.map((ev, idx) => (
                <div key={idx} className="flex gap-2">
                  <input
                    type="text"
                    value={ev.key}
                    onChange={(e) => {
                      const updated = [...envVars]
                      updated[idx].key = e.target.value
                      setEnvVars(updated)
                    }}
                    placeholder="变量名 KEY"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  <span className="flex items-center text-sm font-black text-muted-foreground">=</span>
                  <input
                    type="text"
                    value={ev.value}
                    onChange={(e) => {
                      const updated = [...envVars]
                      updated[idx].value = e.target.value
                      setEnvVars(updated)
                    }}
                    placeholder="变量值 value"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  {envVars.length > 1 && (
                    <button
                      type="button"
                      onClick={() => setEnvVars(envVars.filter((_, i) => i !== idx))}
                      className="min-h-10 rounded-2xl border border-danger/30 bg-danger/10 px-3 text-xs font-black text-danger transition hover:bg-danger/15"
                    >
                      删除
                    </button>
                  )}
                </div>
              ))}
            </div>
          </section>

          {/* 5. 网络配置 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <div className="mb-3 flex items-center justify-between">
              <h4 className="text-sm font-black text-foreground">Host 映射</h4>
              <button
                type="button"
                onClick={() => setHostMappings([...hostMappings, { hostname: '', ip: '' }])}
                className="min-h-8 rounded-xl bg-primary px-3 text-xs font-black text-primary-foreground transition hover:brightness-110"
              >
                + 添加映射
              </button>
            </div>
            <div className="space-y-2">
              {hostMappings.map((hm, idx) => (
                <div key={idx} className="flex gap-2">
                  <input
                    type="text"
                    value={hm.hostname}
                    onChange={(e) => {
                      const updated = [...hostMappings]
                      updated[idx].hostname = e.target.value
                      setHostMappings(updated)
                    }}
                    placeholder="域名 example.com"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  <span className="flex items-center text-sm font-black text-muted-foreground">:</span>
                  <input
                    type="text"
                    value={hm.ip}
                    onChange={(e) => {
                      const updated = [...hostMappings]
                      updated[idx].ip = e.target.value
                      setHostMappings(updated)
                    }}
                    placeholder="IP 地址 192.168.1.1"
                    className="min-h-10 flex-1 rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                  />
                  {hostMappings.length > 1 && (
                    <button
                      type="button"
                      onClick={() => setHostMappings(hostMappings.filter((_, i) => i !== idx))}
                      className="min-h-10 rounded-2xl border border-danger/30 bg-danger/10 px-3 text-xs font-black text-danger transition hover:bg-danger/15"
                    >
                      删除
                    </button>
                  )}
                </div>
              ))}
            </div>
          </section>

          {/* 6. 资源限制 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <h4 className="mb-3 text-sm font-black text-foreground">资源限制（可选）</h4>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="text-xs font-black text-foreground">
                CPU 限制
                <input
                  type="text"
                  value={cpuLimit}
                  onChange={(e) => setCpuLimit(e.target.value)}
                  placeholder="例如: 0.5, 2"
                  className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                />
              </label>
              <label className="text-xs font-black text-foreground">
                内存限制
                <input
                  type="text"
                  value={memoryLimit}
                  onChange={(e) => setMemoryLimit(e.target.value)}
                  placeholder="例如: 512m, 2g"
                  className="mt-1 min-h-10 w-full rounded-2xl border border-border bg-card px-3 text-sm font-bold text-foreground outline-none focus:border-primary focus:ring-4 focus:ring-primary/20"
                />
              </label>
            </div>
          </section>

          {/* 7. 命令预览 */}
          <section className="mb-4 rounded-2xl border border-border bg-surface p-4">
            <h4 className="mb-3 text-sm font-black text-foreground">命令预览</h4>
            <pre className="overflow-x-auto rounded-2xl bg-slate-950 p-4 font-mono text-xs leading-6 text-slate-100">{generateCommand()}</pre>
          </section>

          {/* Messages */}
          {error && (
            <div className="mb-4 rounded-2xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm font-black text-danger">
              {error}
            </div>
          )}
          {success && (
            <div className="mb-4 rounded-2xl border border-success/30 bg-success/10 px-4 py-3 text-sm font-black text-success">
              {success}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="soft-modal-footer flex shrink-0 flex-wrap justify-end gap-2 border-t px-5 py-4">
          <button
            type="button"
            onClick={onClose}
            disabled={creating}
            className="soft-button min-h-11 cursor-pointer border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-60"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleCreate}
            disabled={creating || !image.trim()}
            className="soft-button min-h-11 cursor-pointer bg-primary px-4 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 focus:outline-none focus:ring-4 focus:ring-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {creating ? '创建中...' : '创建容器'}
          </button>
        </div>
      </section>
    </div>
  )
}
