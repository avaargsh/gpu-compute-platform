<template>
  <div class="not-found">
    <el-result
      icon="warning"
      title="404"
      sub-title="抱歉，页面不存在"
    >
      <template #extra>
        <el-button type="primary" @click="goHome">
          返回首页
        </el-button>
        <el-button @click="goBack">
          返回上一页
        </el-button>
      </template>
    </el-result>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

const goHome = () => {
  router.push('/')
}

const goBack = () => {
  router.back()
}
</script>

<style scoped>
.not-found {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}
</style>
