<template>
  <div>
    <div class="panel-heading"><div>
      <h2>训练 / 批量推理任务</h2>
      <p>从已授权项目的 ComputePool 生成 v0.1 Workload 意图；当前为可复制的部署草案。</p>
    </div><el-tag type="warning" effect="plain">Draft · 不自动提交</el-tag></div>
    <el-alert type="info" :closable="false" show-icon
      title="训练和批量推理走 Kubernetes Job + Kueue；在线推理 API 属于后续 Serving 控制面，不由本任务向导承诺。" />
    <el-form label-position="top" style="margin-top: 18px">
      <el-form-item label="任务类型">
        <el-radio-group v-model="mode">
          <el-radio-button value="training">训练任务</el-radio-button>
          <el-radio-button value="batch-inference">批量推理</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="已授权项目 / ComputePool">
        <el-select v-model="poolId" placeholder="从真实资源快照中选择" style="width: 100%">
          <el-option v-for="pool in pools" :key="pool.id" :value="pool.id"
            :label="pool.projectId + ' / ' + pool.id" />
        </el-select>
      </el-form-item>
      <el-alert v-if="!pools.length" type="warning" :closable="false"
        title="当前读取的页面没有有效 ComputePool；如资源被分页截断，请先加载完整资源列表。" />
      <div class="detail-grid">
        <el-form-item label="Workload ID">
          <el-input v-model.trim="workloadId" placeholder="train-qwen-001" maxlength="128" />
        </el-form-item>
        <el-form-item label="加速器类">
          <el-select v-model="acceleratorClass" placeholder="绑定类" style="width: 100%">
            <el-option v-for="acc in selectedPool?.accelerators ?? []" :key="acc.class"
              :label="acc.class + ' · 声明配额 ' + acc.quota" :value="acc.class" />
          </el-select>
        </el-form-item>
        <el-form-item label="请求 GPU 数量">
          <el-input-number v-model="quota" :min="1" :max="256" />
        </el-form-item>
      </div>
      <el-form-item label="容器镜像">
        <el-input v-model.trim="image" placeholder="registry.example/team/train@sha256:..." />
      </el-form-item>
      <el-form-item label="持久工作区 PVC（可选，必须已在项目 Namespace 中创建）">
        <el-input v-model.trim="workspaceClaim" placeholder="training-data-pvc" maxlength="253" />
        <small>容器内固定挂载到 /workspace；删除 Job 不删除 PVC，所填名称不代表已具备访问权限。</small>
      </el-form-item>
      <el-form-item label="Command（JSON argv 数组，不执行 Shell 模板插值）">
        <el-input v-model="commandJson" type="textarea" :rows="3" />
      </el-form-item>
    </el-form>
    <el-alert type="warning" :closable="false"
      title="ComputePool 配额只是声明值，不代表 GPU 实时可用、Kueue 已准入、模型已就绪或训练结果已持久化。" />
    <el-alert v-if="draft.errors.length" style="margin-top: 12px" type="error" :closable="false"
      :title="draft.errors.join('；')" />
    <div style="margin-top: 16px">
      <h3>Workload 意图（仅本地生成）</h3>
      <el-input :model-value="payloadText" type="textarea" :rows="14" readonly
        placeholder="填写有效的项目、镜像和任务参数以生成部署草案" />
      <div style="margin-top: 12px">
        <el-button type="primary" :disabled="!draft.payload" @click="copyDraft">复制 JSON 草案</el-button>
        <span style="margin-left: 12px">{{ copyStatus }}</span>
      </div>
      <el-divider />
      <h3>本机开发提交（可选）</h3>
      <el-alert type="warning" :closable="false"
        title="仅开发环境：必须在服务端设置独立 PORTAL_OPERATOR_WRITE_TOKEN，且不能与读取凭证相同。部署至 PoC/生产模式时该入口禁用。Kueue 是否准入与 Job 是否成功仍需要独立观察。" />
      <el-form-item label="独立写入凭证（只存在当前页面内存）" style="margin-top: 12px">
        <el-input v-model="writeToken" type="password" autocomplete="off"
          placeholder="PORTAL_OPERATOR_WRITE_TOKEN（至少 32 字符）" show-password />
      </el-form-item>
      <el-button type="primary" :loading="submitting"
        :disabled="submitting || !!currentSubmission || !draft.payload || readToken.length < 32 || writeToken.length < 32"
        @click="submitWorkload">
        创建 Workload Desired（仅本机开发）
      </el-button>
      <div v-if="currentSubmission" style="margin-top: 12px">
        <el-alert
          :type="currentSubmission.kind === 'accepted' ? 'success' : 'warning'"
          :title="currentSubmission.detail" :closable="false" />
        <el-button type="primary" plain style="margin-top: 10px"
          @click="emit('submitted')">
          重新读取授权快照（核对 Desired / Observation）
        </el-button>
      </div>
      <el-alert v-if="submitError" :title="submitError" type="error"
        :closable="false" style="margin-top: 12px" />
      <p class="table-footnote" v-if="currentSubmission">
        同一草案不会自动重试。要提交新的任务，请修改 Workload ID 并检查项目/资源配额；
        结果未知时必须先刷新授权快照核对 Desired，未读到也不能证明未写入。
      </p>
      <p class="table-footnote">此处仅创建 Desired State。实际 Pod / Kueue Admission、Job 日志和训练 Checkpoint 须在下方执行监控中查看；操作员预览读取凭证单独使用不能创建资源。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ResourceRow } from './api'
import { buildWorkloadDraft, draftPools, type WorkloadMode } from './workload-draft'
import { submitDevWorkload, WorkloadSubmissionUncertainError } from './dev-submit'

const props = defineProps<{ rows: ResourceRow[]; clusterId: string; readToken: string }>()
const emit = defineEmits<{ submitted: [] }>()
const pools = computed(() => draftPools(props.rows))
const mode = ref<WorkloadMode>('training')
const poolId = ref('')
const selectedPool = computed(() => pools.value.find(p => p.id === poolId.value))
const acceleratorClass = ref('')
const quota = ref(1)
const workloadId = ref('')
const image = ref('')
const workspaceClaim = ref('')
const commandJson = ref('["python","train.py"]')
const copyStatus = ref('')
const writeToken = ref('')
const submitting = ref(false)
const submitError = ref('')
const submissionState = ref<{ fingerprint: string; kind: 'accepted' | 'unknown'; detail: string } | null>(null)

watch(pools, value => {
  if (!value.some(p => p.id === poolId.value)) poolId.value = value[0]?.id ?? ''
}, { immediate: true })
watch(selectedPool, pool => {
  if (!pool?.accelerators.some(a => a.class === acceleratorClass.value)) {
    acceleratorClass.value = pool?.accelerators[0]?.class ?? ''
  }
}, { immediate: true })
watch(mode, value => {
  commandJson.value = value === 'training' ? '["python","train.py"]' : '["python","batch_infer.py"]'
})
const draft = computed(() => buildWorkloadDraft({
  id: workloadId.value, poolId: poolId.value, acceleratorClass: acceleratorClass.value,
  quota: quota.value, image: image.value, commandJson: commandJson.value, mode: mode.value,
  workspaceClaim: workspaceClaim.value,
}, pools.value))
const payloadText = computed(() => draft.value.payload ? JSON.stringify(draft.value.payload, null, 2) : '')
const submissionFingerprint = computed(() => JSON.stringify({
  clusterId: props.clusterId, payload: draft.value.payload,
}))
const currentSubmission = computed(() =>
  submissionState.value?.fingerprint === submissionFingerprint.value ? submissionState.value : null)

async function submitWorkload(): Promise<void> {
  const payload = draft.value.payload
  if (!payload || submitting.value || currentSubmission.value) return
  // Bind feedback to the exact intent that was sent; form fields can change
  // while a request is in flight. An unknown outcome must not be auto-retried.
  const cluster = props.clusterId
  const reader = props.readToken
  const fingerprint = submissionFingerprint.value
  submitting.value = true
  submitError.value = ''
  try {
    await submitDevWorkload(cluster, reader, writeToken.value, payload)
    submissionState.value = {
      fingerprint, kind: 'accepted',
      detail: 'Desired 已接收（HTTP 204）：' + payload.projectId + '/' + payload.metadata.id +
        ' · Generation ' + payload.metadata.generation + '。请刷新并检查 Agent Observation；不代表 Kueue 准入、Pod 运行或训练成功。',
    }
    // Keep the confirmation on screen until the operator explicitly asks
    // for a read-back. Auto-refresh unmounts the form and loses the outcome
    // and no-retry guard before the operator can inspect them.
  } catch (cause) {
    if (cause instanceof WorkloadSubmissionUncertainError) {
      submissionState.value = { fingerprint, kind: 'unknown', detail: cause.message }
    } else {
      submitError.value = cause instanceof Error ? cause.message : '提交被拒绝'
    }
  } finally {
    submitting.value = false
  }
}

watch(() => [props.readToken, props.clusterId], () => {
  writeToken.value = ''
  submitError.value = ''
  submissionState.value = null
})

async function copyDraft(): Promise<void> {
  if (!draft.value.payload) return
  try {
    await navigator.clipboard.writeText(payloadText.value)
    copyStatus.value = '已复制。尚未创建任何服务器资源。'
  } catch {
    copyStatus.value = '浏览器禁止剪贴板访问，可从上方只读文本框手动复制。'
  }
}
</script>
