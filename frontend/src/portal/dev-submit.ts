import { isSafeSegment } from './api.ts'
import type { WorkloadDraftPayload } from './workload-draft'

export async function submitDevWorkload(
  clusterId: string, readToken: string, writeToken: string, payload: WorkloadDraftPayload,
): Promise<void> {
  const id = payload.metadata.id
  const projectId = payload.projectId
  if (![clusterId, projectId, id].every(isSafeSegment)) {
    throw new Error('集群、项目或 Workload ID 无效')
  }
  if (readToken.length < 32 || writeToken.length < 32 || readToken === writeToken) {
    throw new Error('开发提交必须使用两个不同的强凭证')
  }
  const url = '/api/v1/operator/clusters/' + encodeURIComponent(clusterId) +
    '/projects/' + encodeURIComponent(projectId) +
    '/workloads/' + encodeURIComponent(id)
  const response = await fetch(url, {
    method: 'PUT',
    credentials: 'omit',
    redirect: 'error',
    cache: 'no-store',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      Authorization: 'Bearer ' + readToken,
      'X-Operator-Write-Token': writeToken,
    },
    body: JSON.stringify(payload),
    signal: AbortSignal.timeout(15000),
  })
  if (response.status === 204) return
  if (response.status === 401) throw new Error('开发写入凭证未启用或授权失败')
  if (response.status === 403) throw new Error('当前浏览器来源被拒绝')
  if (response.status === 404) throw new Error('此项目/集群不可写入或开发写入入口未启用')
  if (response.status === 409) throw new Error('项目、ComputePool、代际或任务生命周期冲突，请刷新资源')
  throw new Error('提交失败（HTTP ' + response.status + '），未确认创建')
}
