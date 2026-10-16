'use client'

import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Input,
  Label,
} from '@/components/ui'
import type { ResourceRow } from './api'
import { buildWorkloadDraft, draftPools, type WorkloadMode } from './workload-draft'
import { submitDevWorkload, WorkloadSubmissionUncertainError } from './dev-submit'

interface JobComposerProps {
  rows: ResourceRow[]
  clusterId: string
  readToken: string
  onSubmitted: () => void
}

type Submission = { fingerprint: string; kind: 'accepted' | 'unknown'; detail: string }

const FIELD = 'h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm'
const MONO = 'w-full border border-[#e8eaee] rounded-md p-2 text-sm font-mono'

// Vue JobComposer 的 React 等价实现：从真实授权快照的 ComputePool 生成受约束
// Workload 草案，写入凭证只存在页面内存，同一草案绝不自动重放。
export function JobComposer({ rows, clusterId, readToken, onSubmitted }: JobComposerProps) {
  const pools = useMemo(() => draftPools(rows), [rows])
  const [mode, setMode] = useState<WorkloadMode>('training')
  const [poolId, setPoolId] = useState('')
  const [acceleratorClass, setAcceleratorClass] = useState('')
  const [quota, setQuota] = useState(1)
  const [workloadId, setWorkloadId] = useState('')
  const [image, setImage] = useState('')
  const [workspaceClaim, setWorkspaceClaim] = useState('')
  const [commandJson, setCommandJson] = useState('["python","train.py"]')
  const [writeToken, setWriteToken] = useState('')
  const [copyStatus, setCopyStatus] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [submissionState, setSubmissionState] = useState<Submission | null>(null)

  const selectedPool = useMemo(() => pools.find((p) => p.id === poolId), [pools, poolId])

  // 镜像 Vue 的 immediate watch：预选唯一可用池与首个加速器类。
  useEffect(() => {
    if (!pools.some((p) => p.id === poolId)) setPoolId(pools[0]?.id ?? '')
  }, [pools, poolId])
  useEffect(() => {
    if (!selectedPool?.accelerators.some((a) => a.class === acceleratorClass)) {
      setAcceleratorClass(selectedPool?.accelerators[0]?.class ?? '')
    }
  }, [selectedPool, acceleratorClass])
  // 新会话（读取凭证或集群变化）作废内存中的提交结论与写入凭证。
  useEffect(() => {
    setWriteToken('')
    setSubmitError('')
    setSubmissionState(null)
  }, [readToken, clusterId])

  const draft = useMemo(
    () =>
      buildWorkloadDraft(
        {
          id: workloadId,
          poolId,
          acceleratorClass,
          quota,
          image,
          commandJson,
          mode,
          workspaceClaim,
        },
        pools
      ),
    [workloadId, poolId, acceleratorClass, quota, image, commandJson, mode, workspaceClaim, pools]
  )

  const payloadText = draft.payload ? JSON.stringify(draft.payload, null, 2) : ''
  const fingerprint = JSON.stringify({ clusterId, payload: draft.payload })
  const currentSubmission = submissionState?.fingerprint === fingerprint ? submissionState : null
  const canSubmit =
    !submitting &&
    !currentSubmission &&
    !!draft.payload &&
    readToken.length >= 32 &&
    writeToken.length >= 32

  const handleModeChange = (next: WorkloadMode) => {
    setMode(next)
    setCommandJson(next === 'training' ? '["python","train.py"]' : '["python","batch_infer.py"]')
  }

  const handleSubmit = async () => {
    const payload = draft.payload
    if (!payload || submitting || currentSubmission) return
    // 反馈绑定到发出的确切意图：请求在途时表单字段可以变化，
    // 结果未知时不得自动重试。
    const sentFingerprint = fingerprint
    setSubmitting(true)
    setSubmitError('')
    try {
      await submitDevWorkload(clusterId, readToken, writeToken, payload)
      setSubmissionState({
        fingerprint: sentFingerprint,
        kind: 'accepted',
        detail:
          'Desired 已接收（HTTP 204）：' +
          payload.projectId + '/' + payload.metadata.id +
          ' · Generation ' + payload.metadata.generation +
          '。请刷新并检查 Agent Observation；不代表 Kueue 准入、Pod 运行或训练成功。',
      })
    } catch (cause) {
      if (cause instanceof WorkloadSubmissionUncertainError) {
        setSubmissionState({ fingerprint: sentFingerprint, kind: 'unknown', detail: cause.message })
      } else {
        setSubmitError(cause instanceof Error ? cause.message : '提交被拒绝')
      }
    } finally {
      setSubmitting(false)
    }
  }

  const handleCopyDraft = async () => {
    if (!draft.payload) return
    try {
      await navigator.clipboard.writeText(payloadText)
      setCopyStatus('已复制。尚未创建任何服务器资源。')
    } catch {
      setCopyStatus('浏览器禁止剪贴板访问，可从上方只读文本框手动复制。')
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>训练 / 批量推理任务</CardTitle>
            <p className="text-xs text-muted-foreground mt-1">
              从已授权项目的 ComputePool 生成 v0.1 Workload 意图；当前为可复制的部署草案。
            </p>
          </div>
          <Badge variant="warning">Draft · 不自动提交</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <Alert
          title="训练和批量推理走 Kubernetes Job + Kueue；在线推理 API 属于后续 Serving 控制面，不由本任务向导承诺。"
          closable={false}
          showIcon
        />
        <div className="flex flex-col gap-1">
          <Label>任务类型</Label>
          <div className="flex items-center gap-4 text-sm">
            <label className="flex items-center gap-1.5">
              <input
                type="radio"
                name="job-composer-mode"
                checked={mode === 'training'}
                onChange={() => handleModeChange('training')}
              />
              训练任务
            </label>
            <label className="flex items-center gap-1.5">
              <input
                type="radio"
                name="job-composer-mode"
                checked={mode === 'batch-inference'}
                onChange={() => handleModeChange('batch-inference')}
              />
              批量推理
            </label>
          </div>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="job-composer-pool">已授权项目 / ComputePool</Label>
          {selectedPool && (
            <span className="job-composer-pool-selection text-sm font-medium text-[#20695f]">
              {selectedPool.projectId} / {selectedPool.id}
            </span>
          )}
          <select
            id="job-composer-pool"
            value={poolId}
            onChange={(e) => setPoolId(e.target.value)}
            className={FIELD}
          >
            {pools.length === 0 && <option value="">从真实资源快照中选择</option>}
            {pools.map((pool) => (
              <option key={pool.id} value={pool.id}>
                {pool.projectId} / {pool.id}
              </option>
            ))}
          </select>
        </div>
        {pools.length === 0 && (
          <Alert
            variant="warning"
            title="当前读取的页面没有有效 ComputePool；如资源被分页截断，请先加载完整资源列表。"
            closable={false}
          />
        )}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="job-composer-id">Workload ID</Label>
            <Input
              id="job-composer-id"
              value={workloadId}
              onChange={(e) => setWorkloadId(e.target.value)}
              placeholder="train-qwen-001"
              maxLength={128}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="job-composer-accelerator">加速器类</Label>
            <select
              id="job-composer-accelerator"
              value={acceleratorClass}
              onChange={(e) => setAcceleratorClass(e.target.value)}
              className={FIELD}
            >
              {(selectedPool?.accelerators ?? []).map((acc) => (
                <option key={acc.class} value={acc.class}>
                  {acc.class} · 声明配额 {acc.quota}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="job-composer-quota">请求 GPU 数量</Label>
            <Input
              id="job-composer-quota"
              type="number"
              min={1}
              max={256}
              value={quota}
              onChange={(e) => {
                const parsed = Number(e.target.value)
                setQuota(Math.min(256, Math.max(1, Math.floor(parsed) || 1)))
              }}
            />
          </div>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="job-composer-image">容器镜像</Label>
          <Input
            id="job-composer-image"
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="registry.example/team/train@sha256:..."
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="job-composer-workspace">
            持久工作区 PVC（可选，必须已在项目 Namespace 中创建）
          </Label>
          <Input
            id="job-composer-workspace"
            value={workspaceClaim}
            onChange={(e) => setWorkspaceClaim(e.target.value)}
            placeholder="training-data-pvc"
            maxLength={253}
          />
          <small className="text-xs text-muted-foreground">
            容器内固定挂载到 /workspace；删除 Job 不删除 PVC，所填名称不代表已具备访问权限。
          </small>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="job-composer-command">
            Command（JSON argv 数组，不执行 Shell 模板插值）
          </Label>
          <textarea
            id="job-composer-command"
            value={commandJson}
            onChange={(e) => setCommandJson(e.target.value)}
            rows={3}
            spellCheck={false}
            className={MONO}
          />
        </div>
        <Alert
          variant="warning"
          title="ComputePool 配额只是声明值，不代表 GPU 实时可用、Kueue 已准入、模型已就绪或训练结果已持久化。"
          closable={false}
        />
        {draft.errors.length > 0 && (
          <Alert variant="destructive" title={draft.errors.join('；')} closable={false} />
        )}
        <div>
          <h3 className="text-sm font-semibold">Workload 意图（仅本地生成）</h3>
          <textarea
            readOnly
            rows={14}
            value={payloadText}
            placeholder="填写有效的项目、镜像和任务参数以生成部署草案"
            spellCheck={false}
            className={MONO + ' mt-2'}
          />
          <div className="flex items-center gap-3 mt-3">
            <Button variant="outline" disabled={!draft.payload} onClick={handleCopyDraft}>
              复制 JSON 草案
            </Button>
            <span className="text-xs text-muted-foreground">{copyStatus}</span>
          </div>
          <h3 className="text-sm font-semibold mt-4">本机开发提交（可选）</h3>
          <Alert
            variant="warning"
            title="仅开发环境：必须在服务端设置独立 PORTAL_OPERATOR_WRITE_TOKEN，且不能与读取凭证相同。部署至 PoC/生产模式时该入口禁用。Kueue 是否准入与 Job 是否成功仍需要独立观察。"
            closable={false}
            className="mt-3"
          />
          <div className="flex flex-col gap-1 mt-3">
            <Label htmlFor="job-composer-write-token">独立写入凭证（只存在当前页面内存）</Label>
            <Input
              id="job-composer-write-token"
              type="password"
              autoComplete="off"
              showPassword
              value={writeToken}
              onChange={(e) => setWriteToken(e.target.value)}
              placeholder="PORTAL_OPERATOR_WRITE_TOKEN（至少 32 字符）"
            />
          </div>
          <Button
            className="mt-3"
            loading={submitting}
            disabled={!canSubmit}
            onClick={handleSubmit}
          >
            创建 Workload Desired（仅本机开发）
          </Button>
          {currentSubmission && (
            <div className="mt-3 space-y-2">
              <Alert
                variant={currentSubmission.kind === 'accepted' ? 'default' : 'warning'}
                title={currentSubmission.detail}
                closable={false}
              />
              <Button variant="outline" onClick={onSubmitted}>
                重新读取授权快照（核对 Desired / Observation）
              </Button>
            </div>
          )}
          {submitError && (
            <Alert variant="destructive" title={submitError} closable={false} className="mt-3" />
          )}
          {currentSubmission && (
            <p className="text-xs text-muted-foreground mt-3">
              同一草案不会自动重试。要提交新的任务，请修改 Workload ID 并检查项目/资源配额；
              结果未知时必须先刷新授权快照核对 Desired，未读到也不能证明未写入。
            </p>
          )}
          <p className="text-xs text-muted-foreground mt-2">
            此处仅创建 Desired State。实际 Pod / Kueue Admission、Job 日志和训练 Checkpoint
            须在下方执行监控中查看；操作员预览读取凭证单独使用不能创建资源。
          </p>
        </div>
      </CardContent>
    </Card>
  )
}
