<template>
  <div class="dashboard">
    <TaskList />
  </div>
</template>

<script setup lang="ts">
import TaskList from '@/components/tasks/TaskList.vue'
</script>

<style scoped>
.dashboard {
  padding: 24px;
}
</style>
