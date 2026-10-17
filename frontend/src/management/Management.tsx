'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Input,
  Label,
} from '@/components/ui'
import { useAuth } from '@/management/hooks/useAuth'
import { useProjects } from '@/management/hooks/useProjects'
import { useInventory } from '@/management/hooks/useInventory'
import { useResource } from '@/management/hooks/useResource'
import { StaleReceiptReadError, useReceipt } from '@/management/hooks/useReceipt'
import { useOperation } from '@/management/hooks/useOperation'
import { StaleEvidenceReadError, useEvidence } from '@/management/hooks/useEvidence'
import {
  listRecentOperations,
  createOperationId,
  type Scope,
  type Kind,
  type ProjectAccess,
  type Receipt,
  type WorkloadInventoryItem,
  type RecentOperation,
} from '@/management/api'
import {
  parseAuthorizedPool,
  buildAuthorizedWorkloadRequest,
  poolForCurrentSession,
  type VerifiedPoolSelection,
} from '@/management/workload-composer'
import type { EvidenceCheck } from '@/management/evidence-check'

const cardStyle: React.CSSProperties = {
  border: '1px solid',
  borderRadius: '12px',
  padding: '24px',
  marginBottom: '24px',
}

function errMsg(e: unknown, fallback: string): string {
  return e instanceof Error ? e.message : fallback
}

type Feedback = { kind: 'status' | 'alert'; text: string } | null

export function ManagementApp() {
  const {
    issuer,
    session,
    configured,
    authenticated,
    busy,
    signIn,
    signOut,
    token,
  } = useAuth()

  const { projects, truncated: projectListTruncated, loadProjects } = useProjects()

  const workloadInv = useInventory()
  const poolInv = useInventory()

  const {
    desired,
    clearDesired,
    readResource: readResourceFn,
    writeResource: writeResourceFn,
    deleteResource: deleteResourceFn,
  } = useResource()

  const { readReceipt: readReceiptFn, invalidate: invalidateReceipt } = useReceipt()

  const { pointer: operationPointer, remember, recover, forget } = useOperation()

  const {
    readBackDesired: readBackDesiredFn,
    refreshEvidenceChain: refreshEvidenceChainFn,
    invalidate: invalidateEvidence,
  } = useEvidence()

  const [cluster, setCluster] = useState('')
  const [project, setProject] = useState('')
  const [resource, setResource] = useState('')
  const [kind, setKind] = useState<Kind>('ComputePool')
  const [workloadPoolId, setWorkloadPoolId] = useState('')
  const [workloadID, setWorkloadID] = useState('')
  const [workloadImage, setWorkloadImage] = useState('')
  const [workloadAcceleratorClass, setWorkloadAcceleratorClass] = useState('')
  const [workloadQuota, setWorkloadQuota] = useState(1)
  const [workloadCommandJSON, setWorkloadCommandJSON] = useState(
    '["python","train.py"]'
  )
  const [requestJSON, setRequestJSON] = useState(
    JSON.stringify(
      {
        expectedGeneration: 0,
        spec: {
          accelerators: [{ class: 'h100', quota: 2 }],
          acceleratorBindings: [{ class: 'h100', resourceName: 'nvidia.com/gpu', flavor: 'h100' }],
          scheduling: { mode: 'kueue' },
        },
      },
      null,
      2
    )
  )
  const [operationId, setOperationId] = useState('')
  const [activeReceipt, setActiveReceipt] = useState<Receipt | null>(null)
  const [activeEvidence, setActiveEvidence] = useState<EvidenceCheck | null>(null)
  const [evidenceCheckedAt, setEvidenceCheckedAt] = useState<string | null>(null)
  const [verifiedSelection, setVerifiedSelection] = useState<VerifiedPoolSelection | null>(null)
  const [recentOps, setRecentOps] = useState<RecentOperation[]>([])
  const [feedback, setFeedback] = useState<Feedback>(null)

  const scope: Scope = { cluster, project, kind, resource }
  // Updated on each render so a response for a superseded session/scope
  // cannot repopulate a cleared receipt after a form or identity change.
  const receiptContext = useRef({ session, authenticated, cluster, project, operationId })
  receiptContext.current = { session, authenticated, cluster, project, operationId }

  // Vue 的 clearResult() 等价物：任何新的授权读取/回执查询都必须先作废旧的
  // 回执与证据状态，避免旧 note 与新 desired 区在 DOM 中同时出现。
  const clearResult = useCallback(() => {
    invalidateReceipt()
    invalidateEvidence()
    setActiveReceipt(null)
    setActiveEvidence(null)
    setEvidenceCheckedAt(null)
    setFeedback(null)
  }, [invalidateReceipt, invalidateEvidence])

  // Typed scope edits, project selection and OIDC identity transitions also
  // invalidate a pending read, even when the user does not click a new query.
  useEffect(() => {
    clearResult()
  }, [session, authenticated, cluster, project, operationId, clearResult])

  // Load any one-tab recovery pointer from sessionStorage on mount so the
  // "恢复操作范围与 ID" button can appear after a reload + re-login.
  useEffect(() => {
    recover()
  }, [recover])

  const handleSignIn = useCallback(() => {
    signIn()
  }, [signIn])

  const handleSignOut = useCallback(() => {
    clearResult()
    signOut()
    setVerifiedSelection(null)
  }, [clearResult, signOut])

  const handleLoadProjects = useCallback(async () => {
    try {
      const msg = await loadProjects(token())
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '查询项目失败') })
    }
  }, [loadProjects, token])

  const handleSelectProject = useCallback((item: ProjectAccess) => {
    setCluster(item.clusterId)
    setProject(item.projectId)
    setResource('')
    setKind('ComputePool')
    clearDesired()
    setOperationId('')
    setVerifiedSelection(null)
    clearResult()
  }, [clearDesired, clearResult])

  const handleListWorkloads = useCallback(async (after?: string) => {
    try {
      const msg = await workloadInv.listWorkloads(cluster, project, token(), after)
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '查询资源失败') })
    }
  }, [cluster, project, token, workloadInv.listWorkloads])

  const handleListComputePools = useCallback(async (after?: string) => {
    try {
      const msg = await poolInv.listComputePools(cluster, project, token(), after)
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '查询 ComputePool 失败') })
    }
  }, [cluster, project, token, poolInv.listComputePools])

  const handleRead = useCallback(async () => {
    // 镜像 Vue read()：读取前清空回执与证据状态，保证 "Generation:" 等断言
    // 在 DOM 中只有一个匹配元素（desired 区）。
    clearResult()
    try {
      const { message: msg } = await readResourceFn(scope, token())
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '读取资源失败') })
    }
  }, [scope, token, readResourceFn, clearResult])

  const handleWrite = useCallback(async () => {
    try {
      JSON.parse(requestJSON)
      if (
        !window.confirm(
          '确认向项目 ' + project + ' 提交 ' + kind + '/' + resource + ' Desired 变更？'
        )
      )
        return
      // Generate a fresh idempotency key for each new mutation. The server
      // binds a key to an identical operation digest and rejects mismatched
      // reuse, so reuse is reserved for explicit retry of an identical request.
      const opId = createOperationId()
      // Persist the recovery pointer before dispatch so an ambiguous network
      // outcome can still be resolved by re-reading the original ID.
      remember(scope, 'PUT', opId)
      setOperationId(opId)
      const msg = await writeResourceFn(scope, token(), requestJSON, opId)
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '提交失败') })
    }
  }, [scope, token, writeResourceFn, requestJSON, remember, project, kind, resource])

  const handleRemove = useCallback(async () => {
    if (!desired) return
    if (
      !window.confirm(
        '确认发起删除 ' +
          desired.kind +
          '/' +
          desired.id +
          '（Generation ' +
          desired.generation +
          '）？此操作不会立即删除真实 GPU 资源。'
      )
    )
      return
    try {
      const opId = createOperationId()
      remember(scope, 'DELETE', opId)
      setOperationId(opId)
      const msg = await deleteResourceFn(scope, token(), desired.generation, opId)
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '删除失败') })
    }
  }, [scope, token, deleteResourceFn, desired, remember])

  const handleReceipt = useCallback(async () => {
    const submitted = { session, cluster, project, operationId }
    clearResult()
    const stillCurrent = () => {
      const current = receiptContext.current
      return current.authenticated &&
        current.session === submitted.session &&
        current.cluster === submitted.cluster &&
        current.project === submitted.project &&
        current.operationId === submitted.operationId
    }
    try {
      const accessToken = token()
      const { receipt, message: msg } = await readReceiptFn(
        cluster, project, operationId, accessToken
      )
      if (!stillCurrent()) return
      setActiveReceipt(receipt)
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      if (e instanceof StaleReceiptReadError || !stillCurrent()) return
      setFeedback({ kind: 'alert', text: errMsg(e, '查询回执失败') })
    }
  }, [session, cluster, project, operationId, token, readReceiptFn, clearResult])

  const handleReadBackCheck = useCallback(async () => {
    if (!activeReceipt) return
    const submitted = { session, cluster, project, operationId }
    const stillCurrent = () => {
      const current = receiptContext.current
      return current.authenticated && current.session === submitted.session &&
        current.cluster === submitted.cluster && current.project === submitted.project &&
        current.operationId === submitted.operationId
    }
    setActiveEvidence(null)
    try {
      const accessToken = token()
      const { check, message: msg } = await readBackDesiredFn(
        activeReceipt, cluster, project, operationId, accessToken
      )
      if (!stillCurrent()) return
      setActiveEvidence(check)
      setEvidenceCheckedAt(new Date().toISOString())
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      if (e instanceof StaleEvidenceReadError || !stillCurrent()) return
      setFeedback({ kind: 'alert', text: errMsg(e, '回读 Desired 失败') })
    }
  }, [activeReceipt, session, cluster, project, operationId, token, readBackDesiredFn])

  const handleEvidenceRefresh = useCallback(async () => {
    // Read the original audited operation on the server; no implicit retry.
    const submitted = { session, cluster, project, operationId }
    clearResult()
    const stillCurrent = () => {
      const current = receiptContext.current
      return current.authenticated && current.session === submitted.session &&
        current.cluster === submitted.cluster && current.project === submitted.project &&
        current.operationId === submitted.operationId
    }
    try {
      const accessToken = token()
      const { check, message: msg } = await refreshEvidenceChainFn(
        cluster, project, operationId, accessToken
      )
      if (!stillCurrent()) return
      setActiveEvidence(check)
      setEvidenceCheckedAt(new Date().toISOString())
      setFeedback({ kind: 'status', text: msg })
    } catch (e) {
      if (e instanceof StaleEvidenceReadError || !stillCurrent()) return
      setFeedback({ kind: 'alert', text: errMsg(e, '证据链核对失败') })
    }
  }, [session, cluster, project, operationId, token, refreshEvidenceChainFn, clearResult])

  const handleListRecentOps = useCallback(async () => {
    try {
      await token()
      const result = await listRecentOperations(cluster, project, token())
      const items = result.value?.items ?? []
      setRecentOps(items)
      setFeedback({
        kind: 'status',
        text: '已读取 ' + items.length + ' 条本人历史操作；仅用于找回原始 ID，不证明执行或删除完成',
      })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '查询最近操作失败') })
    }
  }, [cluster, project, token])

  const handleCopyOperationId = useCallback(async () => {
    await navigator.clipboard.writeText(operationId)
  }, [operationId])

  const handleSelectPoolRow = useCallback((item: WorkloadInventoryItem) => {
    setWorkloadPoolId(item.id)
    setVerifiedSelection(null)
    clearDesired()
    setOperationId('')
    clearResult()
  }, [clearDesired, clearResult])

  const handleSelectWorkloadRow = useCallback((item: WorkloadInventoryItem) => {
    setKind('Workload')
    setResource(item.id)
    setOperationId('')
    clearDesired()
    setVerifiedSelection(null)
    clearResult()
  }, [clearDesired, clearResult])

  const handleVerifyPool = useCallback(async () => {
    clearResult()
    try {
      if (!workloadPoolId) throw new Error('请先选择要验证的 ComputePool')
      await token()
      const verifyScope: Scope = { cluster, project, kind: 'ComputePool', resource: workloadPoolId }
      const { desired: poolDesired } = await readResourceFn(verifyScope, token())
      if (!poolDesired) throw new Error('未读取到授权 ComputePool')
      const pool = parseAuthorizedPool(cluster, project, poolDesired)
      setVerifiedSelection({ pool, session: session as object })
      setWorkloadAcceleratorClass(pool.accelerators[0]?.class ?? '')
      setWorkloadQuota(1)
      // 反馈消息不能包含 "已确认 research/pool-a"：确认行本身已渲染该文本，
      // 否则 Playwright getByText 会命中两个元素。
      setFeedback({ kind: 'status', text: '已读取并验证授权 ComputePool；尚未创建 Workload' })
    } catch (e) {
      setVerifiedSelection(null)
      setFeedback({ kind: 'alert', text: errMsg(e, '验证 ComputePool 失败') })
    }
  }, [cluster, project, workloadPoolId, session, token, readResourceFn, clearResult])

  const handleGenerateWorkload = useCallback(() => {
    // 镜像 Vue prepareWorkloadRequest()：清空旧回执/证据/desired 与旧操作 ID，
    // 新请求将由下一次 PUT 生成全新幂等键。
    clearResult()
    clearDesired()
    setOperationId('')
    try {
      const pool = poolForCurrentSession(
        verifiedSelection, session, authenticated, cluster, project, workloadPoolId
      )
      if (!pool) throw new Error('请先读取并验证授权 ComputePool')
      const { target, requestJSON: json } = buildAuthorizedWorkloadRequest(
        pool,
        {
          id: workloadID,
          image: workloadImage,
          acceleratorClass: workloadAcceleratorClass,
          quota: workloadQuota,
          commandJSON: workloadCommandJSON,
        },
        cluster,
        project
      )
      setKind(target.kind)
      setResource(target.resource)
      setRequestJSON(json)
      setFeedback({ kind: 'status', text: '已生成 Workload 创建请求；请检查请求体后确认提交' })
    } catch (e) {
      setFeedback({ kind: 'alert', text: errMsg(e, '生成 Workload 请求失败') })
    }
  }, [
    verifiedSelection, session, authenticated, cluster, project, workloadPoolId,
    workloadID, workloadImage, workloadAcceleratorClass, workloadQuota, workloadCommandJSON,
    clearResult, clearDesired,
  ])

  const handleRestoreOperation = useCallback(() => {
    const p = recover()
    if (p) {
      setCluster(p.cluster)
      setProject(p.project)
      setKind(p.kind)
      setResource(p.resource)
      setOperationId(p.operationId)
    }
  }, [recover])

  const handleDiscardOperation = useCallback(() => {
    forget()
    setOperationId('')
    clearResult()
  }, [forget, clearResult])

  return (
    <div className="management-shell max-w-[1040px] mx-auto pt-6 pb-20 px-4 text-sm font-sans text-[#20242a]">
      <header className="management-header flex items-center justify-between gap-4 flex-wrap mb-4">
        <div className="flex items-center gap-2">
          <div className="brandmark h-5 w-5 rounded-md bg-primary grid place-items-center text-primary-foreground text-[10px] font-bold">
            G
          </div>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">计算资源管理 · 本机试用</h1>
            <small className="text-xs uppercase tracking-widest text-[#66707a]">
              GPU COMPUTE · ISOLATED MANAGEMENT
            </small>
          </div>
        </div>
        <span className="management-warning text-xs font-semibold text-[#b84f56]">
          LOCALHOST ONLY · NOT FOR PUBLIC DEPLOYMENT
        </span>
      </header>

      <p className="management-note text-sm text-[#a46c27] bg-[#fcf4e8] border border-[#e8eaee] rounded p-4 mb-4">
        此工作台独立于原只读 Portal。浏览器使用 OAuth S256 PKCE 获取访问令牌；服务器重新校验令牌与 PostgreSQL 项目授权。这里只证明 Desired 状态操作，不代表 Kueue 准入或 GPU 执行成功。
      </p>

      {/* 区域 1: 登录与身份 */}
      <Card style={cardStyle}>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">1. 登录与身份</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-[#66707a]">
            身份提供商、公开客户端 ID 由部署时配置；Token 仅保留在页面内存中，刷新后需重新登录。禁止将机密客户端 Secret 写入浏览器。
          </p>
          {!configured ? (
            <div className="management-status text-sm text-[#66707a]">
              尚未配置 OIDC：请设置{' '}
              <code className="text-[#20242a] font-mono text-sm">
                VITE_MANAGEMENT_OIDC_ISSUER
              </code>{' '}
              和{' '}
              <code className="text-[#20242a] font-mono text-sm">
                VITE_MANAGEMENT_OIDC_CLIENT_ID
              </code>{' '}
              后重新启动本机前端。
            </div>
          ) : (
            <div className="management-status text-sm text-[#66707a]">
              Issuer: <code className="text-[#20242a] font-mono text-sm">{issuer}</code> ·{' '}
              {authenticated ? '已取得短期访问令牌（服务端仍逐次验证）' : '尚未登录'}
            </div>
          )}
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleSignIn}
              disabled={!configured || busy}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              通过 OIDC 登录
            </Button>
            <Button
              onClick={handleSignOut}
              disabled={!authenticated}
              variant="outline"
              className="px-4 py-2 rounded-md"
            >
              清除本页令牌
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* 区域 2: 项目与资源 */}
      <Card style={cardStyle}>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">2. 项目与资源</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-[#66707a]">
            下列项目与角色来自服务端当前 OIDC 身份、PostgreSQL 授权和项目绑定；点击选择，不依赖浏览器声明角色。
          </p>
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleLoadProjects}
              disabled={!authenticated || busy}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询当前账号的项目
            </Button>
          </div>
          {projects.length > 0 && (
            <div className="management-actions flex gap-2 flex-wrap">
              {projects.map((item) => (
                <Button
                  key={item.projectId + '/' + item.clusterId}
                  onClick={() => handleSelectProject(item)}
                  disabled={busy}
                  variant="outline"
                  className="px-4 py-2 rounded-md border"
                >
                  使用 {item.projectId} ({item.clusterId}) · {item.role}
                </Button>
              ))}
            </div>
          )}
          {projectListTruncated && (
            <p className="text-xs text-[#66707a]">
              只显示前 100 个项目，列表不完整。请联系管理员或手动输入授权项目。
            </p>
          )}
          <div className="management-actions flex gap-2">
            <Button
              onClick={() => handleListWorkloads()}
              disabled={!authenticated || busy || !cluster || !project}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询项目 Workload 列表（每页 25 条）
            </Button>
          </div>
          {workloadInv.items.length > 0 && (
            <div
              role="region"
              aria-label="授权 Workload 列表"
              className="management-status text-sm text-[#34486b] bg-[#edf2fc] rounded p-3"
            >
              <p>
                当前项目 {project} · {workloadInv.items.length} 条已读取（仅 Desired 元数据）。
                不包含 Kueue 准入、Agent 状态或执行成功证明。
              </p>
              <ul className="mt-2 text-sm text-[#20242a]">
                {workloadInv.items.map((item) => (
                  <li key={item.id} className="flex items-center gap-2">
                    <span>
                      {item.id} · Generation {item.generation} ·{' '}
                      {item.deleting ? '删除请求已发起' : '未请求删除'}
                    </span>
                    <Button
                      onClick={() => handleSelectWorkloadRow(item)}
                      disabled={busy}
                      variant="outline"
                      size="sm"
                      className="px-2 py-1 border"
                    >
                      选择 Workload {item.id}
                    </Button>
                  </li>
                ))}
              </ul>
              {workloadInv.nextCursor && (
                <Button
                  onClick={() => handleListWorkloads(workloadInv.nextCursor ?? undefined)}
                  disabled={busy || workloadInv.loadingMore}
                  variant="outline"
                  size="sm"
                  className="px-2 py-1 border"
                >
                  加载下一页 Workload
                </Button>
              )}
              {workloadInv.hasMore && (
                <p className="text-xs text-[#66707a] mt-1">
                  尚有其他页面。游标翻页并非单个事务快照，列表不能证明已包含所有任务。
                </p>
              )}
            </div>
          )}
          <div className="management-fields grid grid-cols-2 md:grid-cols-4 gap-3">
            <div className="flex flex-col">
              <Label htmlFor="cluster-id">Cluster ID</Label>
              <Input
                id="cluster-id"
                value={cluster}
                onChange={(e) => setCluster(e.target.value)}
                placeholder="cluster-a"
              />
            </div>
            <div className="flex flex-col">
              <Label htmlFor="project-id">Project ID</Label>
              <Input
                id="project-id"
                value={project}
                onChange={(e) => setProject(e.target.value)}
                placeholder="research"
              />
            </div>
            <div className="flex flex-col">
              <Label htmlFor="kind-select">类型</Label>
              <select
                id="kind-select"
                value={kind}
                onChange={(e) => setKind(e.target.value as Kind)}
                className="h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                <option value="ComputePool">ComputePool</option>
                <option value="Workload">Workload</option>
              </select>
            </div>
            <div className="flex flex-col">
              <Label htmlFor="resource-id">资源 ID</Label>
              <Input
                id="resource-id"
                value={resource}
                onChange={(e) => setResource(e.target.value)}
                placeholder="pool-a"
              />
            </div>
          </div>
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleRead}
              disabled={!authenticated || busy}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              从 PostgreSQL 读取
            </Button>
          </div>
          {desired && (
            <div className="management-result mt-3">
              <div className="text-sm text-[#20242a]">
                Generation: <strong>{desired.generation}</strong> · 删除请求:{' '}
                {desired.deleting ? '已发起' : '未发起'}
              </div>
              <pre className="text-xs mt-1 bg-[#f0f1f3] rounded p-3 overflow-auto max-h-[370px]">
                {JSON.stringify(desired, null, 2)}
              </pre>
            </div>
          )}
          <p className="text-xs text-[#66707a]">
            读取的是已授权项目的单个 Desired 资源，不包含独立 Observed / Ready 证明。
          </p>
        </CardContent>
      </Card>

      {/* 区域 3: Workload 向导 */}
      <Card style={cardStyle}>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">
            3. 训练 / 批量推理 Workload 向导（OIDC 项目授权）
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-[#66707a]">
            先使用当前已登录身份读取项目内的 ComputePool，再按其真实加速器绑定生成任务请求。
            本向导不会自动提交，也不使用旧 Portal 开发双令牌接口。真实准入仍由 Kueue 决定。
          </p>
          <div className="management-actions flex gap-2">
            <Button
              onClick={() => handleListComputePools()}
              disabled={!authenticated || busy || !cluster || !project}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询项目 ComputePool 列表（每页 25 条）
            </Button>
          </div>
          {poolInv.items.length > 0 && (
            <div
              role="region"
              aria-label="授权 ComputePool 列表"
              className="management-status text-sm text-[#34486b] bg-[#edf2fc] rounded p-3"
            >
              <p>
                当前项目 {project} · {poolInv.items.length} 条已读取（仅 Desired 元数据）。
              </p>
              <ul className="mt-2 text-sm text-[#20242a]">
                {poolInv.items.map((item) => (
                  <li key={item.id} className="flex items-center gap-2">
                    <span>
                      {item.id} · Generation {item.generation} ·{' '}
                      {item.deleting ? '删除请求已发起' : '未请求删除'}
                    </span>
                    <Button
                      onClick={() => handleSelectPoolRow(item)}
                      disabled={busy}
                      variant="outline"
                      size="sm"
                      className="px-2 py-1 border"
                    >
                      选择 ComputePool {item.id}
                    </Button>
                  </li>
                ))}
              </ul>
              {poolInv.nextCursor && (
                <Button
                  onClick={() => handleListComputePools(poolInv.nextCursor ?? undefined)}
                  disabled={busy || poolInv.loadingMore}
                  variant="outline"
                  size="sm"
                  className="px-2 py-1 border"
                >
                  加载下一页 ComputePool
                </Button>
              )}
            </div>
          )}
          <div className="flex flex-col">
            <Label htmlFor="workload-pool-id">已授权 ComputePool ID</Label>
            <Input
              id="workload-pool-id"
              value={workloadPoolId}
              onChange={(e) => {
                setWorkloadPoolId(e.target.value)
                setVerifiedSelection(null)
              }}
              placeholder="用于训练的 ComputePool ID"
            />
          </div>
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleVerifyPool}
              disabled={!authenticated || busy || !workloadPoolId}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              读取并验证 ComputePool
            </Button>
          </div>
          {verifiedSelection && (
            <p className="text-sm text-[#20695f] bg-[#e6f2ef] rounded p-2">
              已确认 {project}/{verifiedSelection.pool.id} · Generation {verifiedSelection.pool.generation}
            </p>
          )}
          <div className="management-fields grid grid-cols-2 md:grid-cols-3 gap-3 mt-3">
            <div className="flex flex-col">
              <Label htmlFor="workload-id">新 Workload ID</Label>
              <Input
                id="workload-id"
                value={workloadID}
                onChange={(e) => setWorkloadID(e.target.value)}
                placeholder="新的训练任务 ID"
              />
            </div>
            {verifiedSelection && (
              <div className="flex flex-col">
                <Label htmlFor="workload-accelerator-class">加速器类型</Label>
                <select
                  id="workload-accelerator-class"
                  value={workloadAcceleratorClass}
                  onChange={(e) => setWorkloadAcceleratorClass(e.target.value)}
                  className="h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                >
                  {verifiedSelection.pool.accelerators.map((a) => (
                    <option key={a.class} value={a.class}>
                      {a.class}（配额 {a.quota}）
                    </option>
                  ))}
                </select>
              </div>
            )}
            <div className="flex flex-col">
              <Label htmlFor="workload-quota">请求 GPU 数量</Label>
              <Input
                id="workload-quota"
                type="number"
                value={workloadQuota}
                onChange={(e) => setWorkloadQuota(Number(e.target.value) || 1)}
                min="1"
              />
            </div>
            <div className="flex flex-col">
              <Label htmlFor="workload-image">镜像</Label>
              <Input
                id="workload-image"
                value={workloadImage}
                onChange={(e) => setWorkloadImage(e.target.value)}
                placeholder="registry.example/train@sha256:..."
              />
            </div>
          </div>
          <div className="flex flex-col">
            <Label htmlFor="workload-command">Command（JSON argv 数组；不做 Shell 插值）</Label>
            <textarea
              id="workload-command"
              value={workloadCommandJSON}
              onChange={(e) => setWorkloadCommandJSON(e.target.value)}
              rows={3}
              spellCheck={false}
              className="w-full border border-[#e8eaee] rounded-md p-2 text-sm font-mono"
            />
          </div>
          {verifiedSelection && (
            <Button
              onClick={handleGenerateWorkload}
              disabled={busy}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              生成受约束的 Workload PUT 请求
            </Button>
          )}
          <p className="text-xs text-[#66707a]">
            仅使用服务器读取的 Pool 信息生成草案；服务端提交时仍重新执行 OIDC、项目 RBAC、
            Pool 绑定和 Generation CAS。不会承诺 PVC 持久化、Pod Ready 或训练成功。
          </p>
        </CardContent>
      </Card>

      {/* 区域 4: 创建或修改 Desired 资源 */}
      <Card style={cardStyle}>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">4. 创建或修改 Desired 资源</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Label htmlFor="request-json">JSON 请求体（必须包含 expectedGeneration 和 spec；Workload 还需 poolId）</Label>
          <textarea
            id="request-json"
            value={requestJSON}
            onChange={(e) => setRequestJSON(e.target.value)}
            rows={12}
            spellCheck={false}
            className="w-full border border-[#e8eaee] rounded-md p-2 text-sm font-mono"
          />
          <p className="text-xs text-[#66707a]">
            新建时 expectedGeneration=0；更新已有 ComputePool 时使用读取到的 Generation。已有 Workload
            不允许原地更新。
          </p>
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleWrite}
              disabled={!authenticated || busy}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              确认并提交 PUT
            </Button>
            <Button
              onClick={handleRemove}
              disabled={!authenticated || busy || !desired || desired.deleting}
              variant="destructive"
              className="bg-[#b84f56] text-white px-4 py-2 rounded-md"
            >
              确认并发起删除请求
            </Button>
          </div>
          <p className="text-xs text-[#66707a]">
            删除操作使用当前选中资源的 Generation 和 If-Match；服务端会检查项目角色、资源依赖与
            Finalizer，HTTP 202 不等于实际删除完成。
          </p>
        </CardContent>
      </Card>

      {/* 区域 5: 操作回执与恢复 */}
      <Card style={cardStyle}>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">5. 操作回执与恢复</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-[#66707a]">
            如果本标签的操作 ID 丢失，可按当前 OIDC 登录和项目授权按页查询本人历史操作。每页最多
            25 条，按审计事务时间排序（不是严格提交顺序），分页不是事务快照。索引仅帮助找回原始
            ID，不证明 GPU 执行、删除完成或允许盲目重试。
          </p>
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleListRecentOps}
              disabled={!authenticated || busy || !cluster || !project}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询本项目最近操作（只读）
            </Button>
          </div>
          {recentOps.length > 0 && (
            <ul className="text-sm text-[#20242a] bg-[#f0f1f3] rounded p-3 mt-2">
              {recentOps.map((op) => (
                <li key={op.requestId} className="flex items-center gap-2">
                  <span className="font-mono text-xs">{op.requestId}</span>
                  <span>
                    {op.kind}/{op.resourceId} · {op.action} · {op.recordedAt}
                  </span>
                  <Button
                    onClick={() => setOperationId(op.requestId)}
                    variant="outline"
                    size="sm"
                    className="px-2 py-1 border"
                  >
                    使用该 ID
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <Label htmlFor="operation-id" className="text-xs font-semibold text-[#47566b] block mt-3">
            操作 ID（128 位随机十六进制；断连时必须使用原 ID）
          </Label>
          <Input
            id="operation-id"
            value={operationId}
            onChange={(e) => setOperationId(e.target.value)}
            placeholder="原始 Idempotency-Key"
          />
          <div className="management-actions flex gap-2 mt-2">
            <Button
              onClick={handleReceipt}
              disabled={!authenticated || busy}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询已提交回执
            </Button>
            <Button
              onClick={handleCopyOperationId}
              disabled={!operationId}
              variant="outline"
              className="px-4 py-2 rounded-md border"
            >
              复制操作 ID
            </Button>
            {authenticated && operationPointer && (
              <Button
                onClick={handleRestoreOperation}
                disabled={busy}
                variant="outline"
                className="px-4 py-2 rounded-md border"
              >
                恢复操作范围与 ID
              </Button>
            )}
            <Button
              onClick={handleDiscardOperation}
              disabled={!operationPointer}
              variant="outline"
              className="px-4 py-2 rounded-md border"
            >
              清除此操作记录
            </Button>
          </div>
          {activeReceipt && (
            <div className="management-status text-sm text-[#34486b] bg-[#edf2fc] rounded p-3 mt-3">
              <pre className="text-xs mt-1 overflow-auto">{JSON.stringify(activeReceipt, null, 2)}</pre>
            </div>
          )}
          <div className="management-actions flex gap-2">
            <Button
              onClick={handleReadBackCheck}
              disabled={!authenticated || busy || !activeReceipt}
              variant="outline"
              className="px-4 py-2 rounded-md border"
            >
              按回执核对当前 Desired
            </Button>
            <Button
              onClick={handleEvidenceRefresh}
              disabled={!authenticated || busy}
              variant="outline"
              className="px-4 py-2 rounded-md border"
            >
              一键重新核对回执、Desired 与执行观察
            </Button>
          </div>
          {activeEvidence && (
            <div className="management-evidence mt-3 space-y-2">
              <p className="text-sm font-semibold text-[#47566b]">上次完整证据核对：</p>
              <p className="text-xs text-[#66707a]" data-testid="evidence-check-time">
                本页手动核对完成时间（浏览器时钟）：{evidenceCheckedAt ?? '未记录'}。这是只读查询结束时间，并非 Agent 心跳或实时 Job 状态。
              </p>
              <div
                role="note"
                className="text-sm text-[#34486b] bg-[#edf2fc] rounded p-3"
              >
                <strong>{activeEvidence.readback.status}</strong>
                <p>{activeEvidence.readback.detail}</p>
                {activeEvidence.readback.desired && (
                  <p>
                    当前 Generation: {activeEvidence.readback.desired.generation} · 删除请求:{' '}
                    {activeEvidence.readback.desired.deleting ? '已发起' : '未发起'}
                  </p>
                )}
              </div>
              {activeEvidence.execution && (
                <div
                  role="group"
                  aria-label="独立执行状态"
                  className="text-sm text-[#34486b] bg-[#edf2fc] rounded p-3"
                >
                  <strong>
                    {activeEvidence.execution.label}（{activeEvidence.execution.code}）
                  </strong>
                  <p>{activeEvidence.execution.detail}</p>
                  <p className="text-xs mt-1" data-testid="observation-provenance">
                    Agent 最后上报时间：{activeEvidence.execution.observedAt ?? '未上报'}；
                    {activeEvidence.execution.newerThanReceipt
                      ? '观察对应较新 Generation，不能归因于当前操作回执。'
                      : '本次只读查询读取历史观察，不能证明此刻仍在运行或已完成清理。'}
                  </p>
                </div>
              )}
            </div>
          )}
          <p className="text-xs text-[#66707a]">
            回执仅证明 Desired 写入和操作审计已提交，非 Workload 执行证明。
          </p>
        </CardContent>
      </Card>

      {feedback && feedback.kind === 'status' && (
        <div
          role="status"
          className="management-feedback mt-4 rounded bg-[#e6f2ef] text-[#20695f] p-3"
        >
          {feedback.text}
        </div>
      )}
      {feedback && feedback.kind === 'alert' && (
        <div
          role="alert"
          className="management-error mt-4 rounded bg-[#fcecef] text-[#b84f56] p-3"
        >
          {feedback.text}
        </div>
      )}
    </div>
  )
}
