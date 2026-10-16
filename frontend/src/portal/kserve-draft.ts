import type { ResourceRow } from './api'

export interface ServingProject { id: string }

export interface ServingDraftInput {
  id: string
  projectId: string
  namespace: string
  image: string
  argsJson: string
  wholeGpuCount: number
}

export interface KServeManifest {
  apiVersion: 'serving.kserve.io/v1beta1'
  kind: 'InferenceService'
  metadata: {
    name: string
    namespace: string
    annotations: { 'ai.compute/project-id': string }
  }
  spec: {
    predictor: {
      minReplicas: 1
      maxReplicas: 1
      containers: [{
        name: 'kserve-container'
        image: string
        args: string[]
        ports: [{ containerPort: 8000; protocol: 'TCP' }]
        resources: {
          requests: { cpu: '1'; memory: '16Gi'; 'nvidia.com/gpu': number }
          limits: { cpu: '1'; memory: '16Gi'; 'nvidia.com/gpu': number }
        }
      }]
    }
  }
}

export interface ServingDraftResult { errors: string[]; manifest: KServeManifest | null }

const SAFE_ID = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/
const DNS_LABEL = /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/
const PINNED_IMAGE = /^[^\s\x00-\x1f]+@sha256:[0-9a-f]{64}$/

function validDNSName(value: string): boolean {
  return value.length > 0 && value.length <= 253 &&
    value.split('.').every(part => part.length <= 63 && DNS_LABEL.test(part))
}

export function servingProjects(rows: ResourceRow[]): ServingProject[] {
  const ids = new Set<string>()
  for (const row of rows) {
    // The only source is the already-scoped actual Go snapshot. This is a
    // suggestion for the operator, not an authorization grant for Kubernetes.
    if (row.error || row.desired.deletionTimestamp) continue
    const id = row.desired.spec.projectID
    if (typeof id === 'string' && SAFE_ID.test(id)) ids.add(id)
  }
  return [...ids].sort().map(id => ({ id }))
}

export function buildKServeDraft(input: ServingDraftInput, projects: ServingProject[]): ServingDraftResult {
  const errors: string[] = []
  if (!validDNSName(input.id)) errors.push('服务 ID 必须是 Kubernetes DNS 名称')
  if (!projects.some(p => p.id === input.projectId && SAFE_ID.test(p.id))) {
    errors.push('项目必须来自当前受保护的资源快照')
  }
  if (input.namespace.length > 63 || !DNS_LABEL.test(input.namespace)) {
    errors.push('请输入经过管理员确认的目标 Namespace')
  }
  if (!PINNED_IMAGE.test(input.image) || input.image.length > 512) {
    errors.push('推理容器镜像必须使用真实 sha256 digest 固定版本')
  }
  if (!Number.isSafeInteger(input.wholeGpuCount) || input.wholeGpuCount < 1 ||
      input.wholeGpuCount > 8) {
    errors.push('当前仅支持 1–8 张整数 NVIDIA GPU 的离线交付清单')
  }
  let args: string[] = []
  try {
    const value: unknown = JSON.parse(input.argsJson)
    if (!Array.isArray(value) || value.length > 48 ||
        !value.every(arg => typeof arg === 'string' && arg.length > 0 &&
          arg.length <= 2048 && !arg.includes('\0'))) {
      errors.push('Args 必须是 JSON 字符串数组，最多 48 项')
    } else {
      args = value as string[]
    }
  } catch {
    errors.push('Args 必须是合法 JSON 字符串数组')
  }
  if (errors.length) return { errors, manifest: null }

  const quota = {
    cpu: '1' as const, memory: '16Gi' as const, 'nvidia.com/gpu': input.wholeGpuCount,
  }
  return {
    errors: [],
    manifest: {
      apiVersion: 'serving.kserve.io/v1beta1',
      kind: 'InferenceService',
      metadata: {
        name: input.id,
        namespace: input.namespace,
        annotations: { 'ai.compute/project-id': input.projectId },
      },
      spec: {
        predictor: {
          minReplicas: 1, maxReplicas: 1,
          containers: [{
            name: 'kserve-container', image: input.image,
            args,
            ports: [{ containerPort: 8000, protocol: 'TCP' }],
            resources: { requests: { ...quota }, limits: { ...quota } },
          }],
        },
      },
    },
  }
}
