import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
import WebSocketPlugin from '@/services/websocket'
import { useUserStore } from '@/stores/userStore'

const app = createApp(App)
const pinia = createPinia()

// 安装插件
app.use(pinia)
app.use(router)
app.use(ElementPlus)
app.use(WebSocketPlugin)

// 注册所有图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

// 初始化用户状态
const userStore = useUserStore()
userStore.initUser()

app.mount('#app')
