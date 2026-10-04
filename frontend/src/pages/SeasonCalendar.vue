<template>
  <div class="page">
    <h1>季节养护日历</h1>
    <el-row :gutter="16">
      <el-col :xs="24" :md="14">
        <el-timeline>
          <el-timeline-item v-for="t in seasonTasks" :key="t.month" :timestamp="t.label" :type="t.month === currentMonth ? 'primary' : ''">
            {{ t.task }}
            <el-tag v-if="t.month === currentMonth" size="small" type="success">本月</el-tag>
          </el-timeline-item>
        </el-timeline>
      </el-col>
      <el-col :xs="24" :md="10">
        <el-card>
          <template #header>本月养护提醒</template>
          <el-form inline>
            <el-form-item label="任务"><el-input v-model="form.task_title" placeholder="如：给月季施肥" /></el-form-item>
            <el-form-item label="日期"><el-date-picker v-model="form.remind_date" type="date" value-format="YYYY-MM-DD" /></el-form-item>
            <el-form-item label="频率">
              <el-select v-model="form.frequency" placeholder="单次" clearable style="width: 110px">
                <el-option label="每日" value="daily" />
                <el-option label="每周" value="weekly" />
                <el-option label="每月" value="monthly" />
                <el-option label="每年" value="yearly" />
              </el-select>
            </el-form-item>
            <el-form-item><el-button type="primary" @click="create">创建提醒</el-button></el-form-item>
          </el-form>
          <ReminderList :reminders="reminders" :busy-id="busyId" @done="markDone" @renew="renew" @remove="remove" />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import ReminderList from '@/components/common/ReminderList.vue'
import { useReminderStore } from '@/stores/reminderStore'
import { getSeasonTasks } from '@/utils/season'
import type { CareReminder } from '@/types/api'

const store = useReminderStore()
const seasonTasks = getSeasonTasks()
const currentMonth = new Date().getMonth() + 1
const reminders = ref<CareReminder[]>([])
const busyId = ref<number | null>(null)
const form = reactive({ task_title: '', remind_date: '', frequency: '' })

onMounted(async () => {
  await store.load()
  reminders.value = store.reminders
})

async function create() {
  if (!form.task_title || !form.remind_date) {
    ElMessage.warning('请填写任务与日期')
    return
  }
  await store.create({ task_title: form.task_title, remind_date: form.remind_date, frequency: form.frequency || undefined })
  reminders.value = store.reminders
  form.task_title = ''
  form.remind_date = ''
  form.frequency = ''
  ElMessage.success('养护提醒已创建')
}
async function markDone(id: number) {
  if (busyId.value) return
  busyId.value = id
  try {
    const result = await store.setStatus(id, 'done')
    reminders.value = store.reminders
    if (result.next) {
      ElMessage.success(`已完成，并生成下一周期提醒（#${result.next.id}）`)
    } else {
      ElMessage.success('提醒已标记完成')
    }
  } finally {
    busyId.value = null
  }
}
async function renew(id: number) {
  if (busyId.value) return
  busyId.value = id
  try {
    const result = await store.renew(id)
    reminders.value = store.reminders
    if (result.next) {
      ElMessage.success(`已按原频率补齐下一周期提醒（#${result.next.id}）`)
    }
  } finally {
    busyId.value = null
  }
}
async function remove(id: number) {
  const { deleteReminder } = await import('@/api/reminder')
  await deleteReminder(id)
  await store.load()
  reminders.value = store.reminders
  ElMessage.success('已删除提醒')
}
</script>

<style scoped>
.page { max-width: 1200px; margin: 0 auto; }
</style>
