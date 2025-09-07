<template>
  <div class="login-page">
    <div class="login-container">
      <div class="login-header">
        <h1 class="title">GPU计算平台</h1>
        <p class="subtitle">欢迎回来，请登录您的账户</p>
      </div>
      
      <el-card class="login-card" shadow="hover">
        <template #header>
          <span class="card-header">用户登录</span>
        </template>
        
        <el-form
          ref="loginFormRef"
          :model="loginForm"
          :rules="loginRules"
          label-width="0"
          size="large"
          @keyup.enter="handleLogin"
        >
          <el-form-item prop="username">
            <el-input
              v-model="loginForm.username"
              placeholder="用户名或邮箱"
              prefix-icon="User"
              clearable
            />
          </el-form-item>
          
          <el-form-item prop="password">
            <el-input
              v-model="loginForm.password"
              type="password"
              placeholder="密码"
              prefix-icon="Lock"
              show-password
              clearable
            />
          </el-form-item>
          
          <el-form-item>
            <div class="login-options">
              <el-checkbox v-model="loginForm.remember">
                记住我
              </el-checkbox>
              <el-link type="primary" :underline="false" @click="showForgotPassword">
                忘记密码？
              </el-link>
            </div>
          </el-form-item>
          
          <el-form-item>
            <el-button
              type="primary"
              size="large"
              :loading="loading"
              :disabled="loading"
              @click="handleLogin"
              class="login-button"
            >
              {{ loading ? '登录中...' : '登录' }}
            </el-button>
          </el-form-item>
          
          <el-form-item>
            <div class="register-link">
              还没有账户？
              <el-link type="primary" :underline="false" @click="goToRegister">
                立即注册
              </el-link>
            </div>
          </el-form-item>
        </el-form>
      </el-card>
      
      <!-- 演示账户信息 -->
      <el-card class="demo-card" shadow="never">
        <template #header>
          <span class="demo-header">演示账户</span>
        </template>
        <div class="demo-accounts">
          <div class="demo-account">
            <strong>管理员账户：</strong>
            <el-button text type="primary" @click="fillDemoAccount('admin')">
              admin / admin123
            </el-button>
          </div>
          <div class="demo-account">
            <strong>普通用户：</strong>
            <el-button text type="primary" @click="fillDemoAccount('user')">
              user / user123
            </el-button>
          </div>
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { useUserStore } from '@/stores/userStore'
import type { LoginForm } from '@/types'

const router = useRouter()
const userStore = useUserStore()

// 表单引用
const loginFormRef = ref<FormInstance>()

// 表单数据
const loginForm = reactive<LoginForm>({
  username: '',
  password: '',
  remember: false
})

// 加载状态
const loading = ref(false)

// 表单验证规则
const loginRules: FormRules = {
  username: [
    { required: true, message: '请输入用户名或邮箱', trigger: 'blur' },
    { min: 3, message: '用户名至少3个字符', trigger: 'blur' }
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, message: '密码至少6个字符', trigger: 'blur' }
  ]
}

// 处理登录
const handleLogin = async () => {
  if (!loginFormRef.value) return

  try {
    await loginFormRef.value.validate()
    loading.value = true

    const success = await userStore.login(loginForm)
    
    if (success) {
      // 登录成功，跳转到之前要访问的页面或首页
      const redirect = router.currentRoute.value.query.redirect as string
      await router.push(redirect || '/dashboard')
    }
  } catch (error) {
    console.error('Login validation failed:', error)
  } finally {
    loading.value = false
  }
}

// 跳转到注册页面
const goToRegister = () => {
  router.push('/register')
}

// 显示忘记密码（暂时用消息提示代替）
const showForgotPassword = () => {
  ElMessage.info('请联系管理员重置密码')
}

// 填充演示账户信息
const fillDemoAccount = (type: 'admin' | 'user') => {
  if (type === 'admin') {
    loginForm.username = 'admin'
    loginForm.password = 'admin123'
  } else {
    loginForm.username = 'user'
    loginForm.password = 'user123'
  }
}

// 组件挂载时检查是否已登录
onMounted(() => {
  if (userStore.isLoggedIn) {
    router.push('/dashboard')
  }
})
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.login-container {
  width: 100%;
  max-width: 400px;
}

.login-header {
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

.login-card {
  margin-bottom: 20px;
}

.card-header {
  font-size: 1.2rem;
  font-weight: 600;
  color: #303133;
}

.login-options {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  margin-top: 8px;
}

.login-button {
  width: 100%;
  height: 50px;
  font-size: 16px;
  font-weight: 600;
  border-radius: 8px;
}

.register-link {
  text-align: center;
  color: #606266;
}

/* 演示账户卡片 */
.demo-card {
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(255, 255, 255, 0.2);
}

.demo-card :deep(.el-card__header) {
  background: transparent;
  border-bottom: 1px solid rgba(255, 255, 255, 0.2);
}

.demo-header {
  color: white;
  font-weight: 600;
}

.demo-accounts {
  color: white;
}

.demo-account {
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.demo-account:last-child {
  margin-bottom: 0;
}

.demo-account strong {
  color: rgba(255, 255, 255, 0.9);
}

/* 响应式设计 */
@media (max-width: 768px) {
  .login-page {
    padding: 10px;
  }
  
  .login-container {
    max-width: 350px;
  }
  
  .title {
    font-size: 2rem;
  }
  
  .login-button {
    height: 45px;
    font-size: 15px;
  }
}

@media (max-width: 480px) {
  .title {
    font-size: 1.8rem;
  }
  
  .demo-account {
    flex-direction: column;
    align-items: flex-start;
    gap: 5px;
  }
}
</style>
