<template>
  <div class="register-page">
    <div class="register-container">
      <div class="register-header">
        <h1 class="title">创建账户</h1>
        <p class="subtitle">加入GPU计算平台，开始您的AI之旅</p>
      </div>
      
      <el-card class="register-card" shadow="hover">
        <template #header>
          <span class="card-header">用户注册</span>
        </template>
        
        <el-form
          ref="registerFormRef"
          :model="registerForm"
          :rules="registerRules"
          label-width="80px"
          size="large"
          @keyup.enter="handleRegister"
        >
          <el-form-item label="用户名" prop="username">
            <el-input
              v-model="registerForm.username"
              placeholder="请输入用户名（3-20个字符）"
              prefix-icon="User"
              clearable
            />
          </el-form-item>
          
          <el-form-item label="邮箱" prop="email">
            <el-input
              v-model="registerForm.email"
              placeholder="请输入邮箱地址"
              prefix-icon="Message"
              clearable
            />
          </el-form-item>
          
          <el-form-item label="昵称" prop="nickname">
            <el-input
              v-model="registerForm.nickname"
              placeholder="请输入昵称"
              prefix-icon="Avatar"
              clearable
            />
          </el-form-item>
          
          <el-form-item label="密码" prop="password">
            <el-input
              v-model="registerForm.password"
              type="password"
              placeholder="请输入密码（至少6个字符）"
              prefix-icon="Lock"
              show-password
              clearable
            />
          </el-form-item>
          
          <el-form-item label="确认密码" prop="confirmPassword">
            <el-input
              v-model="registerForm.confirmPassword"
              type="password"
              placeholder="请再次输入密码"
              prefix-icon="Lock"
              show-password
              clearable
            />
          </el-form-item>
          
          <el-form-item prop="agreement">
            <el-checkbox v-model="registerForm.agreement">
              我已阅读并同意
              <el-link type="primary" :underline="false" @click="showUserAgreement">
                《用户服务协议》
              </el-link>
              和
              <el-link type="primary" :underline="false" @click="showPrivacyPolicy">
                《隐私政策》
              </el-link>
            </el-checkbox>
          </el-form-item>
          
          <el-form-item>
            <el-button
              type="primary"
              size="large"
              :loading="loading"
              :disabled="loading || !registerForm.agreement"
              @click="handleRegister"
              class="register-button"
            >
              {{ loading ? '注册中...' : '立即注册' }}
            </el-button>
          </el-form-item>
          
          <el-form-item>
            <div class="login-link">
              已有账户？
              <el-link type="primary" :underline="false" @click="goToLogin">
                立即登录
              </el-link>
            </div>
          </el-form-item>
        </el-form>
      </el-card>
    </div>
    
    <!-- 用户协议对话框 -->
    <el-dialog
      v-model="agreementVisible"
      title="用户服务协议"
      width="70%"
      :close-on-click-modal="false"
    >
      <div class="agreement-content">
        <h3>1. 服务条款</h3>
        <p>欢迎使用GPU计算平台。本协议规定了您使用本平台服务时的权利和义务。</p>
        
        <h3>2. 账户安全</h3>
        <p>您有责任保护您的账户安全，包括保护密码不被泄露。对于因您的疏忽导致的账户安全问题，平台不承担责任。</p>
        
        <h3>3. 服务使用</h3>
        <p>您可以使用本平台提供的GPU计算服务，但必须遵守相关法律法规，不得用于非法用途。</p>
        
        <h3>4. 费用说明</h3>
        <p>使用平台服务可能产生费用，具体费用标准请参考平台定价页面。</p>
        
        <h3>5. 服务变更</h3>
        <p>平台有权在合理通知的情况下修改服务内容和本协议条款。</p>
        
        <h3>6. 免责声明</h3>
        <p>平台不对因不可抗力或其他非平台原因导致的服务中断承担责任。</p>
      </div>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="agreementVisible = false">关闭</el-button>
          <el-button type="primary" @click="acceptAgreement">同意协议</el-button>
        </span>
      </template>
    </el-dialog>
    
    <!-- 隐私政策对话框 -->
    <el-dialog
      v-model="privacyVisible"
      title="隐私政策"
      width="70%"
      :close-on-click-modal="false"
    >
      <div class="privacy-content">
        <h3>1. 信息收集</h3>
        <p>我们收集您主动提供的信息，包括但不限于用户名、邮箱地址、联系方式等。</p>
        
        <h3>2. 信息使用</h3>
        <p>我们使用收集的信息来提供和改善服务，包括用户身份验证、服务优化等。</p>
        
        <h3>3. 信息保护</h3>
        <p>我们采用合理的安全措施保护您的个人信息，防止未经授权的访问、使用或披露。</p>
        
        <h3>4. 信息共享</h3>
        <p>除非获得您的明确同意或法律要求，我们不会向第三方分享您的个人信息。</p>
        
        <h3>5. Cookie使用</h3>
        <p>我们可能使用Cookie来改善用户体验和分析服务使用情况。</p>
        
        <h3>6. 政策更新</h3>
        <p>我们可能会不时更新本隐私政策，重要变更会通过适当方式通知您。</p>
      </div>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="privacyVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { useUserStore } from '@/stores/userStore'
import type { RegisterForm } from '@/types'

const router = useRouter()
const userStore = useUserStore()

// 表单引用
const registerFormRef = ref<FormInstance>()

// 表单数据
const registerForm = reactive<RegisterForm>({
  username: '',
  email: '',
  nickname: '',
  password: '',
  confirmPassword: '',
  agreement: false
})

// 加载状态
const loading = ref(false)

// 对话框显示状态
const agreementVisible = ref(false)
const privacyVisible = ref(false)

// 自定义验证函数
const validateEmail = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请输入邮箱地址'))
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) {
    callback(new Error('请输入有效的邮箱地址'))
  } else {
    callback()
  }
}

const validatePassword = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请输入密码'))
  } else if (value.length < 6) {
    callback(new Error('密码至少6个字符'))
  } else if (!/(?=.*[a-z])(?=.*[A-Z])|(?=.*[a-z])(?=.*\d)|(?=.*[A-Z])(?=.*\d)/.test(value)) {
    callback(new Error('密码应包含字母和数字的组合'))
  } else {
    callback()
  }
}

const validateConfirmPassword = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请确认密码'))
  } else if (value !== registerForm.password) {
    callback(new Error('两次输入的密码不一致'))
  } else {
    callback()
  }
}

const validateUsername = (rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请输入用户名'))
  } else if (value.length < 3 || value.length > 20) {
    callback(new Error('用户名长度应在3-20个字符之间'))
  } else if (!/^[a-zA-Z0-9_]+$/.test(value)) {
    callback(new Error('用户名只能包含字母、数字和下划线'))
  } else {
    callback()
  }
}

// 表单验证规则
const registerRules: FormRules = {
  username: [
    { validator: validateUsername, trigger: 'blur' }
  ],
  email: [
    { validator: validateEmail, trigger: 'blur' }
  ],
  nickname: [
    { required: true, message: '请输入昵称', trigger: 'blur' },
    { min: 2, max: 20, message: '昵称长度应在2-20个字符之间', trigger: 'blur' }
  ],
  password: [
    { validator: validatePassword, trigger: 'blur' }
  ],
  confirmPassword: [
    { validator: validateConfirmPassword, trigger: 'blur' }
  ],
  agreement: [
    { 
      type: 'boolean',
      required: true,
      transform: (value: boolean) => value,
      message: '请阅读并同意用户协议',
      trigger: 'change'
    }
  ]
}

// 处理注册
const handleRegister = async () => {
  if (!registerFormRef.value) return

  try {
    await registerFormRef.value.validate()
    loading.value = true

    const success = await userStore.register(registerForm)
    
    if (success) {
      // 注册成功，跳转到首页
      ElMessage.success('注册成功，欢迎加入GPU计算平台！')
      await router.push('/dashboard')
    }
  } catch (error) {
    console.error('Register validation failed:', error)
  } finally {
    loading.value = false
  }
}

// 跳转到登录页面
const goToLogin = () => {
  router.push('/login')
}

// 显示用户协议
const showUserAgreement = () => {
  agreementVisible.value = true
}

// 显示隐私政策
const showPrivacyPolicy = () => {
  privacyVisible.value = true
}

// 接受用户协议
const acceptAgreement = () => {
  registerForm.agreement = true
  agreementVisible.value = false
  ElMessage.success('已同意用户服务协议')
}

// 组件挂载时检查是否已登录
onMounted(() => {
  if (userStore.isLoggedIn) {
    router.push('/dashboard')
  }
})
</script>

<style scoped>
.register-page {
  min-height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.register-container {
  width: 100%;
  max-width: 500px;
}

.register-header {
  text-align: center;
  margin-bottom: 30px;
  color: white;
}

.title {
  font-size: 2.5rem;
  font-weight: 700;
  margin-bottom: 10px;
  text-shadow: 2px 2px 4px rgba(0, 0, 0, 0.1);
}

.subtitle {
  font-size: 1rem;
  opacity: 0.9;
  margin: 0;
}

.register-card {
  border-radius: 12px;
  overflow: hidden;
}

.card-header {
  font-size: 1.2rem;
  font-weight: 600;
  color: #303133;
}

.register-button {
  width: 100%;
  height: 50px;
  font-size: 16px;
  font-weight: 600;
  border-radius: 8px;
}

.login-link {
  text-align: center;
  color: #606266;
}

/* 协议内容样式 */
.agreement-content,
.privacy-content {
  max-height: 400px;
  overflow-y: auto;
  padding: 0 10px;
}

.agreement-content h3,
.privacy-content h3 {
  color: #303133;
  font-size: 1.1rem;
  margin: 20px 0 10px 0;
}

.agreement-content h3:first-child,
.privacy-content h3:first-child {
  margin-top: 0;
}

.agreement-content p,
.privacy-content p {
  color: #606266;
  line-height: 1.6;
  margin-bottom: 15px;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .register-page {
    padding: 10px;
  }
  
  .register-container {
    max-width: 400px;
  }
  
  .title {
    font-size: 2rem;
  }
  
  .register-button {
    height: 45px;
    font-size: 15px;
  }
}

@media (max-width: 480px) {
  .register-container {
    max-width: 350px;
  }
  
  .title {
    font-size: 1.8rem;
  }
  
  :deep(.el-form-item__label) {
    font-size: 14px;
  }
  
  :deep(.el-dialog) {
    width: 95% !important;
  }
}

/* Element Plus 表单样式调整 */
:deep(.el-form-item) {
  margin-bottom: 20px;
}

:deep(.el-form-item__label) {
  font-weight: 500;
  color: #303133;
}

:deep(.el-input__inner) {
  border-radius: 8px;
}

:deep(.el-checkbox__label) {
  font-size: 14px;
  line-height: 1.4;
}

/* 对话框样式调整 */
:deep(.el-dialog__header) {
  background-color: #f5f7fa;
  border-bottom: 1px solid #e4e7ed;
  padding: 15px 20px;
}

:deep(.el-dialog__body) {
  padding: 20px;
}
</style>
