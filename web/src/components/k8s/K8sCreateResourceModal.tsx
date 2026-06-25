import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Activity, CheckCircle2, ChevronDown, Copy, FileCode2, HardDrive, KeyRound, Plus, ServerCog, Shield, SlidersHorizontal, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { applyK8sManifest } from '../../api/k8s'
import type { K8sNamespace } from '../../types'

type ResourceType =
  | 'deployment'
  | 'pod'
  | 'statefulset'
  | 'daemonset'
  | 'job'
  | 'cronjob'
  | 'service'
  | 'ingress'
  | 'configmap'
  | 'secret'
  | 'pvc'
  | 'custom'

type VolumeType = 'emptyDir' | 'pvc' | 'configMap' | 'secret' | 'hostPath'

type WorkloadOptions = {
  name: string
  image: string
  port: string
  containerPortsText: string
  envText: string
  configMapEnvFrom: string
  secretEnvFrom: string
  configMapMountText: string
  secretMountText: string
  commandText: string
  argsText: string
  workingDir: string
  imagePullPolicy: string
  additionalContainersText: string
  postStartCommandText: string
  preStopCommandText: string
  stdinEnabled: boolean
  ttyEnabled: boolean
  initEnabled: boolean
  initName: string
  initImage: string
  initCommandText: string
  cpuRequest: string
  cpuLimit: string
  memoryRequest: string
  memoryLimit: string
  readinessPath: string
  livenessPath: string
  startupPath: string
  probePort: string
  volumeName: string
  volumeType: VolumeType
  volumeSource: string
  mountPath: string
  volumeReadOnly: boolean
  nodeName: string
  nodeSelectorText: string
  nodeAffinityText: string
  topologySpreadText: string
  tolerationsText: string
  serviceAccountName: string
  imagePullSecretsText: string
  privileged: boolean
  runAsUser: string
  readOnlyRootFilesystem: boolean
  stsVolumeClaimEnabled: boolean
  stsVolumeClaimName: string
  stsVolumeClaimMountPath: string
  stsVolumeClaimSize: string
  stsVolumeClaimStorageClass: string
  stsVolumeClaimAccessMode: string
}

type K8sCreateResourceModalProps = {
  open: boolean
  clusterId: string
  currentNamespace: string
  namespaces: K8sNamespace[]
  onClose: () => void
  onCreated: () => void
  onToast: (message: string, type: 'success' | 'error') => void
}

const resourceTypeLabels: Array<{ value: ResourceType; label: string }> = [
  { value: 'deployment', label: 'Deployment' },
  { value: 'pod', label: 'Pod' },
  { value: 'statefulset', label: 'StatefulSet' },
  { value: 'daemonset', label: 'DaemonSet' },
  { value: 'job', label: 'Job' },
  { value: 'cronjob', label: 'CronJob' },
  { value: 'service', label: 'Service' },
  { value: 'ingress', label: 'Ingress' },
  { value: 'configmap', label: 'ConfigMap' },
  { value: 'secret', label: 'Secret' },
  { value: 'pvc', label: 'PVC' },
  { value: 'custom', label: '自定义 YAML' },
]

const namespacedTypes = new Set<ResourceType>(['deployment', 'pod', 'statefulset', 'daemonset', 'job', 'cronjob', 'service', 'ingress', 'configmap', 'secret', 'pvc'])
const imageTypes = new Set<ResourceType>(['deployment', 'pod', 'statefulset', 'daemonset', 'job', 'cronjob'])
const replicaTypes = new Set<ResourceType>(['deployment', 'statefulset'])
const networkableTypes = new Set<ResourceType>(['deployment', 'pod', 'statefulset', 'daemonset'])

function defaultNamespace(currentNamespace: string, namespaces: K8sNamespace[]): string {
  const current = currentNamespace.trim()
  if (current) return current
  if (namespaces.some((item) => item.name === 'default')) return 'default'
  return namespaces[0]?.name || 'default'
}

function safeName(value: string, fallback: string): string {
  return value.trim() || fallback
}

function yamlString(value: string): string {
  return JSON.stringify(value)
}

function parseKeyValues(value: string): Array<{ key: string; value: string }> {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const index = line.indexOf('=')
      if (index === -1) return { key: line, value: '' }
      return { key: line.slice(0, index).trim(), value: line.slice(index + 1).trim() }
    })
    .filter((item) => item.key)
}

function parseListLines(value: string): string[] {
  return value
    .split(/[\n,]+/)
    .map((line) => line.trim())
    .filter(Boolean)
}

function parsePortValue(value: string): string | number {
  const trimmed = value.trim()
  if (/^\d+$/.test(trimmed)) return Number.parseInt(trimmed, 10)
  return trimmed
}

function yamlValue(value: string | number): string {
  return typeof value === 'number' ? String(value) : value
}

function appendExecCommand(lines: string[], key: string, commandText: string, indent: string) {
  const command = parseListLines(commandText)
  if (command.length === 0) return
  lines.push(`${indent}${key}:`)
  lines.push(`${indent}  exec:`)
  appendStringList(lines, 'command', command, `${indent}    `)
}

function parseContainerPorts(primaryPort: string, portsText: string): Array<{ name?: string; port: number; protocol?: string }> {
  const lines = parseListLines(portsText)
  if (lines.length === 0) {
    const port = Number.parseInt(primaryPort, 10)
    return Number.isFinite(port) && port > 0 ? [{ port }] : []
  }
  return lines
    .map((line) => {
      const parts = line.split(':').map((part) => part.trim()).filter(Boolean)
      if (parts.length >= 3) {
        return {
          name: parts[0],
          port: Number.parseInt(parts[1], 10) || 80,
          protocol: parts[2],
        }
      }
      if (parts.length === 2 && /^\d+$/.test(parts[0])) {
        return {
          port: Number.parseInt(parts[0], 10) || 80,
          protocol: parts[1],
        }
      }
      if (parts.length === 2) {
        return {
          name: parts[0],
          port: Number.parseInt(parts[1], 10) || 80,
        }
      }
      return {
        port: Number.parseInt(parts[0], 10) || 80,
      }
    })
    .filter((item) => item.port > 0)
}

function parseServicePorts(portRulesText: string, fallbackPort: string, fallbackTargetPort: string): Array<{ name: string; port: number; targetPort: string | number; protocol: string; nodePort?: number }> {
  const lines = parseListLines(portRulesText)
  if (lines.length === 0) {
    const servicePort = Number.parseInt(fallbackPort, 10) || 80
    return [{
      name: 'http',
      port: servicePort,
      targetPort: parsePortValue(fallbackTargetPort || String(servicePort)),
      protocol: 'TCP',
    }]
  }
  return lines.map((line, index) => {
    const [name, port, targetPort, protocol, nodePort] = line.split(':').map((part) => part.trim())
    return {
      name: name || `port-${index + 1}`,
      port: Number.parseInt(port, 10) || 80,
      targetPort: parsePortValue(targetPort || port || '80'),
      protocol: protocol || 'TCP',
      nodePort: nodePort ? Number.parseInt(nodePort, 10) || undefined : undefined,
    }
  })
}

function parseMountRefs(value: string): Array<{ name: string; mountPath: string }> {
  return parseListLines(value)
    .map((line) => {
      const [name, mountPath] = line.split(':').map((part) => part.trim())
      return { name, mountPath }
    })
    .filter((item) => item.name && item.mountPath)
}

function parseAdditionalContainers(value: string): Array<{ name: string; image: string; port?: string }> {
  return parseListLines(value)
    .map((line) => {
      const index = line.indexOf('=')
      if (index === -1) return undefined
      const name = line.slice(0, index).trim()
      const imageAndPort = line.slice(index + 1).trim()
      const lastColon = imageAndPort.lastIndexOf(':')
      if (!name || !imageAndPort) return undefined
      if (lastColon > 0 && /^\d+$/.test(imageAndPort.slice(lastColon + 1))) {
        return { name, image: imageAndPort.slice(0, lastColon), port: imageAndPort.slice(lastColon + 1) }
      }
      return { name, image: imageAndPort }
    })
    .filter((item): item is { name: string; image: string; port?: string } => Boolean(item))
}

function hasAnyValue(values: string[]): boolean {
  return values.some((value) => value.trim())
}

function parseTolerations(value: string): Array<{ key: string; value?: string; effect?: string }> {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [keyValue, effect] = line.split(':').map((part) => part.trim())
      const index = keyValue.indexOf('=')
      if (index === -1) return { key: keyValue, effect }
      return {
        key: keyValue.slice(0, index).trim(),
        value: keyValue.slice(index + 1).trim(),
        effect,
      }
    })
    .filter((item) => item.key)
}

function namespaceManifest(namespace: string): string {
  return `apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
`
}

function annotationLines(value: string, indent: string): string[] {
  const annotations = parseKeyValues(value)
  if (annotations.length === 0) return []
  return [
    `${indent}annotations:`,
    ...annotations.map((item) => `${indent}  ${item.key}: ${yamlString(item.value)}`),
  ]
}

function appendStringList(lines: string[], key: string, values: string[], indent: string) {
  if (values.length === 0) return
  lines.push(`${indent}${key}:`)
  values.forEach((value) => {
    lines.push(`${indent}- ${value}`)
  })
}

function appendProbe(lines: string[], key: string, path: string, port: string, indent: string) {
  const probePath = path.trim()
  if (!probePath) return
  lines.push(`${indent}${key}:`)
  lines.push(`${indent}  httpGet:`)
  lines.push(`${indent}    path: ${probePath}`)
  lines.push(`${indent}    port: ${Number.parseInt(port, 10) || 80}`)
}

function containerBlock(options: WorkloadOptions, indent = '      ', overrides?: Partial<WorkloadOptions>): string {
  const merged = { ...options, ...overrides }
  const env = parseKeyValues(merged.envText)
  const command = parseListLines(merged.commandText)
  const args = parseListLines(merged.argsText)
  const containerPorts = parseContainerPorts(merged.port, merged.containerPortsText)
  const configMapEnvFrom = parseListLines(merged.configMapEnvFrom)
  const secretEnvFrom = parseListLines(merged.secretEnvFrom)
  const configMapMounts = parseMountRefs(merged.configMapMountText)
  const secretMounts = parseMountRefs(merged.secretMountText)
  const hasRequests = hasAnyValue([merged.cpuRequest, merged.memoryRequest])
  const hasLimits = hasAnyValue([merged.cpuLimit, merged.memoryLimit])
  const hasSecurityContext = merged.privileged || merged.runAsUser.trim() || merged.readOnlyRootFilesystem
  const hasVolumeMount = merged.volumeName.trim() && merged.mountPath.trim()
  const hasStsVolumeClaimMount = merged.stsVolumeClaimEnabled && merged.stsVolumeClaimName.trim() && merged.stsVolumeClaimMountPath.trim()
  const hasLifecycle = parseListLines(merged.postStartCommandText).length > 0 || parseListLines(merged.preStopCommandText).length > 0
  const lines = [
    `${indent}- name: ${merged.name}`,
    `${indent}  image: ${merged.image}`,
  ]
  if (merged.imagePullPolicy.trim()) {
    lines.push(`${indent}  imagePullPolicy: ${merged.imagePullPolicy}`)
  }
  if (command.length > 0) {
    appendStringList(lines, 'command', command, `${indent}  `)
  }
  if (args.length > 0) {
    appendStringList(lines, 'args', args, `${indent}  `)
  }
  if (merged.workingDir.trim()) {
    lines.push(`${indent}  workingDir: ${merged.workingDir.trim()}`)
  }
  if (merged.stdinEnabled) {
    lines.push(`${indent}  stdin: true`)
  }
  if (merged.ttyEnabled) {
    lines.push(`${indent}  tty: true`)
  }
  if (containerPorts.length > 0) {
    lines.push(`${indent}  ports:`)
    containerPorts.forEach((item) => {
      lines.push(`${indent}  - containerPort: ${item.port}`)
      if (item.name) lines.push(`${indent}    name: ${item.name}`)
      if (item.protocol) lines.push(`${indent}    protocol: ${item.protocol}`)
    })
  }
  if (env.length > 0) {
    lines.push(`${indent}  env:`)
    env.forEach((item) => {
      lines.push(`${indent}  - name: ${item.key}`)
      lines.push(`${indent}    value: ${yamlString(item.value)}`)
    })
  }
  if (configMapEnvFrom.length > 0 || secretEnvFrom.length > 0) {
    lines.push(`${indent}  envFrom:`)
    configMapEnvFrom.forEach((name) => {
      lines.push(`${indent}  - configMapRef:`)
      lines.push(`${indent}      name: ${name}`)
    })
    secretEnvFrom.forEach((name) => {
      lines.push(`${indent}  - secretRef:`)
      lines.push(`${indent}      name: ${name}`)
    })
  }
  if (hasRequests || hasLimits) {
    lines.push(`${indent}  resources:`)
    if (hasRequests) {
      lines.push(`${indent}    requests:`)
      if (merged.cpuRequest.trim()) lines.push(`${indent}      cpu: ${yamlString(merged.cpuRequest.trim())}`)
      if (merged.memoryRequest.trim()) lines.push(`${indent}      memory: ${yamlString(merged.memoryRequest.trim())}`)
    }
    if (hasLimits) {
      lines.push(`${indent}    limits:`)
      if (merged.cpuLimit.trim()) lines.push(`${indent}      cpu: ${yamlString(merged.cpuLimit.trim())}`)
      if (merged.memoryLimit.trim()) lines.push(`${indent}      memory: ${yamlString(merged.memoryLimit.trim())}`)
    }
  }
  const probePort = merged.probePort.trim() || merged.port.trim()
  appendProbe(lines, 'readinessProbe', merged.readinessPath, probePort, `${indent}  `)
  appendProbe(lines, 'livenessProbe', merged.livenessPath, probePort, `${indent}  `)
  appendProbe(lines, 'startupProbe', merged.startupPath, probePort, `${indent}  `)
  if (hasLifecycle) {
    lines.push(`${indent}  lifecycle:`)
    appendExecCommand(lines, 'postStart', merged.postStartCommandText, `${indent}    `)
    appendExecCommand(lines, 'preStop', merged.preStopCommandText, `${indent}    `)
  }
  if (hasSecurityContext) {
    lines.push(`${indent}  securityContext:`)
    if (merged.privileged) lines.push(`${indent}    privileged: true`)
    if (merged.runAsUser.trim()) lines.push(`${indent}    runAsUser: ${Number.parseInt(merged.runAsUser, 10) || 0}`)
    if (merged.readOnlyRootFilesystem) lines.push(`${indent}    readOnlyRootFilesystem: true`)
  }
  if (hasVolumeMount) {
    lines.push(`${indent}  volumeMounts:`)
    lines.push(`${indent}  - name: ${merged.volumeName.trim()}`)
    lines.push(`${indent}    mountPath: ${merged.mountPath.trim()}`)
    if (merged.volumeReadOnly) lines.push(`${indent}    readOnly: true`)
    configMapMounts.forEach((item) => {
      lines.push(`${indent}  - name: ${item.name}-config`)
      lines.push(`${indent}    mountPath: ${item.mountPath}`)
    })
    secretMounts.forEach((item) => {
      lines.push(`${indent}  - name: ${item.name}-secret`)
      lines.push(`${indent}    mountPath: ${item.mountPath}`)
      lines.push(`${indent}    readOnly: true`)
    })
    if (hasStsVolumeClaimMount) {
      lines.push(`${indent}  - name: ${merged.stsVolumeClaimName.trim()}`)
      lines.push(`${indent}    mountPath: ${merged.stsVolumeClaimMountPath.trim()}`)
    }
  } else if (configMapMounts.length > 0 || secretMounts.length > 0 || hasStsVolumeClaimMount) {
    lines.push(`${indent}  volumeMounts:`)
    configMapMounts.forEach((item) => {
      lines.push(`${indent}  - name: ${item.name}-config`)
      lines.push(`${indent}    mountPath: ${item.mountPath}`)
    })
    secretMounts.forEach((item) => {
      lines.push(`${indent}  - name: ${item.name}-secret`)
      lines.push(`${indent}    mountPath: ${item.mountPath}`)
      lines.push(`${indent}    readOnly: true`)
    })
    if (hasStsVolumeClaimMount) {
      lines.push(`${indent}  - name: ${merged.stsVolumeClaimName.trim()}`)
      lines.push(`${indent}    mountPath: ${merged.stsVolumeClaimMountPath.trim()}`)
    }
  }
  return lines.join('\n')
}

function volumeBlock(options: WorkloadOptions, indent: string): string[] {
  const volumeName = options.volumeName.trim()
  const mountPath = options.mountPath.trim()
  const lines: string[] = []
  if (volumeName && mountPath) {
    const source = options.volumeSource.trim() || volumeName
    lines.push(`${indent}- name: ${volumeName}`)
    if (options.volumeType === 'emptyDir') {
      lines.push(`${indent}  emptyDir: {}`)
    } else if (options.volumeType === 'pvc') {
      lines.push(`${indent}  persistentVolumeClaim:`)
      lines.push(`${indent}    claimName: ${source}`)
    } else if (options.volumeType === 'configMap') {
      lines.push(`${indent}  configMap:`)
      lines.push(`${indent}    name: ${source}`)
    } else if (options.volumeType === 'secret') {
      lines.push(`${indent}  secret:`)
      lines.push(`${indent}    secretName: ${source}`)
    } else if (options.volumeType === 'hostPath') {
      lines.push(`${indent}  hostPath:`)
      lines.push(`${indent}    path: ${source}`)
      lines.push(`${indent}    type: DirectoryOrCreate`)
    }
  }
  parseMountRefs(options.configMapMountText).forEach((item) => {
    lines.push(`${indent}- name: ${item.name}-config`)
    lines.push(`${indent}  configMap:`)
    lines.push(`${indent}    name: ${item.name}`)
  })
  parseMountRefs(options.secretMountText).forEach((item) => {
    lines.push(`${indent}- name: ${item.name}-secret`)
    lines.push(`${indent}  secret:`)
    lines.push(`${indent}    secretName: ${item.name}`)
  })
  return lines
}

function podSpecBlock(options: WorkloadOptions, indent: string, restartPolicy?: string): string {
  const lines: string[] = []
  const imagePullSecrets = parseListLines(options.imagePullSecretsText)
  const nodeSelector = parseKeyValues(options.nodeSelectorText)
  const nodeAffinity = parseKeyValues(options.nodeAffinityText)
  const topologySpread = parseKeyValues(options.topologySpreadText)
  const tolerations = parseTolerations(options.tolerationsText)
  const volumes = volumeBlock(options, indent)

  if (restartPolicy) lines.push(`${indent}restartPolicy: ${restartPolicy}`)
  if (options.serviceAccountName.trim()) lines.push(`${indent}serviceAccountName: ${options.serviceAccountName.trim()}`)
  if (imagePullSecrets.length > 0) {
    lines.push(`${indent}imagePullSecrets:`)
    imagePullSecrets.forEach((secret) => {
      lines.push(`${indent}- name: ${secret}`)
    })
  }
  if (options.nodeName.trim()) lines.push(`${indent}nodeName: ${options.nodeName.trim()}`)
  if (nodeSelector.length > 0) {
    lines.push(`${indent}nodeSelector:`)
    nodeSelector.forEach((item) => {
      lines.push(`${indent}  ${item.key}: ${yamlString(item.value)}`)
    })
  }
  if (tolerations.length > 0) {
    lines.push(`${indent}tolerations:`)
    tolerations.forEach((item) => {
      lines.push(`${indent}- key: ${yamlString(item.key)}`)
      if (item.value) {
        lines.push(`${indent}  operator: Equal`)
        lines.push(`${indent}  value: ${yamlString(item.value)}`)
      } else {
        lines.push(`${indent}  operator: Exists`)
      }
      if (item.effect) lines.push(`${indent}  effect: ${item.effect}`)
    })
  }
  if (nodeAffinity.length > 0) {
    lines.push(`${indent}affinity:`)
    lines.push(`${indent}  nodeAffinity:`)
    lines.push(`${indent}    requiredDuringSchedulingIgnoredDuringExecution:`)
    lines.push(`${indent}      nodeSelectorTerms:`)
    lines.push(`${indent}      - matchExpressions:`)
    nodeAffinity.forEach((item) => {
      lines.push(`${indent}        - key: ${item.key}`)
      lines.push(`${indent}          operator: In`)
      lines.push(`${indent}          values:`)
      lines.push(`${indent}          - ${item.value}`)
    })
  }
  if (topologySpread.length > 0) {
    const settings = Object.fromEntries(topologySpread.map((item) => [item.key, item.value]))
    lines.push(`${indent}topologySpreadConstraints:`)
    lines.push(`${indent}- maxSkew: ${Number.parseInt(settings.maxSkew || '1', 10) || 1}`)
    lines.push(`${indent}  topologyKey: ${settings.topologyKey || 'kubernetes.io/hostname'}`)
    lines.push(`${indent}  whenUnsatisfiable: ${settings.whenUnsatisfiable || 'DoNotSchedule'}`)
    lines.push(`${indent}  labelSelector:`)
    lines.push(`${indent}    matchLabels:`)
    lines.push(`${indent}      app: ${options.name}`)
  }
  if (options.initEnabled && options.initImage.trim()) {
    const initName = safeName(options.initName, 'init-container')
    lines.push(`${indent}initContainers:`)
    lines.push(containerBlock(options, indent, {
      name: initName,
      image: options.initImage.trim(),
      port: '',
      containerPortsText: '',
      envText: '',
      configMapEnvFrom: '',
      secretEnvFrom: '',
      configMapMountText: '',
      secretMountText: '',
      commandText: options.initCommandText,
      argsText: '',
      workingDir: '',
      imagePullPolicy: options.imagePullPolicy,
      additionalContainersText: '',
      postStartCommandText: '',
      preStopCommandText: '',
      cpuRequest: '',
      cpuLimit: '',
      memoryRequest: '',
      memoryLimit: '',
      readinessPath: '',
      livenessPath: '',
      startupPath: '',
      probePort: '',
      volumeName: '',
      mountPath: '',
      privileged: false,
      runAsUser: '',
      readOnlyRootFilesystem: false,
      nodeAffinityText: '',
      topologySpreadText: '',
      stdinEnabled: false,
      ttyEnabled: false,
    }))
  }
  lines.push(`${indent}containers:`)
  lines.push(containerBlock(options, indent))
  parseAdditionalContainers(options.additionalContainersText).forEach((container) => {
    lines.push(containerBlock(options, indent, {
      name: container.name,
      image: container.image,
      port: container.port || '',
      containerPortsText: '',
      envText: '',
      configMapEnvFrom: '',
      secretEnvFrom: '',
      configMapMountText: '',
      secretMountText: '',
      commandText: '',
      argsText: '',
      workingDir: '',
      additionalContainersText: '',
      postStartCommandText: '',
      preStopCommandText: '',
      cpuRequest: '',
      cpuLimit: '',
      memoryRequest: '',
      memoryLimit: '',
      readinessPath: '',
      livenessPath: '',
      startupPath: '',
      probePort: '',
      volumeName: '',
      mountPath: '',
      privileged: false,
      runAsUser: '',
      readOnlyRootFilesystem: false,
      stsVolumeClaimEnabled: false,
    }))
  })
  if (volumes.length > 0) {
    lines.push(`${indent}volumes:`)
    lines.push(...volumes)
  }
  return lines.join('\n')
}

function serviceManifest(
  namespace: string,
  name: string,
  selector: string,
  serviceType: string,
  port: string,
  targetPort: string,
  options: {
    selectorText?: string
    portRulesText?: string
    headless?: boolean
  } = {}
): string {
  const selectors = parseKeyValues(options.selectorText || '')
  const ports = parseServicePorts(options.portRulesText || '', port, targetPort)
  return `apiVersion: v1
kind: Service
metadata:
  name: ${name}
  namespace: ${namespace}
spec:
${options.headless ? '  clusterIP: None\n' : `  type: ${serviceType}\n`}  selector:
${selectors.length === 0 ? `    app: ${selector}` : selectors.map((item) => `    ${item.key}: ${yamlString(item.value)}`).join('\n')}
  ports:
${ports.map((item) => `  - name: ${item.name}
    port: ${item.port}
    targetPort: ${yamlValue(item.targetPort)}
    protocol: ${item.protocol}${item.nodePort ? `\n    nodePort: ${item.nodePort}` : ''}`).join('\n')}
`
}

function ingressManifest(
  namespace: string,
  name: string,
  host: string,
  serviceName: string,
  servicePort: string,
  options: {
    className?: string
    path?: string
    pathType?: string
    tlsSecret?: string
    annotations?: string
  } = {}
): string {
  const annotations = annotationLines(options.annotations || '', '  ')
  const className = options.className?.trim()
  const tlsSecret = options.tlsSecret?.trim()
  const ingressHost = host || `${serviceName}.example.com`
  const path = options.path?.trim() || '/'
  const pathType = options.pathType?.trim() || 'Prefix'
  return `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ${name}
  namespace: ${namespace}
${annotations.length ? `${annotations.join('\n')}\n` : ''}spec:
${className ? `  ingressClassName: ${className}\n` : ''}${tlsSecret ? `  tls:
  - hosts:
    - ${yamlString(ingressHost)}
    secretName: ${tlsSecret}
` : ''}  rules:
  - host: ${yamlString(ingressHost)}
    http:
      paths:
      - path: ${path}
        pathType: ${pathType}
        backend:
          service:
            name: ${serviceName}
            port:
              number: ${Number.parseInt(servicePort, 10) || 80}
`
}

function namespaceOptions(namespaces: K8sNamespace[], selectedNamespace: string, namespaceMode: 'existing' | 'create') {
  const options = [
    <option key="__create__" value="__create__">创建命名空间</option>,
  ]
  if (namespaces.length === 0) {
    options.push(<option key={selectedNamespace} value={selectedNamespace}>{selectedNamespace}</option>)
    return options
  }
  namespaces.forEach((item) => {
    options.push(<option key={item.name} value={item.name}>{item.name}</option>)
  })
  if (namespaceMode === 'existing' && selectedNamespace && !namespaces.some((item) => item.name === selectedNamespace)) {
    options.push(<option key={selectedNamespace} value={selectedNamespace}>{selectedNamespace}</option>)
  }
  return options
}

function AdvancedSection({ title, icon: Icon, open, onToggle, children }: {
  title: string
  icon: LucideIcon
  open: boolean
  onToggle: () => void
  children: ReactNode
}) {
  return (
    <section className="overflow-hidden rounded-2xl border border-border/75 bg-card/70 shadow-[0_12px_30px_rgb(15_23_42/0.05)]">
      <button
        type="button"
        aria-label={title}
        aria-expanded={open}
        onClick={onToggle}
        className="flex min-h-12 w-full items-center justify-between gap-3 px-4 text-left text-sm font-black text-foreground transition hover:bg-muted/45 focus:outline-none focus:ring-4 focus:ring-primary/20"
      >
        <span className="inline-flex min-w-0 items-center gap-2">
          <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-2xl bg-primary/10 text-primary">
            <Icon className="h-4 w-4" aria-hidden="true" />
          </span>
          <span className="truncate">{title}</span>
        </span>
        <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? 'rotate-180' : ''}`} aria-hidden="true" />
      </button>
      {open && (
        <div className="border-t border-border/70 bg-surface/50 p-4">
          {children}
        </div>
      )}
    </section>
  )
}

export function K8sCreateResourceModal({ open, clusterId, currentNamespace, namespaces, onClose, onCreated, onToast }: K8sCreateResourceModalProps) {
  const [resourceType, setResourceType] = useState<ResourceType>('deployment')
  const [namespaceMode, setNamespaceMode] = useState<'existing' | 'create'>('existing')
  const [selectedNamespace, setSelectedNamespace] = useState(defaultNamespace(currentNamespace, namespaces))
  const [newNamespace, setNewNamespace] = useState('')
  const [name, setName] = useState('')
  const [image, setImage] = useState('')
  const [replicas, setReplicas] = useState('1')
  const [containerPort, setContainerPort] = useState('80')
  const [containerPortsText, setContainerPortsText] = useState('')
  const [envText, setEnvText] = useState('')
  const [configMapEnvFrom, setConfigMapEnvFrom] = useState('')
  const [secretEnvFrom, setSecretEnvFrom] = useState('')
  const [configMapMountText, setConfigMapMountText] = useState('')
  const [secretMountText, setSecretMountText] = useState('')
  const [commandText, setCommandText] = useState('')
  const [argsText, setArgsText] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [imagePullPolicy, setImagePullPolicy] = useState('')
  const [additionalContainersText, setAdditionalContainersText] = useState('')
  const [postStartCommandText, setPostStartCommandText] = useState('')
  const [preStopCommandText, setPreStopCommandText] = useState('')
  const [stdinEnabled, setStdinEnabled] = useState(false)
  const [ttyEnabled, setTtyEnabled] = useState(false)
  const [initEnabled, setInitEnabled] = useState(false)
  const [initName, setInitName] = useState('')
  const [initImage, setInitImage] = useState('')
  const [initCommandText, setInitCommandText] = useState('')
  const [cpuRequest, setCpuRequest] = useState('')
  const [cpuLimit, setCpuLimit] = useState('')
  const [memoryRequest, setMemoryRequest] = useState('')
  const [memoryLimit, setMemoryLimit] = useState('')
  const [readinessPath, setReadinessPath] = useState('')
  const [livenessPath, setLivenessPath] = useState('')
  const [startupPath, setStartupPath] = useState('')
  const [probePort, setProbePort] = useState('')
  const [volumeName, setVolumeName] = useState('')
  const [volumeType, setVolumeType] = useState<VolumeType>('emptyDir')
  const [volumeSource, setVolumeSource] = useState('')
  const [mountPath, setMountPath] = useState('')
  const [volumeReadOnly, setVolumeReadOnly] = useState(false)
  const [nodeName, setNodeName] = useState('')
  const [nodeSelectorText, setNodeSelectorText] = useState('')
  const [nodeAffinityText, setNodeAffinityText] = useState('')
  const [topologySpreadText, setTopologySpreadText] = useState('')
  const [tolerationsText, setTolerationsText] = useState('')
  const [serviceAccountName, setServiceAccountName] = useState('')
  const [imagePullSecretsText, setImagePullSecretsText] = useState('')
  const [privileged, setPrivileged] = useState(false)
  const [runAsUser, setRunAsUser] = useState('')
  const [readOnlyRootFilesystem, setReadOnlyRootFilesystem] = useState(false)
  const [updateStrategy, setUpdateStrategy] = useState('')
  const [stsVolumeClaimEnabled, setStsVolumeClaimEnabled] = useState(false)
  const [stsVolumeClaimName, setStsVolumeClaimName] = useState('data')
  const [stsVolumeClaimMountPath, setStsVolumeClaimMountPath] = useState('')
  const [stsVolumeClaimSize, setStsVolumeClaimSize] = useState('10Gi')
  const [stsVolumeClaimStorageClass, setStsVolumeClaimStorageClass] = useState('')
  const [stsVolumeClaimAccessMode, setStsVolumeClaimAccessMode] = useState('ReadWriteOnce')
  const [advancedOpen, setAdvancedOpen] = useState({
    runtime: false,
    resources: false,
    storage: false,
    scheduling: false,
    config: false,
    job: false,
  })
  const [restartPolicy, setRestartPolicy] = useState('Always')
  const [schedule, setSchedule] = useState('*/5 * * * *')
  const [jobCompletions, setJobCompletions] = useState('')
  const [jobParallelism, setJobParallelism] = useState('')
  const [jobBackoffLimit, setJobBackoffLimit] = useState('')
  const [jobActiveDeadlineSeconds, setJobActiveDeadlineSeconds] = useState('')
  const [cronConcurrencyPolicy, setCronConcurrencyPolicy] = useState('')
  const [cronSuspend, setCronSuspend] = useState(false)
  const [cronSuccessfulHistoryLimit, setCronSuccessfulHistoryLimit] = useState('')
  const [cronFailedHistoryLimit, setCronFailedHistoryLimit] = useState('')
  const [includeService, setIncludeService] = useState(false)
  const [includeIngress, setIncludeIngress] = useState(false)
  const [serviceType, setServiceType] = useState('ClusterIP')
  const [servicePort, setServicePort] = useState('80')
  const [servicePortRulesText, setServicePortRulesText] = useState('')
  const [serviceSelectorText, setServiceSelectorText] = useState('')
  const [serviceHeadless, setServiceHeadless] = useState(false)
  const [ingressHost, setIngressHost] = useState('')
  const [ingressServiceName, setIngressServiceName] = useState('')
  const [ingressClassName, setIngressClassName] = useState('')
  const [ingressPath, setIngressPath] = useState('/')
  const [ingressPathType, setIngressPathType] = useState('Prefix')
  const [ingressTLSSecret, setIngressTLSSecret] = useState('')
  const [ingressAnnotations, setIngressAnnotations] = useState('')
  const [configData, setConfigData] = useState('APP_ENV=production')
  const [secretData, setSecretData] = useState('PASSWORD=change-me')
  const [storageSize, setStorageSize] = useState('1Gi')
  const [accessMode, setAccessMode] = useState('ReadWriteOnce')
  const [storageClassName, setStorageClassName] = useState('')
  const [volumeMode, setVolumeMode] = useState('Filesystem')
  const [customYAML, setCustomYAML] = useState(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-runner
  namespace: default
`)
  const [dryRunPassed, setDryRunPassed] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    if (!open) return
    setResourceType('deployment')
    setNamespaceMode('existing')
    setSelectedNamespace(defaultNamespace(currentNamespace, namespaces))
    setNewNamespace('')
    setName('')
    setImage('')
    setReplicas('1')
    setContainerPort('80')
    setContainerPortsText('')
    setEnvText('')
    setConfigMapEnvFrom('')
    setSecretEnvFrom('')
    setConfigMapMountText('')
    setSecretMountText('')
    setCommandText('')
    setArgsText('')
    setWorkingDir('')
    setImagePullPolicy('')
    setAdditionalContainersText('')
    setPostStartCommandText('')
    setPreStopCommandText('')
    setStdinEnabled(false)
    setTtyEnabled(false)
    setInitEnabled(false)
    setInitName('')
    setInitImage('')
    setInitCommandText('')
    setCpuRequest('')
    setCpuLimit('')
    setMemoryRequest('')
    setMemoryLimit('')
    setReadinessPath('')
    setLivenessPath('')
    setStartupPath('')
    setProbePort('')
    setVolumeName('')
    setVolumeType('emptyDir')
    setVolumeSource('')
    setMountPath('')
    setVolumeReadOnly(false)
    setNodeName('')
    setNodeSelectorText('')
    setNodeAffinityText('')
    setTopologySpreadText('')
    setTolerationsText('')
    setServiceAccountName('')
    setImagePullSecretsText('')
    setPrivileged(false)
    setRunAsUser('')
    setReadOnlyRootFilesystem(false)
    setUpdateStrategy('')
    setStsVolumeClaimEnabled(false)
    setStsVolumeClaimName('data')
    setStsVolumeClaimMountPath('')
    setStsVolumeClaimSize('10Gi')
    setStsVolumeClaimStorageClass('')
    setStsVolumeClaimAccessMode('ReadWriteOnce')
    setAdvancedOpen({
      runtime: false,
      resources: false,
      storage: false,
      scheduling: false,
      config: false,
      job: false,
    })
    setRestartPolicy('Always')
    setSchedule('*/5 * * * *')
    setJobCompletions('')
    setJobParallelism('')
    setJobBackoffLimit('')
    setJobActiveDeadlineSeconds('')
    setCronConcurrencyPolicy('')
    setCronSuspend(false)
    setCronSuccessfulHistoryLimit('')
    setCronFailedHistoryLimit('')
    setIncludeService(false)
    setIncludeIngress(false)
    setServiceType('ClusterIP')
    setServicePort('80')
    setServicePortRulesText('')
    setServiceSelectorText('')
    setServiceHeadless(false)
    setIngressHost('')
    setIngressServiceName('')
    setIngressClassName('')
    setIngressPath('/')
    setIngressPathType('Prefix')
    setIngressTLSSecret('')
    setIngressAnnotations('')
    setConfigData('APP_ENV=production')
    setSecretData('PASSWORD=change-me')
    setStorageSize('1Gi')
    setAccessMode('ReadWriteOnce')
    setStorageClassName('')
    setVolumeMode('Filesystem')
    setDryRunPassed(false)
    setError(undefined)
  }, [currentNamespace, namespaces, open])

  const namespace = namespaceMode === 'create' ? newNamespace.trim() : selectedNamespace.trim()
  const effectiveNamespace = namespace || 'default'
  const resourceName = safeName(name, resourceType === 'custom' ? 'custom-resource' : 'sample-app')
  const imageName = safeName(image, 'nginx:latest')
  const appLabel = resourceName

  const generatedYAML = useMemo(() => {
    if (resourceType === 'custom') return customYAML

    const docs: string[] = []
    if (namespaceMode === 'create' && newNamespace.trim()) {
      docs.push(namespaceManifest(newNamespace.trim()))
    }

    const workloadOptions: WorkloadOptions = {
      name: resourceName,
      image: imageName,
      port: containerPort,
      containerPortsText,
      envText,
      configMapEnvFrom,
      secretEnvFrom,
      configMapMountText,
      secretMountText,
      commandText,
      argsText,
      workingDir,
      imagePullPolicy,
      additionalContainersText,
      postStartCommandText,
      preStopCommandText,
      stdinEnabled,
      ttyEnabled,
      initEnabled,
      initName,
      initImage,
      initCommandText,
      cpuRequest,
      cpuLimit,
      memoryRequest,
      memoryLimit,
      readinessPath,
      livenessPath,
      startupPath,
      probePort,
      volumeName,
      volumeType,
      volumeSource,
      mountPath,
      volumeReadOnly,
      nodeName,
      nodeSelectorText,
      nodeAffinityText,
      topologySpreadText,
      tolerationsText,
      serviceAccountName,
      imagePullSecretsText,
      privileged,
      runAsUser,
      readOnlyRootFilesystem,
      stsVolumeClaimEnabled,
      stsVolumeClaimName,
      stsVolumeClaimMountPath,
      stsVolumeClaimSize,
      stsVolumeClaimStorageClass,
      stsVolumeClaimAccessMode,
    }

    if (resourceType === 'deployment' || resourceType === 'statefulset' || resourceType === 'daemonset') {
      const kind = resourceType === 'deployment' ? 'Deployment' : resourceType === 'statefulset' ? 'StatefulSet' : 'DaemonSet'
      const replicaLine = replicaTypes.has(resourceType) ? `  replicas: ${Number.parseInt(replicas, 10) || 1}\n` : ''
      const serviceNameLine = resourceType === 'statefulset' ? `  serviceName: ${resourceName}-headless\n` : ''
      const strategyLine = updateStrategy.trim()
        ? resourceType === 'deployment'
          ? `  strategy:\n    type: ${updateStrategy.trim()}\n`
          : `  updateStrategy:\n    type: ${updateStrategy.trim()}\n`
        : ''
      if (resourceType === 'statefulset') {
        docs.push(serviceManifest(effectiveNamespace, `${resourceName}-headless`, appLabel, 'ClusterIP', servicePort, containerPort, {
          headless: true,
          portRulesText: servicePortRulesText,
        }))
      }
      docs.push(`apiVersion: apps/v1
kind: ${kind}
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
  labels:
    app: ${appLabel}
spec:
${replicaLine}${serviceNameLine}${strategyLine}  selector:
    matchLabels:
      app: ${appLabel}
  template:
    metadata:
      labels:
        app: ${appLabel}
    spec:
${podSpecBlock(workloadOptions, '      ')}
${resourceType === 'statefulset' && stsVolumeClaimEnabled && stsVolumeClaimName.trim() ? `  volumeClaimTemplates:
  - metadata:
      name: ${stsVolumeClaimName.trim()}
    spec:
      accessModes:
      - ${stsVolumeClaimAccessMode}
${stsVolumeClaimStorageClass.trim() ? `      storageClassName: ${stsVolumeClaimStorageClass.trim()}\n` : ''}      resources:
        requests:
          storage: ${stsVolumeClaimSize.trim() || '10Gi'}
` : ''}
`)
    } else if (resourceType === 'pod') {
      docs.push(`apiVersion: v1
kind: Pod
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
  labels:
    app: ${appLabel}
spec:
${podSpecBlock(workloadOptions, '  ', restartPolicy)}
`)
    } else if (resourceType === 'job' || resourceType === 'cronjob') {
      const jobOptions = { ...workloadOptions, port: '' }
      const jobPolicyLines = [
        jobCompletions.trim() ? `completions: ${Number.parseInt(jobCompletions, 10) || 1}` : '',
        jobParallelism.trim() ? `parallelism: ${Number.parseInt(jobParallelism, 10) || 1}` : '',
        jobBackoffLimit.trim() ? `backoffLimit: ${Number.parseInt(jobBackoffLimit, 10) || 0}` : '',
        jobActiveDeadlineSeconds.trim() ? `activeDeadlineSeconds: ${Number.parseInt(jobActiveDeadlineSeconds, 10) || 1}` : '',
      ].filter(Boolean)
      if (resourceType === 'cronjob') {
        docs.push(`apiVersion: batch/v1
kind: CronJob
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
spec:
  schedule: ${yamlString(schedule)}
${cronConcurrencyPolicy.trim() ? `  concurrencyPolicy: ${cronConcurrencyPolicy.trim()}\n` : ''}${cronSuspend ? '  suspend: true\n' : ''}${cronSuccessfulHistoryLimit.trim() ? `  successfulJobsHistoryLimit: ${Number.parseInt(cronSuccessfulHistoryLimit, 10) || 0}\n` : ''}${cronFailedHistoryLimit.trim() ? `  failedJobsHistoryLimit: ${Number.parseInt(cronFailedHistoryLimit, 10) || 0}\n` : ''}  jobTemplate:
    spec:
${jobPolicyLines.length ? `${jobPolicyLines.map((line) => `      ${line}`).join('\n')}\n` : ''}      template:
        metadata:
          labels:
            app: ${appLabel}
        spec:
${podSpecBlock(jobOptions, '          ', 'OnFailure')}
`)
      } else {
        docs.push(`apiVersion: batch/v1
kind: Job
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
spec:
${jobPolicyLines.length ? `${jobPolicyLines.map((line) => `  ${line}`).join('\n')}\n` : ''}  template:
    metadata:
      labels:
        app: ${appLabel}
    spec:
${podSpecBlock(jobOptions, '      ', 'OnFailure')}
`)
      }
    } else if (resourceType === 'service') {
      docs.push(serviceManifest(effectiveNamespace, resourceName, appLabel, serviceType, servicePort, containerPort, {
        selectorText: serviceSelectorText,
        portRulesText: servicePortRulesText,
        headless: serviceHeadless,
      }))
    } else if (resourceType === 'ingress') {
      docs.push(ingressManifest(effectiveNamespace, resourceName, ingressHost, safeName(ingressServiceName, resourceName), servicePort, {
        className: ingressClassName,
        path: ingressPath,
        pathType: ingressPathType,
        tlsSecret: ingressTLSSecret,
        annotations: ingressAnnotations,
      }))
    } else if (resourceType === 'configmap') {
      const items = parseKeyValues(configData)
      docs.push(`apiVersion: v1
kind: ConfigMap
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
data:
${items.length === 0 ? '  APP_ENV: "production"' : items.map((item) => `  ${item.key}: ${yamlString(item.value)}`).join('\n')}
`)
    } else if (resourceType === 'secret') {
      const items = parseKeyValues(secretData)
      docs.push(`apiVersion: v1
kind: Secret
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
type: Opaque
stringData:
${items.length === 0 ? '  PASSWORD: "change-me"' : items.map((item) => `  ${item.key}: ${yamlString(item.value)}`).join('\n')}
`)
    } else if (resourceType === 'pvc') {
      docs.push(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${resourceName}
  namespace: ${effectiveNamespace}
spec:
${storageClassName.trim() ? `  storageClassName: ${storageClassName.trim()}\n` : ''}  accessModes:
  - ${accessMode}
  volumeMode: ${volumeMode}
  resources:
    requests:
      storage: ${storageSize || '1Gi'}
`)
    }

    if (networkableTypes.has(resourceType) && includeService) {
      const serviceName = `${resourceName}-svc`
      docs.push(serviceManifest(effectiveNamespace, serviceName, appLabel, serviceType, servicePort, containerPort, {
        selectorText: serviceSelectorText,
        portRulesText: servicePortRulesText,
        headless: serviceHeadless,
      }))
      if (includeIngress) {
        docs.push(ingressManifest(effectiveNamespace, `${resourceName}-ingress`, ingressHost, serviceName, servicePort, {
          className: ingressClassName,
          path: ingressPath,
          pathType: ingressPathType,
          tlsSecret: ingressTLSSecret,
          annotations: ingressAnnotations,
        }))
      }
    }

    return docs.join('---\n')
  }, [
    accessMode,
    additionalContainersText,
    argsText,
    commandText,
    configData,
    configMapEnvFrom,
    configMapMountText,
    containerPort,
    containerPortsText,
    cronConcurrencyPolicy,
    cronFailedHistoryLimit,
    cronSuccessfulHistoryLimit,
    cronSuspend,
    cpuLimit,
    cpuRequest,
    customYAML,
    effectiveNamespace,
    envText,
    imageName,
    imagePullPolicy,
    imagePullSecretsText,
    includeIngress,
    includeService,
    ingressAnnotations,
    ingressClassName,
    ingressHost,
    ingressPath,
    ingressPathType,
    ingressServiceName,
    ingressTLSSecret,
    initCommandText,
    initEnabled,
    initImage,
    initName,
    jobActiveDeadlineSeconds,
    jobBackoffLimit,
    jobCompletions,
    jobParallelism,
    livenessPath,
    memoryLimit,
    memoryRequest,
    mountPath,
    namespaceMode,
    newNamespace,
    nodeAffinityText,
    nodeName,
    nodeSelectorText,
    postStartCommandText,
    preStopCommandText,
    privileged,
    probePort,
    readinessPath,
    readOnlyRootFilesystem,
    replicas,
    resourceName,
    resourceType,
    restartPolicy,
    runAsUser,
    schedule,
    secretData,
    secretEnvFrom,
    secretMountText,
    serviceAccountName,
    serviceHeadless,
    servicePort,
    servicePortRulesText,
    serviceSelectorText,
    serviceType,
    startupPath,
    stdinEnabled,
    storageSize,
    storageClassName,
    stsVolumeClaimAccessMode,
    stsVolumeClaimEnabled,
    stsVolumeClaimMountPath,
    stsVolumeClaimName,
    stsVolumeClaimSize,
    stsVolumeClaimStorageClass,
    tolerationsText,
    topologySpreadText,
    ttyEnabled,
    updateStrategy,
    volumeMode,
    volumeName,
    volumeReadOnly,
    volumeSource,
    volumeType,
    workingDir,
  ])

  useEffect(() => {
    setDryRunPassed(false)
  }, [generatedYAML])

  if (!open) return null

  const validate = () => {
    if (!generatedYAML.trim()) return 'YAML 不能为空'
    if (resourceType === 'custom') return undefined
    if (namespacedTypes.has(resourceType) && !effectiveNamespace) return '命名空间不能为空'
    if (!name.trim()) return '资源名称不能为空'
    if (imageTypes.has(resourceType) && !image.trim()) return '镜像不能为空'
    if (namespaceMode === 'create' && !newNamespace.trim()) return '新命名空间不能为空'
    return undefined
  }

  const runApply = async (dryRun: boolean) => {
    const validationError = validate()
    if (validationError) {
      setError(validationError)
      return
    }
    setSubmitting(true)
    setError(undefined)
    try {
      await applyK8sManifest(clusterId, { yaml: generatedYAML, dry_run: dryRun })
      if (dryRun) {
        setDryRunPassed(true)
        onToast('资源校验成功', 'success')
        return
      }
      onToast('资源创建成功', 'success')
      onCreated()
      onClose()
    } catch (err) {
      const message = err instanceof Error ? err.message : '未知错误'
      onToast(`${dryRun ? '资源校验' : '资源创建'}失败: ${message}`, 'error')
      setError(message)
    } finally {
      setSubmitting(false)
    }
  }

  const copyYAML = async () => {
    try {
      await navigator.clipboard.writeText(generatedYAML)
      onToast('YAML复制成功', 'success')
    } catch (err) {
      const message = err instanceof Error ? err.message : '浏览器拒绝复制'
      onToast(`YAML复制失败: ${message}`, 'error')
    }
  }

  const toggleAdvanced = (key: keyof typeof advancedOpen) => {
    setAdvancedOpen((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  return (
    <div className="soft-modal-overlay fixed inset-0 z-[70] flex items-center justify-center px-3 py-5" onClick={(event) => {
      if (event.target === event.currentTarget) onClose()
    }}>
      <section role="dialog" aria-modal="true" aria-label="创建 Kubernetes 资源" className="soft-modal-shell flex max-h-[92vh] w-full max-w-7xl flex-col outline-none">
        <header className="soft-modal-header flex shrink-0 items-start justify-between gap-4 border-b px-5 py-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="inline-flex h-9 w-9 items-center justify-center rounded-2xl bg-primary/10 text-primary">
                <Plus className="h-4 w-4" aria-hidden="true" />
              </span>
              <div>
                <h2 className="text-base font-black text-foreground">创建 Kubernetes 资源</h2>
                <p className="mt-0.5 text-xs font-bold text-muted-foreground">Dry Run 通过后提交</p>
              </div>
            </div>
          </div>
          <button type="button" aria-label="关闭" onClick={onClose} className="soft-button inline-flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-card text-muted-foreground hover:text-foreground focus:outline-none focus:ring-4 focus:ring-primary/20">
            <X className="h-4 w-4" aria-hidden="true" />
          </button>
        </header>

        <div className="grid min-h-0 flex-1 gap-4 overflow-y-auto p-4 lg:grid-cols-[minmax(360px,0.86fr)_minmax(420px,1.14fr)]">
          <div className="space-y-4">
            <div className="rounded-2xl border border-border/80 bg-surface/70 p-4">
              <div className="grid gap-3 sm:grid-cols-2">
                <label className="text-xs font-black text-muted-foreground">
                  资源类型
                  <select
                    aria-label="资源类型"
                    value={resourceType}
                    onChange={(event) => setResourceType(event.target.value as ResourceType)}
                    className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold"
                  >
                    {resourceTypeLabels.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                  </select>
                </label>

                {resourceType !== 'custom' && (
                  <label className="text-xs font-black text-muted-foreground">
                    目标命名空间
                    <select
                      aria-label="目标命名空间"
                      value={namespaceMode === 'create' ? '__create__' : selectedNamespace}
                      onChange={(event) => {
                        if (event.target.value === '__create__') {
                          setNamespaceMode('create')
                          return
                        }
                        setNamespaceMode('existing')
                        setSelectedNamespace(event.target.value)
                      }}
                      className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold"
                    >
                      {namespaceOptions(namespaces, selectedNamespace, namespaceMode)}
                    </select>
                  </label>
                )}
              </div>

              {resourceType !== 'custom' && namespaceMode === 'create' && (
                <label className="mt-3 block text-xs font-black text-muted-foreground">
                  新命名空间
                  <input
                    aria-label="新命名空间"
                    type="text"
                    value={newNamespace}
                    onChange={(event) => setNewNamespace(event.target.value)}
                    placeholder="staging"
                    className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground"
                  />
                </label>
              )}
            </div>

            {resourceType === 'custom' ? (
              <div className="rounded-2xl border border-border/80 bg-surface/70 p-4">
                <label className="text-xs font-black text-muted-foreground">
                  自定义 YAML
                  <textarea
                    value={customYAML}
                    onChange={(event) => setCustomYAML(event.target.value)}
                    className="soft-input mt-2 min-h-[360px] w-full resize-y px-3 py-3 font-mono text-xs leading-relaxed"
                    spellCheck={false}
                  />
                </label>
              </div>
            ) : (
              <>
                <div className="rounded-2xl border border-border/80 bg-surface/70 p-4">
                  <div className="grid gap-3 sm:grid-cols-2">
                    <label className="text-xs font-black text-muted-foreground">
                      资源名称
                      <input aria-label="资源名称" type="text" value={name} onChange={(event) => setName(event.target.value)} placeholder="web" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                    </label>

                    {imageTypes.has(resourceType) && (
                      <label className="text-xs font-black text-muted-foreground">
                        镜像
                        <input aria-label="镜像" type="text" value={image} onChange={(event) => setImage(event.target.value)} placeholder="nginx:1.27" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                      </label>
                    )}

                    {replicaTypes.has(resourceType) && (
                      <label className="text-xs font-black text-muted-foreground">
                        副本数
                        <input aria-label="副本数" inputMode="numeric" value={replicas} onChange={(event) => setReplicas(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                      </label>
                    )}

                    {imageTypes.has(resourceType) && resourceType !== 'job' && resourceType !== 'cronjob' && (
                      <label className="text-xs font-black text-muted-foreground">
                        容器端口
                        <input aria-label="容器端口" inputMode="numeric" value={containerPort} onChange={(event) => setContainerPort(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                      </label>
                    )}

                    {resourceType === 'pod' && (
                      <label className="text-xs font-black text-muted-foreground">
                        重启策略
                        <select aria-label="重启策略" value={restartPolicy} onChange={(event) => setRestartPolicy(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                          <option value="Always">Always</option>
                          <option value="OnFailure">OnFailure</option>
                          <option value="Never">Never</option>
                        </select>
                      </label>
                    )}

                    {resourceType === 'cronjob' && (
                      <label className="text-xs font-black text-muted-foreground">
                        调度
                        <input aria-label="调度" value={schedule} onChange={(event) => setSchedule(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                      </label>
                    )}

                    {(resourceType === 'service' || resourceType === 'ingress') && (
                      <label className="text-xs font-black text-muted-foreground">
                        服务端口
                        <input aria-label="服务端口" inputMode="numeric" value={servicePort} onChange={(event) => setServicePort(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                      </label>
                    )}

                    {resourceType === 'service' && (
                      <>
                        <label className="text-xs font-black text-muted-foreground">
                          Service 类型
                          <select aria-label="Service 类型" value={serviceType} onChange={(event) => setServiceType(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                            <option value="ClusterIP">ClusterIP</option>
                            <option value="NodePort">NodePort</option>
                            <option value="LoadBalancer">LoadBalancer</option>
                          </select>
                        </label>
                        <label className="flex min-h-11 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground">
                          <input aria-label="Headless Service" type="checkbox" checked={serviceHeadless} onChange={(event) => setServiceHeadless(event.target.checked)} className="h-4 w-4 accent-primary" />
                          Headless Service
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Service Selector
                          <textarea aria-label="Service Selector" value={serviceSelectorText} onChange={(event) => setServiceSelectorText(event.target.value)} placeholder="app=api&#10;tier=backend" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Service 端口规则
                          <textarea aria-label="Service 端口规则" value={servicePortRulesText} onChange={(event) => setServicePortRulesText(event.target.value)} placeholder="http:80:http:TCP:30080&#10;metrics:9090:metrics:TCP" className="soft-input mt-1 min-h-[82px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                          <span className="mt-1 block text-[11px] font-bold text-muted-foreground">每行: name:port:targetPort:protocol:nodePort，nodePort 可留空。</span>
                        </label>
                      </>
                    )}

                    {resourceType === 'ingress' && (
                      <>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Host
                          <input aria-label="Host" value={ingressHost} onChange={(event) => setIngressHost(event.target.value)} placeholder="web.example.com" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          后端 Service
                          <input aria-label="后端 Service" value={ingressServiceName} onChange={(event) => setIngressServiceName(event.target.value)} placeholder={resourceName} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Ingress Class
                          <input aria-label="Ingress Class" value={ingressClassName} onChange={(event) => setIngressClassName(event.target.value)} placeholder="nginx" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          路径
                          <input aria-label="路径" value={ingressPath} onChange={(event) => setIngressPath(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Path Type
                          <select aria-label="Path Type" value={ingressPathType} onChange={(event) => setIngressPathType(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                            <option value="Prefix">Prefix</option>
                            <option value="Exact">Exact</option>
                            <option value="ImplementationSpecific">ImplementationSpecific</option>
                          </select>
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          TLS Secret
                          <input aria-label="TLS Secret" value={ingressTLSSecret} onChange={(event) => setIngressTLSSecret(event.target.value)} placeholder="web-tls" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Ingress Annotations
                          <textarea aria-label="Ingress Annotations" value={ingressAnnotations} onChange={(event) => setIngressAnnotations(event.target.value)} placeholder="nginx.ingress.kubernetes.io/rewrite-target=/" className="soft-input mt-1 min-h-[76px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                      </>
                    )}

                    {resourceType === 'pvc' && (
                      <>
                        <label className="text-xs font-black text-muted-foreground">
                          StorageClass
                          <input aria-label="StorageClass" value={storageClassName} onChange={(event) => setStorageClassName(event.target.value)} placeholder="fast-ssd" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Volume Mode
                          <select aria-label="Volume Mode" value={volumeMode} onChange={(event) => setVolumeMode(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                            <option value="Filesystem">Filesystem</option>
                            <option value="Block">Block</option>
                          </select>
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          容量
                          <input aria-label="容量" value={storageSize} onChange={(event) => setStorageSize(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          访问模式
                          <select aria-label="访问模式" value={accessMode} onChange={(event) => setAccessMode(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                            <option value="ReadWriteOnce">ReadWriteOnce</option>
                            <option value="ReadOnlyMany">ReadOnlyMany</option>
                            <option value="ReadWriteMany">ReadWriteMany</option>
                          </select>
                        </label>
                      </>
                    )}
                  </div>

                  {imageTypes.has(resourceType) && (
                    <label className="mt-3 block text-xs font-black text-muted-foreground">
                      环境变量
                      <textarea value={envText} onChange={(event) => setEnvText(event.target.value)} placeholder="KEY=value" className="soft-input mt-1 min-h-[86px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" />
                    </label>
                  )}

                  {resourceType === 'configmap' && (
                    <label className="mt-3 block text-xs font-black text-muted-foreground">
                      ConfigMap 数据
                      <textarea value={configData} onChange={(event) => setConfigData(event.target.value)} className="soft-input mt-1 min-h-[120px] w-full resize-y px-3 py-2 font-mono text-xs" />
                    </label>
                  )}

                  {resourceType === 'secret' && (
                    <label className="mt-3 block text-xs font-black text-muted-foreground">
                      Secret 数据
                      <textarea value={secretData} onChange={(event) => setSecretData(event.target.value)} className="soft-input mt-1 min-h-[120px] w-full resize-y px-3 py-2 font-mono text-xs" />
                    </label>
                  )}
                </div>

                {imageTypes.has(resourceType) && (
                  <div className="space-y-3">
                    {(resourceType === 'job' || resourceType === 'cronjob') && (
                      <AdvancedSection title="Job 策略" icon={ServerCog} open={advancedOpen.job} onToggle={() => toggleAdvanced('job')}>
                        <div className="grid gap-3 sm:grid-cols-2">
                          <label className="text-xs font-black text-muted-foreground">
                            Completions
                            <input aria-label="Completions" inputMode="numeric" value={jobCompletions} onChange={(event) => setJobCompletions(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                          </label>
                          <label className="text-xs font-black text-muted-foreground">
                            Parallelism
                            <input aria-label="Parallelism" inputMode="numeric" value={jobParallelism} onChange={(event) => setJobParallelism(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                          </label>
                          <label className="text-xs font-black text-muted-foreground">
                            Backoff Limit
                            <input aria-label="Backoff Limit" inputMode="numeric" value={jobBackoffLimit} onChange={(event) => setJobBackoffLimit(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                          </label>
                          <label className="text-xs font-black text-muted-foreground">
                            Active Deadline Seconds
                            <input aria-label="Active Deadline Seconds" inputMode="numeric" value={jobActiveDeadlineSeconds} onChange={(event) => setJobActiveDeadlineSeconds(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                          </label>
                          {resourceType === 'cronjob' && (
                            <>
                              <label className="text-xs font-black text-muted-foreground">
                                Concurrency Policy
                                <select aria-label="Concurrency Policy" value={cronConcurrencyPolicy} onChange={(event) => setCronConcurrencyPolicy(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold">
                                  <option value="">默认</option>
                                  <option value="Allow">Allow</option>
                                  <option value="Forbid">Forbid</option>
                                  <option value="Replace">Replace</option>
                                </select>
                              </label>
                              <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground">
                                <input aria-label="暂停 CronJob" type="checkbox" checked={cronSuspend} onChange={(event) => setCronSuspend(event.target.checked)} className="h-4 w-4 accent-primary" />
                                暂停 CronJob
                              </label>
                              <label className="text-xs font-black text-muted-foreground">
                                成功历史保留
                                <input aria-label="成功历史保留" inputMode="numeric" value={cronSuccessfulHistoryLimit} onChange={(event) => setCronSuccessfulHistoryLimit(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                              </label>
                              <label className="text-xs font-black text-muted-foreground">
                                失败历史保留
                                <input aria-label="失败历史保留" inputMode="numeric" value={cronFailedHistoryLimit} onChange={(event) => setCronFailedHistoryLimit(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                              </label>
                            </>
                          )}
                        </div>
                      </AdvancedSection>
                    )}

                    <AdvancedSection title="容器运行" icon={SlidersHorizontal} open={advancedOpen.runtime} onToggle={() => toggleAdvanced('runtime')}>
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="text-xs font-black text-muted-foreground">
                          启动命令
                          <textarea
                            aria-label="启动命令"
                            value={commandText}
                            onChange={(event) => setCommandText(event.target.value)}
                            className="soft-input mt-1 min-h-[80px] w-full resize-y px-3 py-2 font-mono text-xs"
                            spellCheck={false}
                          />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          启动参数
                          <textarea
                            aria-label="启动参数"
                            value={argsText}
                            onChange={(event) => setArgsText(event.target.value)}
                            className="soft-input mt-1 min-h-[80px] w-full resize-y px-3 py-2 font-mono text-xs"
                            spellCheck={false}
                          />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          工作目录
                          <input aria-label="工作目录" value={workingDir} onChange={(event) => setWorkingDir(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          拉取策略
                          <select aria-label="拉取策略" value={imagePullPolicy} onChange={(event) => setImagePullPolicy(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold">
                            <option value="">默认</option>
                            <option value="IfNotPresent">IfNotPresent</option>
                            <option value="Always">Always</option>
                            <option value="Never">Never</option>
                          </select>
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          容器端口规则
                          <textarea aria-label="容器端口规则" value={containerPortsText} onChange={(event) => setContainerPortsText(event.target.value)} placeholder="http:8080:TCP&#10;metrics:9090:TCP" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                          <span className="mt-1 block text-[11px] font-bold text-muted-foreground">每行: name:containerPort:protocol；留空时使用上方容器端口。</span>
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          附加容器
                          <textarea aria-label="附加容器" value={additionalContainersText} onChange={(event) => setAdditionalContainersText(event.target.value)} placeholder="sidecar=busybox:1.36:9000" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                          <span className="mt-1 block text-[11px] font-bold text-muted-foreground">每行: name=image:port，端口可留空。</span>
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          PostStart 命令
                          <textarea aria-label="PostStart 命令" value={postStartCommandText} onChange={(event) => setPostStartCommandText(event.target.value)} className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          PreStop 命令
                          <textarea aria-label="PreStop 命令" value={preStopCommandText} onChange={(event) => setPreStopCommandText(event.target.value)} className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs" spellCheck={false} />
                        </label>
                        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground">
                          <input aria-label="Stdin" type="checkbox" checked={stdinEnabled} onChange={(event) => setStdinEnabled(event.target.checked)} className="h-4 w-4 accent-primary" />
                          Stdin
                        </label>
                        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground">
                          <input aria-label="TTY" type="checkbox" checked={ttyEnabled} onChange={(event) => setTtyEnabled(event.target.checked)} className="h-4 w-4 accent-primary" />
                          TTY
                        </label>
                        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-primary/20 bg-primary/5 px-3 text-xs font-black text-foreground sm:col-span-2">
                          <input aria-label="启用 Init 容器" type="checkbox" checked={initEnabled} onChange={(event) => setInitEnabled(event.target.checked)} className="h-4 w-4 accent-primary" />
                          启用 Init 容器
                        </label>
                        {initEnabled && (
                          <>
                            <label className="text-xs font-black text-muted-foreground">
                              Init 容器名称
                              <input aria-label="Init 容器名称" value={initName} onChange={(event) => setInitName(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                            </label>
                            <label className="text-xs font-black text-muted-foreground">
                              Init 容器镜像
                              <input aria-label="Init 容器镜像" value={initImage} onChange={(event) => setInitImage(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                            </label>
                            <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                              Init 容器命令
                              <textarea
                                aria-label="Init 容器命令"
                                value={initCommandText}
                                onChange={(event) => setInitCommandText(event.target.value)}
                                className="soft-input mt-1 min-h-[76px] w-full resize-y px-3 py-2 font-mono text-xs"
                                spellCheck={false}
                              />
                            </label>
                          </>
                        )}
                      </div>
                    </AdvancedSection>

                    <AdvancedSection title="资源与健康检查" icon={Activity} open={advancedOpen.resources} onToggle={() => toggleAdvanced('resources')}>
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="text-xs font-black text-muted-foreground">
                          CPU Request
                          <input aria-label="CPU Request" value={cpuRequest} onChange={(event) => setCpuRequest(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          CPU Limit
                          <input aria-label="CPU Limit" value={cpuLimit} onChange={(event) => setCpuLimit(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          内存 Request
                          <input aria-label="内存 Request" value={memoryRequest} onChange={(event) => setMemoryRequest(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          内存 Limit
                          <input aria-label="内存 Limit" value={memoryLimit} onChange={(event) => setMemoryLimit(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          探针端口
                          <input aria-label="探针端口" inputMode="numeric" value={probePort} onChange={(event) => setProbePort(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Readiness 路径
                          <input aria-label="Readiness 路径" value={readinessPath} onChange={(event) => setReadinessPath(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Liveness 路径
                          <input aria-label="Liveness 路径" value={livenessPath} onChange={(event) => setLivenessPath(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Startup 路径
                          <input aria-label="Startup 路径" value={startupPath} onChange={(event) => setStartupPath(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                      </div>
                    </AdvancedSection>

                    <AdvancedSection title="配置与密钥" icon={KeyRound} open={advancedOpen.config} onToggle={() => toggleAdvanced('config')}>
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="text-xs font-black text-muted-foreground">
                          ConfigMap envFrom
                          <input aria-label="ConfigMap envFrom" value={configMapEnvFrom} onChange={(event) => setConfigMapEnvFrom(event.target.value)} placeholder="app-config" className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Secret envFrom
                          <input aria-label="Secret envFrom" value={secretEnvFrom} onChange={(event) => setSecretEnvFrom(event.target.value)} placeholder="app-secret" className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          ConfigMap 文件挂载
                          <textarea aria-label="ConfigMap 文件挂载" value={configMapMountText} onChange={(event) => setConfigMapMountText(event.target.value)} placeholder="app-config:/etc/config" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Secret 文件挂载
                          <textarea aria-label="Secret 文件挂载" value={secretMountText} onChange={(event) => setSecretMountText(event.target.value)} placeholder="app-secret:/etc/secret" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                      </div>
                    </AdvancedSection>

                    <AdvancedSection title="存储挂载" icon={HardDrive} open={advancedOpen.storage} onToggle={() => toggleAdvanced('storage')}>
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="text-xs font-black text-muted-foreground">
                          卷名称
                          <input aria-label="卷名称" value={volumeName} onChange={(event) => setVolumeName(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          挂载路径
                          <input aria-label="挂载路径" value={mountPath} onChange={(event) => setMountPath(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          卷类型
                          <select aria-label="卷类型" value={volumeType} onChange={(event) => setVolumeType(event.target.value as VolumeType)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold">
                            <option value="emptyDir">emptyDir</option>
                            <option value="pvc">PVC</option>
                            <option value="configMap">ConfigMap</option>
                            <option value="secret">Secret</option>
                            <option value="hostPath">HostPath</option>
                          </select>
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          卷来源
                          <input aria-label="卷来源" value={volumeSource} onChange={(event) => setVolumeSource(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground sm:col-span-2">
                          <input aria-label="只读挂载" type="checkbox" checked={volumeReadOnly} onChange={(event) => setVolumeReadOnly(event.target.checked)} className="h-4 w-4 accent-primary" />
                          只读挂载
                        </label>
                        {resourceType === 'statefulset' && (
                          <>
                            <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-primary/20 bg-primary/5 px-3 text-xs font-black text-foreground sm:col-span-2">
                              <input aria-label="启用 StatefulSet PVC 模板" type="checkbox" checked={stsVolumeClaimEnabled} onChange={(event) => setStsVolumeClaimEnabled(event.target.checked)} className="h-4 w-4 accent-primary" />
                              启用 StatefulSet PVC 模板
                            </label>
                            {stsVolumeClaimEnabled && (
                              <>
                                <label className="text-xs font-black text-muted-foreground">
                                  PVC 模板名称
                                  <input aria-label="PVC 模板名称" value={stsVolumeClaimName} onChange={(event) => setStsVolumeClaimName(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                                </label>
                                <label className="text-xs font-black text-muted-foreground">
                                  PVC 模板挂载路径
                                  <input aria-label="PVC 模板挂载路径" value={stsVolumeClaimMountPath} onChange={(event) => setStsVolumeClaimMountPath(event.target.value)} placeholder="/data" className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                                </label>
                                <label className="text-xs font-black text-muted-foreground">
                                  PVC 模板 StorageClass
                                  <input aria-label="PVC 模板 StorageClass" value={stsVolumeClaimStorageClass} onChange={(event) => setStsVolumeClaimStorageClass(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                                </label>
                                <label className="text-xs font-black text-muted-foreground">
                                  PVC 模板容量
                                  <input aria-label="PVC 模板容量" value={stsVolumeClaimSize} onChange={(event) => setStsVolumeClaimSize(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                                </label>
                                <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                                  PVC 模板访问模式
                                  <select aria-label="PVC 模板访问模式" value={stsVolumeClaimAccessMode} onChange={(event) => setStsVolumeClaimAccessMode(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold">
                                    <option value="ReadWriteOnce">ReadWriteOnce</option>
                                    <option value="ReadOnlyMany">ReadOnlyMany</option>
                                    <option value="ReadWriteMany">ReadWriteMany</option>
                                  </select>
                                </label>
                              </>
                            )}
                          </>
                        )}
                      </div>
                    </AdvancedSection>

                    <AdvancedSection title="调度与安全" icon={Shield} open={advancedOpen.scheduling} onToggle={() => toggleAdvanced('scheduling')}>
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="text-xs font-black text-muted-foreground">
                          指定节点
                          <input aria-label="指定节点" value={nodeName} onChange={(event) => setNodeName(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          ServiceAccount
                          <input aria-label="ServiceAccount" value={serviceAccountName} onChange={(event) => setServiceAccountName(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          镜像拉取密钥
                          <input aria-label="镜像拉取密钥" value={imagePullSecretsText} onChange={(event) => setImagePullSecretsText(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Run As User
                          <input aria-label="Run As User" inputMode="numeric" value={runAsUser} onChange={(event) => setRunAsUser(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold" />
                        </label>
                        {(resourceType === 'deployment' || resourceType === 'statefulset' || resourceType === 'daemonset') && (
                          <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                            更新策略
                            <select aria-label="更新策略" value={updateStrategy} onChange={(event) => setUpdateStrategy(event.target.value)} className="soft-input mt-1 h-10 w-full px-3 text-sm font-bold">
                              <option value="">默认</option>
                              {resourceType === 'deployment' && <option value="Recreate">Recreate</option>}
                              <option value="RollingUpdate">RollingUpdate</option>
                              {(resourceType === 'statefulset' || resourceType === 'daemonset') && <option value="OnDelete">OnDelete</option>}
                            </select>
                          </label>
                        )}
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          节点选择器
                          <textarea aria-label="节点选择器" value={nodeSelectorText} onChange={(event) => setNodeSelectorText(event.target.value)} className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Node Affinity
                          <textarea aria-label="Node Affinity" value={nodeAffinityText} onChange={(event) => setNodeAffinityText(event.target.value)} placeholder="disk=ssd&#10;zone=east" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Topology Spread
                          <textarea aria-label="Topology Spread" value={topologySpreadText} onChange={(event) => setTopologySpreadText(event.target.value)} placeholder="topologyKey=topology.kubernetes.io/zone&#10;maxSkew=1&#10;whenUnsatisfiable=DoNotSchedule" className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-2">
                          Tolerations
                          <textarea aria-label="Tolerations" value={tolerationsText} onChange={(event) => setTolerationsText(event.target.value)} className="soft-input mt-1 min-h-[72px] w-full resize-y px-3 py-2 font-mono text-xs" spellCheck={false} />
                        </label>
                        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-danger/20 bg-danger/5 px-3 text-xs font-black text-foreground">
                          <input aria-label="Privileged" type="checkbox" checked={privileged} onChange={(event) => setPrivileged(event.target.checked)} className="h-4 w-4 accent-danger" />
                          Privileged
                        </label>
                        <label className="flex min-h-10 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground">
                          <input aria-label="只读根文件系统" type="checkbox" checked={readOnlyRootFilesystem} onChange={(event) => setReadOnlyRootFilesystem(event.target.checked)} className="h-4 w-4 accent-primary" />
                          只读根文件系统
                        </label>
                      </div>
                    </AdvancedSection>
                  </div>
                )}

                {networkableTypes.has(resourceType) && (
                  <div className="rounded-2xl border border-border/80 bg-surface/70 p-4">
                    <label className="flex items-center gap-2 text-sm font-black text-foreground">
                      <input type="checkbox" checked={includeService} onChange={(event) => setIncludeService(event.target.checked)} className="h-4 w-4 accent-primary" />
                      同时创建 Service
                    </label>
                    {includeService && (
                      <div className="mt-3 grid gap-3 sm:grid-cols-3">
                        <label className="text-xs font-black text-muted-foreground">
                          Service 类型
                          <select value={serviceType} onChange={(event) => setServiceType(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                            <option value="ClusterIP">ClusterIP</option>
                            <option value="NodePort">NodePort</option>
                            <option value="LoadBalancer">LoadBalancer</option>
                          </select>
                        </label>
                        <label className="text-xs font-black text-muted-foreground">
                          Service 端口
                          <input inputMode="numeric" value={servicePort} onChange={(event) => setServicePort(event.target.value.replace(/[^\d]/g, ''))} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                        </label>
                        <label className="flex items-end gap-2 pb-3 text-sm font-black text-foreground">
                          <input type="checkbox" checked={includeIngress} onChange={(event) => setIncludeIngress(event.target.checked)} className="h-4 w-4 accent-primary" />
                          创建 Ingress
                        </label>
                        <label className="flex min-h-11 items-center gap-2 rounded-2xl border border-border/70 bg-card/80 px-3 text-xs font-black text-foreground sm:col-span-3">
                          <input type="checkbox" checked={serviceHeadless} onChange={(event) => setServiceHeadless(event.target.checked)} className="h-4 w-4 accent-primary" />
                          Headless Service
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-3">
                          Service Selector
                          <textarea value={serviceSelectorText} onChange={(event) => setServiceSelectorText(event.target.value)} placeholder="app=web" className="soft-input mt-1 min-h-[68px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                        <label className="text-xs font-black text-muted-foreground sm:col-span-3">
                          Service 端口规则
                          <textarea value={servicePortRulesText} onChange={(event) => setServicePortRulesText(event.target.value)} placeholder="http:80:http:TCP:30080&#10;metrics:9090:metrics:TCP" className="soft-input mt-1 min-h-[78px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                        </label>
                        {includeIngress && (
                          <>
                            <label className="text-xs font-black text-muted-foreground sm:col-span-3">
                              Ingress Host
                              <input value={ingressHost} onChange={(event) => setIngressHost(event.target.value)} placeholder="web.example.com" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                            </label>
                            <label className="text-xs font-black text-muted-foreground">
                              Ingress Class
                              <input value={ingressClassName} onChange={(event) => setIngressClassName(event.target.value)} placeholder="nginx" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                            </label>
                            <label className="text-xs font-black text-muted-foreground">
                              路径
                              <input value={ingressPath} onChange={(event) => setIngressPath(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold" />
                            </label>
                            <label className="text-xs font-black text-muted-foreground">
                              Path Type
                              <select value={ingressPathType} onChange={(event) => setIngressPathType(event.target.value)} className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold">
                                <option value="Prefix">Prefix</option>
                                <option value="Exact">Exact</option>
                                <option value="ImplementationSpecific">ImplementationSpecific</option>
                              </select>
                            </label>
                            <label className="text-xs font-black text-muted-foreground sm:col-span-3">
                              TLS Secret
                              <input value={ingressTLSSecret} onChange={(event) => setIngressTLSSecret(event.target.value)} placeholder="web-tls" className="soft-input mt-1 h-11 w-full px-3 text-sm font-bold placeholder:text-muted-foreground" />
                            </label>
                            <label className="text-xs font-black text-muted-foreground sm:col-span-3">
                              Ingress Annotations
                              <textarea value={ingressAnnotations} onChange={(event) => setIngressAnnotations(event.target.value)} placeholder="nginx.ingress.kubernetes.io/rewrite-target=/" className="soft-input mt-1 min-h-[76px] w-full resize-y px-3 py-2 font-mono text-xs placeholder:text-muted-foreground" spellCheck={false} />
                            </label>
                          </>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </>
            )}

            {error && (
              <div className="rounded-2xl border border-danger/25 bg-danger/10 px-4 py-3 text-sm font-bold text-danger">
                {error}
              </div>
            )}
          </div>

          <div className="flex min-h-[520px] flex-col rounded-2xl border border-border/80 bg-code/95">
            <div className="flex shrink-0 items-center justify-between gap-3 border-b border-white/10 px-4 py-3">
              <div className="flex items-center gap-2 text-sm font-black text-primary-foreground">
                <FileCode2 className="h-4 w-4" aria-hidden="true" />
                YAML
              </div>
              <button type="button" onClick={copyYAML} className="soft-button inline-flex h-9 items-center gap-1.5 border border-white/10 bg-white/10 px-3 text-xs font-black text-primary-foreground hover:bg-white/15">
                <Copy className="h-3.5 w-3.5" aria-hidden="true" />
                复制
              </button>
            </div>
            <textarea
              aria-label="YAML 预览"
              readOnly={resourceType !== 'custom'}
              value={generatedYAML}
              onChange={(event) => setCustomYAML(event.target.value)}
              className="min-h-0 flex-1 resize-none overflow-auto bg-transparent px-4 py-4 font-mono text-xs leading-relaxed text-primary-foreground outline-none"
              spellCheck={false}
            />
          </div>
        </div>

        <footer className="soft-modal-footer flex shrink-0 flex-wrap items-center justify-between gap-3 border-t px-5 py-4">
          <div className="text-xs font-bold text-muted-foreground">
            {dryRunPassed ? (
              <span className="inline-flex items-center gap-1.5 text-success"><CheckCircle2 className="h-4 w-4" aria-hidden="true" />Dry Run 已通过</span>
            ) : '等待 Dry Run'}
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <button type="button" onClick={onClose} disabled={submitting} className="soft-button min-h-11 border border-border bg-card px-4 text-sm font-black text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50">
              取消
            </button>
            <button type="button" onClick={() => runApply(true)} disabled={submitting} className="soft-button inline-flex min-h-11 items-center gap-2 border border-primary/30 bg-primary/10 px-4 text-sm font-black text-primary hover:bg-primary/15 disabled:cursor-not-allowed disabled:opacity-50">
              {submitting ? '校验中...' : 'Dry Run'}
            </button>
            <button type="button" onClick={() => runApply(false)} disabled={submitting || !dryRunPassed} className="soft-button min-h-11 bg-primary px-5 text-sm font-black text-primary-foreground shadow-sm hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50">
              创建资源
            </button>
          </div>
        </footer>
      </section>
    </div>
  )
}
