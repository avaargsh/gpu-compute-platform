<template>
  <div class="settings">
    <el-card shadow="hover">
      <template #header>
        <h3>系统设置</h3>
      </template>
      
      <div class="settings-content">
        <el-form label-width="120px">
          <el-form-item label="WebSocket地址">
            <el-input v-model="wsUrl" placeholder="ws://localhost:8000" />
          </el-form-item>
          
          <el-form-item label="API地址">
            <el-input v-model="apiUrl" placeholder="http://localhost:8000/api" />
          </el-form-item>
          
          <el-form-item label="自动刷新间隔">
            <el-input-number v-model="refreshInterval" :min="1" :max="60" />
            <span style="margin-left: 8px">秒</span>
          </el-form-item>
          
          <el-form-item>
            <el-button type="primary" @click="saveSettings">
              保存设置
            </el-button>
            <el-button @click="resetSettings">
              重置
            </el-button>
          </el-form-item>
        </el-form>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'

const wsUrl = ref('ws://localhost:8000')
const apiUrl = ref('http://localhost:8000/api')
const refreshInterval = ref(5)

const saveSettings = () => {
  // TODO: 实现保存设置逻辑
  ElMessage.success('设置保存成功')
}

const resetSettings = () => {
  wsUrl.value = 'ws://localhost:8000'
  apiUrl.value = 'http://localhost:8000/api'
  refreshInterval.value = 5
  ElMessage.info('设置已重置')
}
</script>

<style scoped>
.settings {
  padding: 24px;
  max-width: 800px;
  margin: 0 auto;
}

.settings-content {
  padding: 24px 0;
}
</style>
