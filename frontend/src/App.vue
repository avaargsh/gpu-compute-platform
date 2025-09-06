<template>
  <div id="app">
    <el-config-provider :locale="zhCn">
      <Layout />
    </el-config-provider>
  </div>
</template>

<script setup lang="ts">
import { provide, onMounted } from 'vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import Layout from '@/components/layout/Layout.vue'
import { wsService } from '@/services/websocket'

// 提供 WebSocket 服务
provide('websocket', wsService)

onMounted(async () => {
  // 初始化 WebSocket 连接
  try {
    await wsService.connect()
  } catch (error) {
    console.warn('WebSocket connection failed:', error)
  }
})
</script>

<style>
#app {
  height: 100vh;
  width: 100vw;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html, body {
  height: 100%;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}

/* 自定义滚动条 */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

::-webkit-scrollbar-track {
  background: #f1f1f1;
}

::-webkit-scrollbar-thumb {
  background: #c1c1c1;
  border-radius: 3px;
}

::-webkit-scrollbar-thumb:hover {
  background: #a8a8a8;
}

/* Element Plus 样式调整 */
.el-button + .el-button {
  margin-left: 8px;
}

.el-card {
  border-radius: 8px;
}

.el-card__header {
  background-color: #fafbfc;
  border-bottom: 1px solid #ebeef5;
}

.el-table {
  font-size: 14px;
}

.el-table th {
  background-color: #fafafa;
  font-weight: 500;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .el-card {
    margin: 8px;
  }
  
  .el-table {
    font-size: 12px;
  }
  
  .el-button {
    padding: 8px 12px;
  }
}
</style>
