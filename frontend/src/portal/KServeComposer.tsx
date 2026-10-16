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
import { buildKServeDraft, servingProjects } from './kserve-draft'
import { kserveHTTPAcceptanceCommand } from './kserve-http-command'

const FIELD = 'h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm'
const MONO = 'w-full border border-[#e8eaee] rounded-md p-2 text-sm font-mono'

// Vue KServeComposer 的 React 等价实现：只生成原生 InferenceService JSON
// 与本地手动命令，不在浏览器中部署或调用模型服务。
export function KServeComposer({ rows }: { rows: ResourceRow[] }) {
  const projects = useMemo(() => servingProjects(rows), [rows])
  const [projectId, setProjectId] = useState('')
  const [namespace, setNamespace] = useState('')
  const [serviceId, setServiceId] = useState('')
  const [gpu, setGpu] = useState(1)
  const [image, setImage] = useState('')
  const [argsJson, setArgsJson] = useState('["--model","/models/model"]')
  const [context, setContext] = useState('')
  const [httpUrl, setHttpUrl] = useState('')
  const [httpHost, setHttpHost] = useState('')
  const [copyStatus, setCopyStatus] = useState('')

  // 镜像 Vue 的 immediate watch：预选快照内的首个项目。
  useEffect(() => {
    if (!projects.some((p) => p.id === projectId)) setProjectId(projects[0]?.id ?? '')
  }, [projects, projectId])

  const draft = useMemo(
    () =>
      buildKServeDraft(
        {
          id: serviceId,
          projectId,
          namespace,
          image,
          argsJson,
          wholeGpuCount: gpu,
        },
        projects
      ),
    [serviceId, projectId, namespace, image, argsJson, gpu, projects]
  )

  const manifestJson = draft.manifest ? JSON.stringify(draft.manifest, null, 2) : ''
  const validContext = /^[a-zA-Z0-9][a-zA-Z0-9_.:@/-]{0,127}$/.test(context)
  const httpAcceptanceCommand = kserveHTTPAcceptanceCommand({
    manifestValid: draft.manifest !== null,
    context,
    url: httpUrl,
    host: httpHost,
  })
  const commands =
    !draft.manifest || !validContext
      ? '填写 KServe 清单后，输入明确的本地 Kubernetes context 才显示命令。'
      : 'python3 scripts/dev/kserve_service.py serving.json\n' +
        'python3 scripts/dev/kserve_service.py serving.json --context ' +
        context +
        ' --create\n' +
        'python3 scripts/dev/kserve_service.py serving.json --context ' +
        context +
        ' --status'

  const handleCopyManifest = async () => {
    if (!draft.manifest) return
    try {
      await navigator.clipboard.writeText(manifestJson)
      setCopyStatus('清单已复制；尚未创建任何集群资源。')
    } catch {
      setCopyStatus('浏览器剪贴板权限不足，可手动复制 JSON。')
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>在线推理服务 · KServe</CardTitle>
            <p className="text-xs text-muted-foreground mt-1">
              生成原生 KServe InferenceService v1beta1 清单；管理员在已有 KServe
              集群中显式创建。
            </p>
          </div>
          <Badge variant="warning">Stage C · Developer preview</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <Alert
          variant="warning"
          title="当前只有外部 KServe 创建/读取工具，尚未接入生产级 Serving Provider、API 路由或模型管理。此页面不直接部署，也不代表 llm-d、Gateway 和模型 HTTP 已就绪。"
          closable={false}
          showIcon
        />
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="kserve-project">项目（来自授权快照）</Label>
            <select
              id="kserve-project"
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              className={FIELD}
            >
              {projects.length === 0 && <option value="">选择项目</option>}
              {projects.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.id}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="kserve-namespace">目标 Kubernetes Namespace（管理员确认）</Label>
            <Input
              id="kserve-namespace"
              value={namespace}
              onChange={(e) => setNamespace(e.target.value)}
              placeholder="research"
              maxLength={63}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="kserve-id">Serving ID</Label>
            <Input
              id="kserve-id"
              value={serviceId}
              onChange={(e) => setServiceId(e.target.value)}
              placeholder="qwen-serving"
              maxLength={253}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="kserve-gpu">完整 NVIDIA GPU 张数</Label>
            <Input
              id="kserve-gpu"
              type="number"
              min={1}
              max={8}
              value={gpu}
              onChange={(e) => {
                const parsed = Number(e.target.value)
                setGpu(Math.min(8, Math.max(1, Math.floor(parsed) || 1)))
              }}
            />
          </div>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="kserve-image">推理镜像（必须 sha256 digest）</Label>
          <Input
            id="kserve-image"
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="registry.example/llm@sha256:..."
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="kserve-args">
            推理容器参数（JSON argv，镜像自行提供入口和模型文件）
          </Label>
          <textarea
            id="kserve-args"
            value={argsJson}
            onChange={(e) => setArgsJson(e.target.value)}
            rows={3}
            spellCheck={false}
            className={MONO}
          />
        </div>
        <Alert
          title="固定使用 KServe 自定义 Predictor、端口 8000、单副本、CPU 1、内存 16Gi；不自动安装模型文件、配置 PVC、创建路由或分配 GPU。"
          closable={false}
        />
        {draft.errors.length > 0 && (
          <Alert variant="destructive" title={draft.errors.join('；')} closable={false} />
        )}
        <div>
          <h3 className="text-sm font-semibold">原生 InferenceService 清单（仅生成 JSON）</h3>
          <textarea
            readOnly
            rows={14}
            value={manifestJson}
            placeholder="填写有效配置以生成 KServe 清单"
            spellCheck={false}
            className={MONO + ' mt-2'}
          />
          <div className="flex items-center gap-3 mt-3">
            <Button variant="outline" disabled={!draft.manifest} onClick={handleCopyManifest}>
              复制 KServe JSON
            </Button>
            <span className="text-xs text-muted-foreground">{copyStatus}</span>
          </div>
          <h3 className="text-sm font-semibold mt-4">本地开发命令（手动执行，不在浏览器调用）</h3>
          <div className="flex flex-col gap-1 mt-2">
            <Label htmlFor="kserve-context">本地 kubeconfig context</Label>
            <Input
              id="kserve-context"
              value={context}
              onChange={(e) => setContext(e.target.value)}
              placeholder="kind-gpu"
            />
          </div>
          <textarea
            readOnly
            rows={4}
            value={commands}
            spellCheck={false}
            className={MONO + ' mt-2'}
          />
          <p className="text-xs text-muted-foreground mt-2">
            先将 JSON 保存为 serving.json。KServe Controller Ready 并不等于真实模型响应，必须单独验证数据面。
          </p>
          <h3 className="text-sm font-semibold mt-4">真实 HTTP 推理验收 · 仅生成本地命令</h3>
          <Alert
            variant="warning"
            title="必须先由管理员确认 KServe Service / Ingress 对应关系，并将其转发到本机回环地址；这里不自动端口转发、执行推理或发送凭证。HTTP 200 及响应字段匹配仍不能证明模型质量或生产路由身份。"
            closable={false}
            className="mt-3"
          />
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="kserve-http-url">本地已授权 Port-forward URL</Label>
              <Input
                id="kserve-http-url"
                value={httpUrl}
                onChange={(e) => setHttpUrl(e.target.value)}
                placeholder="http://127.0.0.1:18080/v1/chat/completions"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="kserve-http-host">HTTP Host（可选，Ingress 名称路由）</Label>
              <Input
                id="kserve-http-host"
                value={httpHost}
                onChange={(e) => setHttpHost(e.target.value)}
                placeholder="model-a.research.example.local"
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            先根据实际推理镜像接口准备 <code>request.json</code> 和 <code>expect.json</code>。
            例如，OpenAI 兼容接口可断言 <code>/choices/0/message/content</code> 为非空字符串；
            并非所有自定义 Predictor 都支持这个协议。
          </p>
          <textarea
            readOnly
            rows={3}
            value={
              httpAcceptanceCommand ||
              '填写合法的本机 URL、Kubernetes context 和 KServe 清单后才能生成验收命令。'
            }
            spellCheck={false}
            className={MONO + ' mt-2'}
          />
          <p className="text-xs text-muted-foreground mt-2">
            本地验收工具默认离线，可用 --execute 进行 KServe GET → 真实 HTTP POST → KServe
            GET，返回 SHA256 证据；原始请求和响应不会打印到 Portal。详情见
            docs/KSERVE_DEV_SERVING.md。
          </p>
        </div>
      </CardContent>
    </Card>
  )
}
