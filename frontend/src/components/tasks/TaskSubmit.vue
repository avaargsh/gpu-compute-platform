<template>
  <div class="task-submit">
    <el-card shadow="hover">
      <template #header>
        <div class="card-header">
          <span>创建新任务</span>
        </div>
      </template>

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
        size="default"
      >
        <el-form-item label="任务名称" prop="name">
          <el-input
            v-model="form.name"
            placeholder="请输入任务名称"
            clearable
          />
        </el-form-item>

        <el-form-item label="脚本文件" prop="script_path">
          <div class="script-upload">
            <el-input
              v-model="form.script_path"
              placeholder="脚本文件路径或URL"
              clearable
            >
              <template #append>
                <el-button @click="showUploadDialog = true">
                  <el-icon><Upload /></el-icon>
                  上传
                </el-button>
              </template>
            </el-input>
          </div>
        </el-form-item>

        <el-form-item label="依赖包" prop="requirements">
          <el-input
            v-model="requirementsText"
            type="textarea"
            :rows="4"
            placeholder="请输入依赖包，每行一个，例如：&#10;numpy==1.21.0&#10;torch>=1.9.0&#10;transformers"
          />
        </el-form-item>

        <el-form-item label="GPU类型">
          <el-select
            v-model="form.gpu_type"
            placeholder="选择GPU类型（可选）"
            clearable
            style="width: 100%"
          >
            <el-option label="NVIDIA RTX 4090" value="rtx4090" />
            <el-option label="NVIDIA A100" value="a100" />
            <el-option label="NVIDIA V100" value="v100" />
            <el-option label="NVIDIA Tesla K80" value="k80" />
          </el-select>
        </el-form-item>

        <el-form-item label="最大运行时间">
          <el-input-number
            v-model="form.max_duration"
            :min="1"
            :max="24"
            :step="0.5"
            placeholder="小时"
            style="width: 100%"
          />
          <div class="form-tip">任务运行超过此时间将自动终止（可选）</div>
        </el-form-item>

        <el-form-item label="实例推荐">
          <el-button @click="getRecommendations" :loading="loadingRecommendations">
            获取实例推荐
          </el-button>
        </el-form-item>

        <!-- 实例推荐结果 -->
        <div v-if="recommendations.length > 0" class="recommendations">
          <h4>推荐实例</h4>
          <el-table :data="recommendations" size="small">
            <el-table-column prop="name" label="实例名称" width="150" />
            <el-table-column prop="gpu_type" label="GPU类型" width="120" />
            <el-table-column label="配置" width="150">
              <template #default="{ row }">
                {{ row.vcpus }}核 / {{ row.memory_gb }}GB
              </template>
            </el-table-column>
            <el-table-column label="预估成本" width="100">
              <template #default="{ row }">
                ${{ row.total_cost.toFixed(2) }}
              </template>
            </el-table-column>
            <el-table-column label="性能评分" width="100">
              <template #default="{ row }">
                {{ row.performance_score }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="80">
              <template #default="{ row }">
                <el-button
                  size="small"
                  type="primary"
                  @click="selectInstance(row)"
                >
                  选择
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <el-form-item>
          <el-button
            type="primary"
            @click="handleSubmit"
            :loading="submitting"
            size="large"
          >
            <el-icon><Check /></el-icon>
            创建任务
          </el-button>
          <el-button @click="handleReset" size="large">
            <el-icon><RefreshLeft /></el-icon>
            重置
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 文件上传对话框 -->
    <el-dialog
      v-model="showUploadDialog"
      title="上传脚本文件"
      width="500px"
    >
      <el-upload
        ref="uploadRef"
        action="/api/files/upload"
        :auto-upload="false"
        :show-file-list="true"
        :limit="1"
        accept=".py,.sh,.r,.ipynb"
        @change="handleFileChange"
      >
        <el-button type="primary">
          <el-icon><Upload /></el-icon>
          选择文件
        </el-button>
        <template #tip>
          <div class="el-upload__tip">
            支持 .py, .sh, .r, .ipynb 格式的文件，文件大小不超过 50MB
          </div>
        </template>
      </el-upload>

      <template #footer>
        <span class="dialog-footer">
          <el-button @click="showUploadDialog = false">取消</el-button>
          <el-button
            type="primary"
            @click="handleUpload"
            :loading="uploading"
          >
            确认上传
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { useTaskStore } from '@/stores/taskStore'
import { useCostStore } from '@/stores/costStore'
import type { TaskSubmitData, InstanceRecommendation } from '@/types'

const router = useRouter()
const taskStore = useTaskStore()
const costStore = useCostStore()

// 表单引用
const formRef = ref<FormInstance>()
const uploadRef = ref()

// 状态
const submitting = ref(false)
const loadingRecommendations = ref(false)
const uploading = ref(false)
const showUploadDialog = ref(false)
const selectedFile = ref<File | null>(null)

// 表单数据
const form = reactive<TaskSubmitData & { max_duration?: number }>({
  name: '',
  script_path: '',
  requirements: [],
  gpu_type: '',
  max_duration: undefined
})

// 依赖包文本（用于textarea显示）
const requirementsText = ref('')

// 表单验证规则
const rules: FormRules = {
  name: [
    { required: true, message: '请输入任务名称', trigger: 'blur' },
    { min: 2, max: 50, message: '长度在 2 到 50 个字符', trigger: 'blur' }
  ],
  script_path: [
    { required: true, message: '请选择或输入脚本文件路径', trigger: 'blur' }
  ]
}

// 计算属性
const recommendations = computed(() => costStore.recommendations)

// 事件处理
const handleSubmit = async () => {
  if (!formRef.value) return
  
  try {
    await formRef.value.validate()
    
    // 处理依赖包
    form.requirements = requirementsText.value
      .split('\n')
      .map(line => line.trim())
      .filter(line => line.length > 0)
    
    submitting.value = true
    
    const newTask = await taskStore.submitTask(form as TaskSubmitData)
    
    ElMessage.success('任务创建成功！')
    
    // 跳转到任务详情页
    router.push(`/tasks/${newTask.id}`)
    
  } catch (error) {
    if (error !== false) { // 验证错误时error为false
      ElMessage.error('创建任务失败')
    }
  } finally {
    submitting.value = false
  }
}

const handleReset = () => {
  formRef.value?.resetFields()
  requirementsText.value = ''
  form.gpu_type = ''
  form.max_duration = undefined
}

const getRecommendations = async () => {
  loadingRecommendations.value = true
  try {
    await costStore.fetchRecommendations({
      task_type: 'general',
      duration_estimate: form.max_duration || 2
    })
  } catch (error) {
    ElMessage.error('获取推荐失败')
  } finally {
    loadingRecommendations.value = false
  }
}

const selectInstance = (instance: InstanceRecommendation) => {
  form.gpu_type = instance.gpu_type
  ElMessage.success(`已选择实例: ${instance.name}`)
}

const handleFileChange = (file: any) => {
  selectedFile.value = file.raw
}

const handleUpload = async () => {
  if (!selectedFile.value) {
    ElMessage.warning('请先选择文件')
    return
  }
  
  uploading.value = true
  try {
    // TODO: 实现文件上传逻辑
    // const response = await fileApi.uploadFile(selectedFile.value)
    // form.script_path = response.data.file_path
    
    form.script_path = '/uploads/' + selectedFile.value.name
    
    ElMessage.success('文件上传成功')
    showUploadDialog.value = false
  } catch (error) {
    ElMessage.error('文件上传失败')
  } finally {
    uploading.value = false
  }
}
</script>

<style scoped>
.task-submit {
  padding: 24px;
  max-width: 800px;
  margin: 0 auto;
}

.card-header {
  font-size: 18px;
  font-weight: 600;
}

.script-upload {
  width: 100%;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}

.recommendations {
  margin-bottom: 24px;
}

.recommendations h4 {
  margin: 0 0 12px 0;
  color: #303133;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
}

:deep(.el-form-item__label) {
  font-weight: 500;
}

:deep(.el-textarea__inner) {
  font-family: monospace;
}
</style>
