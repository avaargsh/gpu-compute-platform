<template>
  <el-dialog
    v-model="visible"
    title="修改密码"
    width="450px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
      size="default"
    >
      <el-form-item label="当前密码" prop="old_password">
        <el-input
          v-model="form.old_password"
          type="password"
          placeholder="请输入当前密码"
          show-password
          clearable
          autocomplete="current-password"
        />
      </el-form-item>
      
      <el-form-item label="新密码" prop="new_password">
        <el-input
          v-model="form.new_password"
          type="password"
          placeholder="请输入新密码（至少6个字符）"
          show-password
          clearable
          autocomplete="new-password"
        />
        
        <div class="password-strength" v-if="form.new_password">
          <div class="strength-meter">
            <div 
              class="strength-bar"
              :class="`strength-${passwordStrength.level}`"
              :style="{ width: `${passwordStrength.percentage}%` }"
            ></div>
          </div>
          <span class="strength-text" :class="`text-${passwordStrength.level}`">
            {{ passwordStrength.text }}
          </span>
        </div>
      </el-form-item>
      
      <el-form-item label="确认新密码" prop="confirm_password">
        <el-input
          v-model="form.confirm_password"
          type="password"
          placeholder="请再次输入新密码"
          show-password
          clearable
          autocomplete="new-password"
        />
      </el-form-item>
      
      <!-- 密码要求提示 -->
      <el-form-item>
        <div class="password-requirements">
          <el-text type="info" size="small">密码要求：</el-text>
          <ul class="requirements-list">
            <li :class="{ valid: requirements.length }">
              <el-icon><Check v-if="requirements.length" /><Close v-else /></el-icon>
              至少 6 个字符
            </li>
            <li :class="{ valid: requirements.hasLetter }">
              <el-icon><Check v-if="requirements.hasLetter" /><Close v-else /></el-icon>
              包含字母
            </li>
            <li :class="{ valid: requirements.hasNumber }">
              <el-icon><Check v-if="requirements.hasNumber" /><Close v-else /></el-icon>
              包含数字
            </li>
            <li :class="{ valid: requirements.hasSpecial }">
              <el-icon><Check v-if="requirements.hasSpecial" /><Close v-else /></el-icon>
              包含特殊字符（可选，提升安全性）
            </li>
          </ul>
        </div>
      </el-form-item>
    </el-form>
    
    <template #footer>
      <span class="dialog-footer">
        <el-button @click="handleClose">取消</el-button>
        <el-button 
          type="primary" 
          :loading="loading" 
          :disabled="!isFormValid"
          @click="handleSubmit"
        >
          确认修改
        </el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { Check, Close } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/userStore'
import type { ChangePasswordForm } from '@/types'

interface Props {
  modelValue: boolean
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
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
const form = reactive<ChangePasswordForm>({
  old_password: '',
  new_password: '',
  confirm_password: ''
})

// 密码要求检查
const requirements = computed(() => ({
  length: form.new_password.length >= 6,
  hasLetter: /[a-zA-Z]/.test(form.new_password),
  hasNumber: /\d/.test(form.new_password),
  hasSpecial: /[!@#$%^&*(),.?\":{}|<>]/.test(form.new_password)
}))

// 密码强度评估
const passwordStrength = computed(() => {
  const password = form.new_password
  if (!password) return { level: 'weak', percentage: 0, text: '' }
  
  let score = 0
  let checks = 0
  
  // 长度检查
  if (password.length >= 6) { score += 1; checks++ }
  if (password.length >= 8) { score += 1; checks++ }
  if (password.length >= 12) { score += 1; checks++ }
  
  // 字符类型检查
  if (/[a-z]/.test(password)) { score += 1; checks++ }
  if (/[A-Z]/.test(password)) { score += 1; checks++ }
  if (/\d/.test(password)) { score += 1; checks++ }
  if (/[!@#$%^&*(),.?\":{}|<>]/.test(password)) { score += 2; checks++ }
  
  // 计算强度等级
  let level = 'weak'
  let text = '弱'
  let percentage = 20
  
  if (score >= 8) {
    level = 'strong'
    text = '强'
    percentage = 100
  } else if (score >= 5) {
    level = 'medium'
    text = '中等'
    percentage = 60
  } else if (score >= 3) {
    level = 'fair'
    text = '一般'
    percentage = 40
  }
  
  return { level, percentage, text }
})

// 表单是否有效
const isFormValid = computed(() => {
  return form.old_password && 
         form.new_password && 
         form.confirm_password &&
         requirements.value.length &&
         requirements.value.hasLetter &&
         requirements.value.hasNumber &&
         form.new_password === form.confirm_password
})

// 自定义验证函数
const validateOldPassword = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请输入当前密码'))
  } else if (value.length < 6) {
    callback(new Error('当前密码长度不正确'))
  } else {
    callback()
  }
}

const validateNewPassword = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请输入新密码'))
  } else if (value.length < 6) {
    callback(new Error('新密码至少6个字符'))
  } else if (!requirements.value.hasLetter) {
    callback(new Error('新密码必须包含字母'))
  } else if (!requirements.value.hasNumber) {
    callback(new Error('新密码必须包含数字'))
  } else if (value === form.old_password) {
    callback(new Error('新密码不能与当前密码相同'))
  } else {
    // 如果确认密码已经输入，重新验证确认密码
    if (form.confirm_password) {
      formRef.value?.validateField('confirm_password')
    }
    callback()
  }
}

const validateConfirmPassword = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请确认新密码'))
  } else if (value !== form.new_password) {
    callback(new Error('两次输入的密码不一致'))
  } else {
    callback()
  }
}

// 表单验证规则
const rules: FormRules = {
  old_password: [
    { validator: validateOldPassword, trigger: 'blur' }
  ],
  new_password: [
    { validator: validateNewPassword, trigger: 'blur' }
  ],
  confirm_password: [
    { validator: validateConfirmPassword, trigger: 'blur' }
  ]
}

// 重置表单
const resetForm = () => {
  form.old_password = ''
  form.new_password = ''
  form.confirm_password = ''
  
  if (formRef.value) {
    formRef.value.clearValidate()
  }
}

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    loading.value = true

    const success = await userStore.changePassword(form)
    
    if (success) {
      visible.value = false
      resetForm()
    }
  } catch (error) {
    console.error('Change password validation failed:', error)
  } finally {
    loading.value = false
  }
}

// 关闭对话框
const handleClose = () => {
  visible.value = false
  resetForm()
}

// 监听对话框打开，重置表单
watch(
  () => props.modelValue,
  (newValue) => {
    if (newValue) {
      resetForm()
    }
  }
)
</script>

<style scoped>
.password-strength {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.strength-meter {
  flex: 1;
  height: 4px;
  background-color: #e4e7ed;
  border-radius: 2px;
  overflow: hidden;
}

.strength-bar {
  height: 100%;
  transition: all 0.3s ease;
  border-radius: 2px;
}

.strength-bar.strength-weak {
  background-color: #f56c6c;
}

.strength-bar.strength-fair {
  background-color: #e6a23c;
}

.strength-bar.strength-medium {
  background-color: #409eff;
}

.strength-bar.strength-strong {
  background-color: #67c23a;
}

.strength-text {
  font-size: 12px;
  font-weight: 500;
  min-width: 30px;
}

.text-weak {
  color: #f56c6c;
}

.text-fair {
  color: #e6a23c;
}

.text-medium {
  color: #409eff;
}

.text-strong {
  color: #67c23a;
}

.password-requirements {
  width: 100%;
  padding: 12px;
  background-color: #f8f9fa;
  border-radius: 6px;
  border: 1px solid #e9ecef;
}

.requirements-list {
  margin: 8px 0 0 0;
  padding: 0;
  list-style: none;
}

.requirements-list li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0;
  font-size: 12px;
  color: #909399;
  transition: color 0.3s ease;
}

.requirements-list li.valid {
  color: #67c23a;
}

.requirements-list li .el-icon {
  font-size: 14px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

/* Element Plus 样式覆盖 */
:deep(.el-form-item__content) {
  line-height: 1.4;
}

:deep(.el-input__inner) {
  transition: border-color 0.3s ease;
}

/* 响应式设计 */
@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95% !important;
    margin: 5vh auto;
  }
  
  :deep(.el-form-item__label) {
    font-size: 14px;
  }
  
  .requirements-list li {
    font-size: 11px;
  }
}
</style>
