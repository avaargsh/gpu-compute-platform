<template>
  <TaskStatus :task-id="taskId" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import TaskStatus from '@/components/tasks/TaskStatus.vue'

const route = useRoute()
const taskId = computed(() => route.params.id as string)
</script>
