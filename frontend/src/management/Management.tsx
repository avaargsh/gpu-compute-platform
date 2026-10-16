'use client'

import { useCallback, useMemo, useState } from 'react'
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Input,
  Label,
  Badge,
  Alert,
  AlertTitle,
  AlertDescription,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import { useAuth } from '@/management/hooks/useAuth'
import { useProjects } from '@/management/hooks/useProjects'
import { useInventory } from '@/management/hooks/useInventory'
import { useResource } from '@/management/hooks/useResource'
import { useReceipt } from '@/management/hooks/useReceipt'
import { useEvidence } from '@/management/hooks/useEvidence'
import { useOperation } from '@/management/hooks/useOperation'
import {
  readResource,
  writeResource,
  deleteResource,
  type Scope,
  type Kind,
  type ProjectAccess,
} from '@/management/api'

const cardStyle: React.CSSProperties = {
  border: '1px solid',
  borderRadius: '12px',
  padding: '24px',
  marginBottom: '24px',
}

export function ManagementApp() {
  const {
    issuer,
    setIssuer,
    clientId,
    setClientId,
    configured,
    session,
    authenticated,
    busy,
    message,
    error,
    signIn,
    signOut,
    handleOAuthCallback,
    token,
  } = useAuth()

  const { projects, truncated: projectListTruncated, loadProjects } = useProjects()

  const {
    items: inventoryItems,
    nextCursor,
    hasMore,
    loading: inventoryLoading,
    loadingMore,
    listWorkloads,
    listComputePools,
  } = useInventory()

  const {
    desired,
    loading: resourceLoading,
    clearDesired,
    readResource: readResourceFn,
    writeResource: writeResourceFn,
    deleteResource: deleteResourceFn,
  } = useResource()

  const { receipt, loading: receiptLoading, readReceipt: readReceiptFn } = useReceipt()

  const { check: evidenceCheck, loading: evidenceLoading, refreshEvidenceChain } =
    useEvidence()

  const { pointer: operationPointer, remember, recover, forget } = useOperation()

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
  const [activeReceipt, setActiveReceipt] = useState<any>(null)
  const [activeReadback, setActiveReadback] = useState<any>(null)
  const [activeExecution, setActiveExecution] = useState<any>(null)

  const scope: Scope = { cluster, project, kind, resource }

  const handleSignIn = useCallback(() => {
    signIn()
  }, [signIn])

  const handleSignOut = useCallback(() => {
    signOut()
  }, [signOut])

  const handleLoadProjects = useCallback(async () => {
    try {
      await loadProjects(token())
    } catch {
      // error handled in hook
    }
  }, [loadProjects, token])

  const handleSelectProject = useCallback((item: ProjectAccess) => {
    setCluster(item.clusterId)
    setProject(item.projectId)
    setResource('')
    setKind('ComputePool')
    clearDesired()
    setOperationId('')
  }, [clearDesired])

  const handleListWorkloads = useCallback(async (after?: string) => {
    try {
      await listWorkloads(cluster, project, token(), after)
    } catch {
      // error handled in hook
    }
  }, [cluster, project, token, listWorkloads])

  const handleListComputePools = useCallback(async (after?: string) => {
    try {
      await listComputePools(cluster, project, token(), after)
    } catch {
      // error handled in hook
    }
  }, [cluster, project, token, listComputePools])

  const handleRead = useCallback(async () => {
    try {
      await readResourceFn(scope, token())
    } catch {
      // error handled in hook
    }
  }, [scope, token, readResourceFn])

  const handleWrite = useCallback(async () => {
    try {
      JSON.parse(requestJSON)
      const opId = operationId || createOperationId()
      setOperationId(opId)
      await writeResourceFn(scope, token(), requestJSON, opId)
    } catch {
      // error handled in hook
    }
  }, [scope, token, writeResourceFn, requestJSON, operationId])

  const handleRemove = useCallback(async () => {
    if (!desired) return
    try {
      await deleteResourceFn(scope, token(), desired.generation, createOperationId())
    } catch {
      // error handled in hook
    }
  }, [scope, token, deleteResourceFn, desired])

  const handleReceipt = useCallback(async () => {
    try {
      await token()
      await readReceiptFn(cluster, project, operationId, token())
      setActiveReceipt(receipt)
    } catch {
      // error handled in hook
    }
  }, [cluster, project, operationId, token, readReceiptFn, receipt])

  const handleCopyOperationId = useCallback(async () => {
    await navigator.clipboard.writeText(operationId)
  }, [operationId])

  const handleReadBackDesired = useCallback(async () => {
    if (!activeReceipt) return
    try {
      await token()
      setActiveReadback({ status: 'desired-visible', detail: '回读完成', desired })
    } catch {
      // error handled
    }
  }, [activeReceipt, token, desired])

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
    setActiveReceipt(null)
    setActiveReadback(null)
    setActiveExecution(null)
  }, [forget])

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
          {inventoryItems.length > 0 && (
            <div className="management-status text-sm text-[#34486b] bg-[#edf2fc] rounded p-3">
              <p>
                当前项目 {project} · {inventoryItems.length} 条已读取（仅 Desired 元数据）。
                不包含 Kueue 准入、Agent 状态或执行成功证明。
              </p>
              {inventoryItems.length === 0 && (
                <p>此页未找到任务；可能为空、已删除或项目授权发生变化。</p>
              )}
              <ul className="mt-2 text-sm text-[#20242a]">
                {inventoryItems.map((item) => (
                  <li key={item.id} className="flex items-center gap-2">
                    {item.id} · Generation {item.generation} ·{' '}
                    {item.deleting ? '删除请求已发起' : '未请求删除'}
                  </li>
                ))}
              </ul>
              {nextCursor && (
                <Button
                  onClick={() => handleListWorkloads(nextCursor)}
                  disabled={busy || loadingMore}
                  variant="outline"
                  size="sm"
                  className="px-2 py-1 border"
                >
                  加载下一页 Workload
                </Button>
              )}
              {hasMore && (
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
              <Select
                id="kind-select"
                value={kind}
                onValueChange={(v) => setKind(v as Kind)}
                className="w-full"
              >
                <SelectTrigger>
                  <SelectValue>{kind}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ComputePool">ComputePool</SelectItem>
                  <SelectItem value="Workload">Workload</SelectItem>
                </SelectContent>
              </Select>
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
          <div className="flex flex-col">
            <Label htmlFor="workload-pool-id">已授权 ComputePool ID</Label>
            <Input
              id="workload-pool-id"
              value={workloadPoolId}
              onChange={(e) => setWorkloadPoolId(e.target.value)}
              placeholder="用于训练的 ComputePool ID"
            />
          </div>
          <div className="management-actions flex gap-2">
            <Button
              onClick={() => handleListComputePools()}
              disabled={!authenticated || busy || !cluster || !project}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询项目 ComputePool 列表（每页 25 条）
            </Button>
          </div>
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
            <div className="flex flex-col">
              <Label htmlFor="workload-accelerator-class">加速器类型</Label>
              <Select
                id="workload-accelerator-class"
                value={workloadAcceleratorClass}
                onValueChange={(v) => setWorkloadAcceleratorClass(v as string)}
                className="w-full"
              >
                <SelectTrigger>
                  <SelectValue>{workloadAcceleratorClass || '未选择'}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">未选择</SelectItem>
                </SelectContent>
              </Select>
            </div>
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
          <Button
            onClick={() => {}}
            disabled={busy}
            className="bg-[#357d73] text-white px-4 py-2 rounded-md"
          >
            生成受约束的 Workload PUT 请求
          </Button>
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
              onClick={() => {}}
              disabled={!authenticated || busy || !cluster || !project}
              className="bg-[#357d73] text-white px-4 py-2 rounded-md"
            >
              查询本项目最近操作（只读）
            </Button>
          </div>
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
            <Button
              onClick={handleRestoreOperation}
              disabled={busy}
              variant="outline"
              className="px-4 py-2 rounded-md border"
            >
              恢复操作范围与 ID
            </Button>
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
          {activeReadback && (
            <div className="management-status text-sm text-[#34486b] bg-[#edf2fc] rounded p-3 mt-3">
              <strong>独立授权回读：{activeReadback.status}</strong>
              <p>{activeReadback.detail}</p>
              {activeReadback.desired && (
                <p>
                  当前 Generation: {activeReadback.desired.generation} · 删除请求:{' '}
                  {activeReadback.desired.deleting ? '已发起' : '未发起'}
                </p>
              )}
            </div>
          )}
          {activeExecution && (
            <div className="management-status text-sm text-[#34486b] bg-[#edf2fc] rounded p-3 mt-3">
              <strong>独立执行状态：{activeExecution.label}（{activeExecution.code}）</strong>
              <p>{activeExecution.detail}</p>
            </div>
          )}
          <p className="text-xs text-[#66707a]">
            回执仅证明 Desired 写入和操作审计已提交，非 Workload 执行证明。
          </p>
        </CardContent>
      </Card>

      {message && (
        <div className="management-feedback mt-4 rounded bg-[#e6f2ef] text-[#20695f] p-3">
          {message}
        </div>
      )}
      {error && (
        <div className="management-error mt-4 rounded bg-[#fcecef] text-[#b84f56] p-3">
          {error}
        </div>
      )}
    </div>
  )
}

function createOperationId(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
