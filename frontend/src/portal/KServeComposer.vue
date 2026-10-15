<template>
  <section class="panel">
    <div class="panel-heading"><div>
      <h2>在线推理服务 · KServe</h2>
      <p>生成原生 KServe InferenceService v1beta1 清单；管理员在已有 KServe 集群中显式创建。</p>
    </div><el-tag type="warning" effect="plain">Stage C · Developer preview</el-tag></div>
    <el-alert type="warning" :closable="false" show-icon
      title="当前只有外部 KServe 创建/读取工具，尚未接入生产级 Serving Provider、API 路由或模型管理。此页面不直接部署，也不代表 llm-d、Gateway 和模型 HTTP 已就绪。" />
    <el-form label-position="top" style="margin-top: 16px">
      <div class="detail-grid">
        <el-form-item label="项目（来自授权快照）">
          <el-select v-model="projectId" placeholder="选择项目" style="width: 100%">
            <el-option v-for="project in projects" :key="project.id"
              :value="project.id" :label="project.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标 Kubernetes Namespace（管理员确认）">
          <el-input v-model.trim="namespace" placeholder="research" maxlength="63" />
        </el-form-item>
        <el-form-item label="Serving ID">
          <el-input v-model.trim="serviceId" placeholder="qwen-serving" maxlength="253" />
        </el-form-item>
        <el-form-item label="完整 NVIDIA GPU 张数">
          <el-input-number v-model="gpu" :min="1" :max="8" />
        </el-form-item>
      </div>
      <el-form-item label="推理镜像（必须 sha256 digest）">
        <el-input v-model.trim="image" placeholder="registry.example/llm@sha256:..." />
      </el-form-item>
      <el-form-item label="推理容器参数（JSON argv，镜像自行提供入口和模型文件）">
        <el-input v-model="argsJson" type="textarea" :rows="3" />
      </el-form-item>
    </el-form>
    <el-alert type="info" :closable="false"
      title="固定使用 KServe 自定义 Predictor、端口 8000、单副本、CPU 1、内存 16Gi；不自动安装模型文件、配置 PVC、创建路由或分配 GPU。" />
    <el-alert v-if="draft.errors.length" :title="draft.errors.join('；')"
      type="error" :closable="false" style="margin-top: 12px" />
    <h3 style="margin-top: 16px">原生 InferenceService 清单（仅生成 JSON）</h3>
    <el-input type="textarea" readonly :model-value="manifestJson" :rows="14"
      placeholder="填写有效配置以生成 KServe 清单" />
    <el-button type="primary" plain :disabled="!draft.manifest"
      style="margin-top: 10px" @click="copyManifest">复制 KServe JSON</el-button>
    <span style="margin-left: 12px">{{ copyStatus }}</span>
    <el-divider />
    <h3>本地开发命令（手动执行，不在浏览器调用）</h3>
    <el-form-item label="本地 kubeconfig context">
      <el-input v-model.trim="context" placeholder="kind-gpu" />
    </el-form-item>
    <el-input type="textarea" readonly :rows="4" :model-value="commands" />
    <p class="table-footnote">先将 JSON 保存为 serving.json。KServe Controller Ready 并不等于真实模型响应，必须单独验证数据面。</p>
    <el-divider />
    <h3>真实 HTTP 推理验收 · 仅生成本地命令</h3>
    <el-alert type="warning" :closable="false"
      title="必须先由管理员确认 KServe Service / Ingress 对应关系，并将其转发到本机回环地址；这里不自动端口转发、执行推理或发送凭证。HTTP 200 及响应字段匹配仍不能证明模型质量或生产路由身份。" />
    <div class="detail-grid" style="margin-top: 12px">
      <el-form-item label="本地已授权 Port-forward URL">
        <el-input v-model.trim="httpUrl" placeholder="http://127.0.0.1:18080/v1/chat/completions" />
      </el-form-item>
      <el-form-item label="HTTP Host（可选，Ingress 名称路由）">
        <el-input v-model.trim="httpHost" placeholder="model-a.research.example.local" />
      </el-form-item>
    </div>
    <p>先根据实际推理镜像接口准备 <code>request.json</code> 和 <code>expect.json</code>。例如，OpenAI 兼容接口可断言 <code>/choices/0/message/content</code> 为非空字符串；并非所有自定义 Predictor 都支持这个协议。</p>
    <el-input type="textarea" readonly :rows="3"
      :model-value="httpAcceptanceCommand || '填写合法的本机 URL、Kubernetes context 和 KServe 清单后才能生成验收命令。'" />
    <p class="table-footnote">本地验收工具默认离线，可用 --execute 进行 KServe GET → 真实 HTTP POST → KServe GET，返回 SHA256 证据；原始请求和响应不会打印到 Portal。详情见 docs/KSERVE_DEV_SERVING.md。</p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ResourceRow } from './api'
import { buildKServeDraft, servingProjects } from './kserve-draft'
import { kserveHTTPAcceptanceCommand } from './kserve-http-command'

const props = defineProps<{ rows: ResourceRow[] }>()
const projects = computed(() => servingProjects(props.rows))
const projectId = ref('')
const namespace = ref('')
const serviceId = ref('')
const gpu = ref(1)
const image = ref('')
const argsJson = ref('["--model","/models/model"]')
const context = ref('')
const httpUrl = ref('')
const httpHost = ref('')
const copyStatus = ref('')
watch(projects, value => {
  if (!value.some(p => p.id === projectId.value)) projectId.value = value[0]?.id ?? ''
}, { immediate: true })
const draft = computed(() => buildKServeDraft({
  id: serviceId.value, projectId: projectId.value,
  namespace: namespace.value, image: image.value, argsJson: argsJson.value,
  wholeGpuCount: gpu.value,
}, projects.value))
const manifestJson = computed(() => draft.value.manifest
  ? JSON.stringify(draft.value.manifest, null, 2) : '')
const validContext = computed(() => /^[a-zA-Z0-9][a-zA-Z0-9_.:@/-]{0,127}$/.test(context.value))
const httpAcceptanceCommand = computed(() => kserveHTTPAcceptanceCommand({
  manifestValid: draft.value.manifest !== null,
  context: context.value, url: httpUrl.value, host: httpHost.value,
}))
const commands = computed(() => {
  if (!draft.value.manifest || !validContext.value) {
    return '填写 KServe 清单后，输入明确的本地 Kubernetes context 才显示命令。'
  }
  return 'python3 scripts/dev/kserve_service.py serving.json\n' +
    'python3 scripts/dev/kserve_service.py serving.json --context ' + context.value + ' --create\n' +
    'python3 scripts/dev/kserve_service.py serving.json --context ' + context.value + ' --status'
})
async function copyManifest(): Promise<void> {
  if (!draft.value.manifest) return
  try {
    await navigator.clipboard.writeText(manifestJson.value)
    copyStatus.value = '清单已复制；尚未创建任何集群资源。'
  } catch {
    copyStatus.value = '浏览器剪贴板权限不足，可手动复制 JSON。'
  }
}
</script>
