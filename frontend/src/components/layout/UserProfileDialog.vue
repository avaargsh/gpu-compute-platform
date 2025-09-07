<template>
  <el-dialog
    v-model="visible"
    title="个人资料"
    width="500px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="80px"
      size="default"
    >
      <el-form-item label="用户名">
        <el-input :value="userStore.user?.username" disabled />
      </el-form-item>
      
      <el-form-item label="邮箱">
        <el-input :value="userStore.user?.email" disabled />
      </el-form-item>
      
      <el-form-item label="角色">
        <el-tag :type="userStore.isAdmin ? 'danger' : 'primary'" size="small">
          {{ userStore.isAdmin ? '管理员' : '普通用户' }}
        </el-tag>
      </el-form-item>
      
      <el-form-item label="昵称" prop="nickname">
        <el-input
          v-model="form.nickname"
          placeholder="请输入昵称"
          clearable
          maxlength="20"
          show-word-limit
        />
      </el-form-item>
      
      <el-form-item label="头像" prop="avatar">
        <div class="avatar-section">
          <el-avatar
            :size="64"
            :src="form.avatar || userStore.user?.avatar"
            class="avatar-preview"
          >
            <el-icon><User /></el-icon>
          </el-avatar>
          
          <div class="avatar-actions">
            <el-upload
              :show-file-list="false"
              :before-upload="handleAvatarUpload"
              accept="image/*"
              action="#"
              class="avatar-uploader"
            >
              <el-button size="small" type="primary">
                <el-icon><Upload /></el-icon>
                上传头像
              </el-button>
            </el-upload>
            
            <el-button
              v-if="form.avatar"
              size="small"
              @click="removeAvatar"
            >
              <el-icon><Delete /></el-icon>
              移除头像
            </el-button>
          </div>
        </div>
        
        <div class="avatar-tips">
          <el-text type="info" size="small">
            支持 JPG、PNG 格式，文件大小不超过 2MB
          </el-text>
        </div>
      </el-form-item>
      
      <el-form-item label="注册时间">
        <el-text>{{ formatTime(userStore.user?.created_at) }}</el-text>
      </el-form-item>
      
      <el-form-item label="最后登录">
        <el-text>{{ formatTime(userStore.user?.last_login) || '从未登录' }}</el-text>
      </el-form-item>
    </el-form>
    
    <template #footer>
      <span class="dialog-footer">
        <el-button @click="handleClose">取消</el-button>
        <el-button type="primary" :loading="loading" @click="handleSubmit">
          保存
        </el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules, UploadRawFile } from 'element-plus'
import { User, Upload, Delete } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/userStore'
import type { UserProfile } from '@/types'

interface Props {
  modelValue: boolean
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'updated'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const userStore = useUserStore()

// 组件状态
const formRef = ref<FormInstance>()
const loading = ref(false)

// 计算属性
const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

// 表单数据
const form = reactive<UserProfile & { avatar?: string }>({
  nickname: '',
  email: '',
  avatar: ''
})

// 表单验证规则
const rules: FormRules = {
  nickname: [
    { required: true, message: '请输入昵称', trigger: 'blur' },
    { min: 2, max: 20, message: '昵称长度应在2-20个字符之间', trigger: 'blur' }
  ]
}

// 初始化表单数据
const initForm = () => {
  if (userStore.user) {
    form.nickname = userStore.user.nickname
    form.email = userStore.user.email
    form.avatar = userStore.user.avatar || ''
  }
}

// 格式化时间
const formatTime = (time?: string) => {
  if (!time) return ''
  return new Date(time).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// 处理头像上传
const handleAvatarUpload = (file: UploadRawFile): boolean => {
  // 检查文件类型
  const isValidType = file.type === 'image/jpeg' || file.type === 'image/png' || file.type === 'image/jpg'
  if (!isValidType) {
    ElMessage.error('头像只能是 JPG 或 PNG 格式')
    return false
  }

  // 检查文件大小
  const isLt2M = file.size / 1024 / 1024 < 2
  if (!isLt2M) {
    ElMessage.error('头像大小不能超过 2MB')
    return false
  }

  // 读取文件并转换为 base64
  const reader = new FileReader()
  reader.onload = (e) => {
    form.avatar = e.target?.result as string
  }
  reader.readAsDataURL(file)

  return false // 阻止自动上传
}

// 移除头像
const removeAvatar = () => {
  ElMessageBox.confirm(
    '确认要移除头像吗？',
    '确认操作',
    {
      confirmButtonText: '确认',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(() => {
    form.avatar = ''
    ElMessage.success('头像已移除')
  }).catch(() => {
    // 取消操作
  })
}

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    loading.value = true

    const profileData: UserProfile = {
      nickname: form.nickname,
      email: form.email,
      avatar: form.avatar
    }

    const success = await userStore.updateProfile(profileData)
    
    if (success) {
      emit('updated')
      visible.value = false
    }
  } catch (error) {
    console.error('Profile update validation failed:', error)
  } finally {
    loading.value = false
  }
}

// 关闭对话框
const handleClose = () => {
  visible.value = false
  // 重置表单
  if (formRef.value) {
    formRef.value.clearValidate()
  }
  initForm()
}

// 监听对话框打开，初始化表单数据
watch(
  () => props.modelValue,
  (newValue) => {
    if (newValue) {
      initForm()
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.avatar-section {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 8px;
}

.avatar-preview {
  flex-shrink: 0;
  border: 2px solid #e1e5e9;
}

.avatar-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.avatar-uploader {
  display: block;
}

.avatar-tips {
  margin-top: 8px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

/* Element Plus 样式覆盖 */
:deep(.el-upload) {
  display: block;
}

:deep(.el-form-item__content) {
  line-height: 1.4;
}

:deep(.el-text) {
  font-size: 14px;
}

/* 响应式设计 */
@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95% !important;
    margin: 5vh auto;
  }
  
  .avatar-section {
    flex-direction: column;
    align-items: center;
    text-align: center;
  }
  
  .avatar-actions {
    flex-direction: row;
    justify-content: center;
  }
}
</style>
