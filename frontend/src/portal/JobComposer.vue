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
      <p class="table-footnote">下一步由 OIDC + Project RBAC + CAS 审计的 Management API 提交，禁止复用只读 Operator Token 进行写入。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ResourceRow } from './api'
import { buildWorkloadDraft, draftPools, type WorkloadMode } from './workload-draft'

const props = defineProps<{ rows: ResourceRow[] }>()
const pools = computed(() => draftPools(props.rows))
const mode = ref<WorkloadMode>('training')
const poolId = ref('')
const selectedPool = computed(() => pools.value.find(p => p.id === poolId.value))
const acceleratorClass = ref('')
const quota = ref(1)
const workloadId = ref('')
const image = ref('')
const commandJson = ref('["python","train.py"]')
const copyStatus = ref('')

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
}, pools.value))
const payloadText = computed(() => draft.value.payload ? JSON.stringify(draft.value.payload, null, 2) : '')
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
