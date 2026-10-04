<template>
  <el-table :data="reminders" stripe empty-text="暂无养护提醒" v-loading="loading">
    <el-table-column prop="task_title" label="任务" min-width="180" />
    <el-table-column label="提醒日期" width="120">
      <template #default="{ row }">{{ formatDate(row.remind_date) }}</template>
    </el-table-column>
    <el-table-column label="频率" width="100">
      <template #default="{ row }">{{ frequencyText(row.frequency) }}</template>
    </el-table-column>
    <el-table-column label="状态" width="110">
      <template #default="{ row }">
        <el-tag :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="下一周期" width="110">
      <template #default="{ row }">
        <el-tag v-if="row.next_reminder_id" size="small" type="success">已续作 #{{ row.next_reminder_id }}</el-tag>
        <span v-else-if="row.status === 'done' && isRecurring(row.frequency)" class="renew-tip">待续作</span>
        <span v-else>-</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="220">
      <template #default="{ row }">
        <el-button
          v-if="row.status !== 'done'"
          size="small"
          type="success"
          :loading="busyId === row.id"
          @click="$emit('done', row.id)"
        >完成</el-button>
        <el-button
          v-if="row.status === 'done' && isRecurring(row.frequency) && !row.next_reminder_id"
          size="small"
          type="primary"
          :loading="busyId === row.id"
          @click="$emit('renew', row.id)"
        >续作</el-button>
        <el-button size="small" type="danger" :disabled="busyId === row.id" @click="$emit('remove', row.id)">删除</el-button>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import type { CareReminder } from '@/types/api'
import { formatDate } from '@/utils/dateFormat'

defineProps<{ reminders: CareReminder[]; loading?: boolean; busyId?: number | null }>()
defineEmits<{
  (e: 'done', id: number): void
  (e: 'renew', id: number): void
  (e: 'remove', id: number): void
}>()

const RECURRING = new Set(['daily', 'weekly', 'monthly', 'yearly'])

function statusText(s: string): string {
  return s === 'pending' ? '待处理' : s === 'done' ? '已完成' : '已逾期'
}
function statusType(s: string): 'warning' | 'success' | 'danger' {
  return s === 'pending' ? 'warning' : s === 'done' ? 'success' : 'danger'
}
function frequencyText(f: string): string {
  const map: Record<string, string> = { daily: '每日', weekly: '每周', monthly: '每月', yearly: '每年' }
  return map[f] || f || '-'
}
function isRecurring(f: string): boolean {
  return RECURRING.has(f)
}
</script>

<style scoped>
.renew-tip { color: var(--el-color-warning); font-size: 12px; }
</style>
