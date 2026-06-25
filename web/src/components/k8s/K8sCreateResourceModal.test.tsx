import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { applyK8sManifest } from '../../api/k8s'
import { K8sCreateResourceModal } from './K8sCreateResourceModal'

vi.mock('../../api/k8s', () => ({
  applyK8sManifest: vi.fn(),
}))

function renderModal(overrides: Partial<Parameters<typeof K8sCreateResourceModal>[0]> = {}) {
  return render(
    <K8sCreateResourceModal
      open
      clusterId="cluster-1"
      currentNamespace="payments"
      namespaces={[
        { name: 'default', status: 'Active', age: '10d' },
        { name: 'payments', status: 'Active', age: '8d' },
      ]}
      onClose={vi.fn()}
      onCreated={vi.fn()}
      onToast={vi.fn()}
      {...overrides}
    />
  )
}

describe('K8sCreateResourceModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(applyK8sManifest).mockResolvedValue({ success: true, message: 'ok' })
  })

  test('defaults the namespace from the current resource filter and previews Deployment YAML', () => {
    renderModal()

    expect(screen.getByRole('dialog', { name: '创建 Kubernetes 资源' })).toBeInTheDocument()
    expect(screen.getByLabelText('目标命名空间')).toHaveValue('payments')
    const preview = screen.getByLabelText('YAML 预览') as HTMLTextAreaElement
    expect(preview.value).toContain('kind: Deployment')
    expect(preview.value).toContain('namespace: payments')
  })

  test('requires a successful dry run before creating resources', async () => {
    const onCreated = vi.fn()
    const onToast = vi.fn()
    renderModal({ onCreated, onToast })

    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'web' } })
    fireEvent.change(screen.getByLabelText('镜像'), { target: { value: 'nginx:1.27' } })
    const createButton = screen.getByRole('button', { name: '创建资源' })
    expect(createButton).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Dry Run' }))

    await waitFor(() => {
      expect(applyK8sManifest).toHaveBeenCalledWith('cluster-1', expect.objectContaining({
        dry_run: true,
        yaml: expect.stringContaining('name: web'),
      }))
    })
    await waitFor(() => expect(createButton).not.toBeDisabled())

    fireEvent.click(createButton)

    await waitFor(() => {
      expect(applyK8sManifest).toHaveBeenLastCalledWith('cluster-1', expect.objectContaining({
        dry_run: false,
        yaml: expect.stringContaining('image: nginx:1.27'),
      }))
    })
    expect(onCreated).toHaveBeenCalled()
    expect(onToast).toHaveBeenCalledWith('资源创建成功', 'success')
  })

  test('adds a Namespace document when creating into a new namespace', () => {
    renderModal({ currentNamespace: '' })

    const namespaceOptions = Array.from(screen.getByLabelText('目标命名空间').querySelectorAll('option')).map((option) => option.textContent)
    expect(namespaceOptions[0]).toBe('创建命名空间')
    expect(namespaceOptions).toEqual(expect.arrayContaining(['default', 'payments']))

    fireEvent.change(screen.getByLabelText('目标命名空间'), { target: { value: '__create__' } })
    fireEvent.change(screen.getByLabelText('新命名空间'), { target: { value: 'staging' } })

    const preview = screen.getByLabelText('YAML 预览')
    expect((preview as HTMLTextAreaElement).value).toContain('kind: Namespace')
    expect((preview as HTMLTextAreaElement).value).toContain('name: staging')
    expect((preview as HTMLTextAreaElement).value).toContain('namespace: staging')
  })

  test('adds common Ingress routing fields to generated YAML', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源类型'), { target: { value: 'ingress' } })
    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'api-ingress' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'api.example.com' } })
    fireEvent.change(screen.getByLabelText('后端 Service'), { target: { value: 'api-svc' } })
    fireEvent.change(screen.getByLabelText('服务端口'), { target: { value: '8080' } })
    fireEvent.change(screen.getByLabelText('Ingress Class'), { target: { value: 'nginx' } })
    fireEvent.change(screen.getByLabelText('路径'), { target: { value: '/api' } })
    fireEvent.change(screen.getByLabelText('Path Type'), { target: { value: 'ImplementationSpecific' } })
    fireEvent.change(screen.getByLabelText('TLS Secret'), { target: { value: 'api-tls' } })
    fireEvent.change(screen.getByLabelText('Ingress Annotations'), { target: { value: 'nginx.ingress.kubernetes.io/rewrite-target=/' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('annotations:')
    expect(yaml).toContain('nginx.ingress.kubernetes.io/rewrite-target: "/"')
    expect(yaml).toContain('ingressClassName: nginx')
    expect(yaml).toContain('host: "api.example.com"')
    expect(yaml).toContain('path: /api')
    expect(yaml).toContain('pathType: ImplementationSpecific')
    expect(yaml).toContain('name: api-svc')
    expect(yaml).toContain('number: 8080')
    expect(yaml).toContain('tls:')
    expect(yaml).toContain('secretName: api-tls')
  })

  test('adds container runtime, init container, resource and probe fields to generated workload YAML', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'api' } })
    fireEvent.change(screen.getByLabelText('镜像'), { target: { value: 'registry.example.com/api:1.0' } })

    fireEvent.click(screen.getByRole('button', { name: '容器运行' }))
    fireEvent.change(screen.getByLabelText('启动命令'), { target: { value: '/bin/sh' } })
    fireEvent.change(screen.getByLabelText('启动参数'), { target: { value: '-c\nnpm start' } })
    fireEvent.change(screen.getByLabelText('工作目录'), { target: { value: '/app' } })
    fireEvent.change(screen.getByLabelText('拉取策略'), { target: { value: 'IfNotPresent' } })
    fireEvent.click(screen.getByLabelText('启用 Init 容器'))
    fireEvent.change(screen.getByLabelText('Init 容器名称'), { target: { value: 'init-db' } })
    fireEvent.change(screen.getByLabelText('Init 容器镜像'), { target: { value: 'busybox:1.36' } })
    fireEvent.change(screen.getByLabelText('Init 容器命令'), { target: { value: 'sh\n-c\nuntil nslookup db; do sleep 2; done' } })

    fireEvent.click(screen.getByRole('button', { name: '资源与健康检查' }))
    fireEvent.change(screen.getByLabelText('CPU Request'), { target: { value: '100m' } })
    fireEvent.change(screen.getByLabelText('CPU Limit'), { target: { value: '500m' } })
    fireEvent.change(screen.getByLabelText('内存 Request'), { target: { value: '128Mi' } })
    fireEvent.change(screen.getByLabelText('内存 Limit'), { target: { value: '256Mi' } })
    fireEvent.change(screen.getByLabelText('Readiness 路径'), { target: { value: '/ready' } })
    fireEvent.change(screen.getByLabelText('Liveness 路径'), { target: { value: '/healthz' } })
    fireEvent.change(screen.getByLabelText('探针端口'), { target: { value: '8080' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('command:')
    expect(yaml).toContain('- /bin/sh')
    expect(yaml).toContain('args:')
    expect(yaml).toContain('- npm start')
    expect(yaml).toContain('workingDir: /app')
    expect(yaml).toContain('imagePullPolicy: IfNotPresent')
    expect(yaml).toContain('initContainers:')
    expect(yaml).toContain('name: init-db')
    expect(yaml).toContain('resources:')
    expect(yaml).toContain('requests:')
    expect(yaml).toContain('cpu: "100m"')
    expect(yaml).toContain('memory: "256Mi"')
    expect(yaml).toContain('readinessProbe:')
    expect(yaml).toContain('path: /ready')
    expect(yaml).toContain('livenessProbe:')
    expect(yaml).toContain('path: /healthz')
  })

  test('adds volume, scheduling and security fields to generated workload YAML', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'worker' } })
    fireEvent.change(screen.getByLabelText('镜像'), { target: { value: 'worker:2.0' } })

    fireEvent.click(screen.getByRole('button', { name: '存储挂载' }))
    fireEvent.change(screen.getByLabelText('卷名称'), { target: { value: 'data' } })
    fireEvent.change(screen.getByLabelText('挂载路径'), { target: { value: '/data' } })
    fireEvent.change(screen.getByLabelText('卷类型'), { target: { value: 'pvc' } })
    fireEvent.change(screen.getByLabelText('卷来源'), { target: { value: 'data-pvc' } })
    fireEvent.click(screen.getByLabelText('只读挂载'))

    fireEvent.click(screen.getByRole('button', { name: '调度与安全' }))
    fireEvent.change(screen.getByLabelText('指定节点'), { target: { value: 'node-a' } })
    fireEvent.change(screen.getByLabelText('节点选择器'), { target: { value: 'disk=ssd\nzone=east' } })
    fireEvent.change(screen.getByLabelText('ServiceAccount'), { target: { value: 'app-runner' } })
    fireEvent.change(screen.getByLabelText('镜像拉取密钥'), { target: { value: 'regcred' } })
    fireEvent.click(screen.getByLabelText('Privileged'))
    fireEvent.change(screen.getByLabelText('Run As User'), { target: { value: '1000' } })
    fireEvent.click(screen.getByLabelText('只读根文件系统'))

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('volumeMounts:')
    expect(yaml).toContain('mountPath: /data')
    expect(yaml).toContain('readOnly: true')
    expect(yaml).toContain('volumes:')
    expect(yaml).toContain('persistentVolumeClaim:')
    expect(yaml).toContain('claimName: data-pvc')
    expect(yaml).toContain('nodeName: node-a')
    expect(yaml).toContain('nodeSelector:')
    expect(yaml).toContain('disk: "ssd"')
    expect(yaml).toContain('serviceAccountName: app-runner')
    expect(yaml).toContain('imagePullSecrets:')
    expect(yaml).toContain('name: regcred')
    expect(yaml).toContain('securityContext:')
    expect(yaml).toContain('privileged: true')
    expect(yaml).toContain('runAsUser: 1000')
    expect(yaml).toContain('readOnlyRootFilesystem: true')
  })

  test('generates Service YAML with multiple ports, named targetPort, nodePort and custom selectors', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源类型'), { target: { value: 'service' } })
    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'api-svc' } })
    fireEvent.change(screen.getByLabelText('Service 类型'), { target: { value: 'NodePort' } })
    fireEvent.change(screen.getByLabelText('Service Selector'), { target: { value: 'app=api\ntier=backend' } })
    fireEvent.change(screen.getByLabelText('Service 端口规则'), { target: { value: 'http:80:http:TCP:30080\nmetrics:9090:metrics:TCP' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('type: NodePort')
    expect(yaml).toContain('app: "api"')
    expect(yaml).toContain('tier: "backend"')
    expect(yaml).toContain('name: http')
    expect(yaml).toContain('port: 80')
    expect(yaml).toContain('targetPort: http')
    expect(yaml).toContain('nodePort: 30080')
    expect(yaml).toContain('name: metrics')
    expect(yaml).toContain('targetPort: metrics')
  })

  test('adds PVC storageClass, volumeMode and access mode fields', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源类型'), { target: { value: 'pvc' } })
    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'data' } })
    fireEvent.change(screen.getByLabelText('StorageClass'), { target: { value: 'fast-ssd' } })
    fireEvent.change(screen.getByLabelText('Volume Mode'), { target: { value: 'Block' } })
    fireEvent.change(screen.getByLabelText('访问模式'), { target: { value: 'ReadWriteMany' } })
    fireEvent.change(screen.getByLabelText('容量'), { target: { value: '20Gi' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('storageClassName: fast-ssd')
    expect(yaml).toContain('volumeMode: Block')
    expect(yaml).toContain('- ReadWriteMany')
    expect(yaml).toContain('storage: 20Gi')
  })

  test('adds Job and CronJob execution policy fields to generated YAML', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源类型'), { target: { value: 'cronjob' } })
    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'nightly' } })
    fireEvent.change(screen.getByLabelText('镜像'), { target: { value: 'busybox:1.36' } })
    fireEvent.click(screen.getByRole('button', { name: 'Job 策略' }))
    fireEvent.change(screen.getByLabelText('Completions'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Parallelism'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Backoff Limit'), { target: { value: '4' } })
    fireEvent.change(screen.getByLabelText('Active Deadline Seconds'), { target: { value: '120' } })
    fireEvent.change(screen.getByLabelText('Concurrency Policy'), { target: { value: 'Forbid' } })
    fireEvent.click(screen.getByLabelText('暂停 CronJob'))
    fireEvent.change(screen.getByLabelText('成功历史保留'), { target: { value: '5' } })
    fireEvent.change(screen.getByLabelText('失败历史保留'), { target: { value: '2' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('concurrencyPolicy: Forbid')
    expect(yaml).toContain('suspend: true')
    expect(yaml).toContain('successfulJobsHistoryLimit: 5')
    expect(yaml).toContain('failedJobsHistoryLimit: 2')
    expect(yaml).toContain('completions: 3')
    expect(yaml).toContain('parallelism: 2')
    expect(yaml).toContain('backoffLimit: 4')
    expect(yaml).toContain('activeDeadlineSeconds: 120')
  })

  test('adds workload config, secret, lifecycle, extra containers and scheduling advanced fields', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'api' } })
    fireEvent.change(screen.getByLabelText('镜像'), { target: { value: 'api:1.0' } })
    fireEvent.click(screen.getByRole('button', { name: '容器运行' }))
    fireEvent.change(screen.getByLabelText('容器端口规则'), { target: { value: 'http:8080:TCP\nmetrics:9090:TCP' } })
    fireEvent.change(screen.getByLabelText('附加容器'), { target: { value: 'sidecar=busybox:1.36:9000' } })
    fireEvent.change(screen.getByLabelText('PostStart 命令'), { target: { value: 'sh\n-c\necho ready' } })
    fireEvent.change(screen.getByLabelText('PreStop 命令'), { target: { value: 'sh\n-c\necho stop' } })

    fireEvent.click(screen.getByRole('button', { name: '配置与密钥' }))
    fireEvent.change(screen.getByLabelText('ConfigMap envFrom'), { target: { value: 'app-config' } })
    fireEvent.change(screen.getByLabelText('Secret envFrom'), { target: { value: 'app-secret' } })
    fireEvent.change(screen.getByLabelText('ConfigMap 文件挂载'), { target: { value: 'app-config:/etc/config' } })
    fireEvent.change(screen.getByLabelText('Secret 文件挂载'), { target: { value: 'app-secret:/etc/secret' } })

    fireEvent.click(screen.getByRole('button', { name: '调度与安全' }))
    fireEvent.change(screen.getByLabelText('更新策略'), { target: { value: 'Recreate' } })
    fireEvent.change(screen.getByLabelText('Node Affinity'), { target: { value: 'disk=ssd\nzone=east' } })
    fireEvent.change(screen.getByLabelText('Topology Spread'), { target: { value: 'topologyKey=topology.kubernetes.io/zone\nmaxSkew=2\nwhenUnsatisfiable=ScheduleAnyway' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('strategy:')
    expect(yaml).toContain('type: Recreate')
    expect(yaml).toContain('containerPort: 8080')
    expect(yaml).toContain('name: metrics')
    expect(yaml).toContain('name: sidecar')
    expect(yaml).toContain('image: busybox:1.36')
    expect(yaml).toContain('lifecycle:')
    expect(yaml).toContain('postStart:')
    expect(yaml).toContain('preStop:')
    expect(yaml).toContain('configMapRef:')
    expect(yaml).toContain('name: app-config')
    expect(yaml).toContain('secretRef:')
    expect(yaml).toContain('name: app-secret')
    expect(yaml).toContain('mountPath: /etc/config')
    expect(yaml).toContain('mountPath: /etc/secret')
    expect(yaml).toContain('affinity:')
    expect(yaml).toContain('nodeAffinity:')
    expect(yaml).toContain('topologySpreadConstraints:')
    expect(yaml).toContain('topologyKey: topology.kubernetes.io/zone')
  })

  test('adds StatefulSet volumeClaimTemplates when requested', () => {
    renderModal()

    fireEvent.change(screen.getByLabelText('资源类型'), { target: { value: 'statefulset' } })
    fireEvent.change(screen.getByLabelText('资源名称'), { target: { value: 'mysql' } })
    fireEvent.change(screen.getByLabelText('镜像'), { target: { value: 'mysql:8' } })
    fireEvent.click(screen.getByRole('button', { name: '存储挂载' }))
    fireEvent.click(screen.getByLabelText('启用 StatefulSet PVC 模板'))
    fireEvent.change(screen.getByLabelText('PVC 模板名称'), { target: { value: 'data' } })
    fireEvent.change(screen.getByLabelText('PVC 模板挂载路径'), { target: { value: '/var/lib/mysql' } })
    fireEvent.change(screen.getByLabelText('PVC 模板 StorageClass'), { target: { value: 'fast-ssd' } })
    fireEvent.change(screen.getByLabelText('PVC 模板容量'), { target: { value: '50Gi' } })

    const yaml = (screen.getByLabelText('YAML 预览') as HTMLTextAreaElement).value
    expect(yaml).toContain('volumeClaimTemplates:')
    expect(yaml).toContain('name: data')
    expect(yaml).toContain('mountPath: /var/lib/mysql')
    expect(yaml).toContain('storageClassName: fast-ssd')
    expect(yaml).toContain('storage: 50Gi')
  })
})
