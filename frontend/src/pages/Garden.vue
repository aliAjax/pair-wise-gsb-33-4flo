<template>
  <div class="page">
    <h1>我的花园</h1>
    <el-row :gutter="16">
      <el-col :xs="24" :md="16">
        <el-card>
          <template #header>花园清单</template>
          <el-table :data="gardenItems" empty-text="花园还是空的，去品种库添加吧">
            <el-table-column label="植物">
              <template #default="{ row }">
                <span class="garden-name">{{ row.nickname || `植物 #${row.plant_species_id}` }}</span>
              </template>
            </el-table-column>
            <el-table-column label="位置" prop="location" />
            <el-table-column label="拥有时间">
              <template #default="{ row }">{{ formatDate(row.owned_since) }}</template>
            </el-table-column>
            <el-table-column label="养护提醒" min-width="220">
              <template #default="{ row }">
                <div class="bind-cell">
                  <el-select
                    :model-value="row.care_reminder_id || undefined"
                    size="small"
                    placeholder="绑定我的提醒"
                    style="width: 170px"
                    @change="(val: number | undefined) => bind(row.id, val)"
                  >
                    <el-option
                      v-for="r in reminders"
                      :key="r.id"
                      :label="`#${r.id} ${r.task_title}`"
                      :value="r.id"
                    />
                  </el-select>
                  <span v-if="row.care_reminder_id && !boundReminder(row.care_reminder_id)" class="bind-gone">提醒已删除</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="100">
              <template #default="{ row }">
                <el-button size="small" type="danger" @click="remove(row.id)">移除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
        <el-card class="block">
          <template #header>我的养护提醒</template>
          <ReminderList
            :reminders="reminders"
            :busy-id="busyId"
            @done="markDone"
            @renew="renew"
            @remove="removeReminder"
          />
        </el-card>
      </el-col>
      <el-col :xs="24" :md="8">
        <el-card>
          <template #header>我的收藏</template>
          <el-table :data="favorites" empty-text="暂无收藏">
            <el-table-column prop="target_type" label="类型" width="80">
              <template #default="{ row }">{{ FavoriteTargetTypeMap[row.target_type as FavoriteTargetType] }}</template>
            </el-table-column>
            <el-table-column prop="target_id" label="目标 ID" />
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import ReminderList from '@/components/common/ReminderList.vue'
import { listGardens, removeGarden, bindReminder } from '@/api/garden'
import { listFavorites } from '@/api/favorite'
import { listReminders, deleteReminder, updateReminderStatus, renewReminder } from '@/api/reminder'
import { FavoriteTargetTypeMap, type Favorite, type FavoriteTargetType } from '@/constants/favorite'
import { formatDate } from '@/utils/dateFormat'
import type { CareReminder, UserGarden } from '@/types/api'

const gardenItems = ref<UserGarden[]>([])
const favorites = ref<Favorite[]>([])
const reminders = ref<CareReminder[]>([])
const busyId = ref<number | null>(null)

onMounted(loadAll)

async function loadAll() {
  const [gardens, favs, rems] = await Promise.all([listGardens(), listFavorites(), listReminders()])
  gardenItems.value = gardens
  favorites.value = favs
  reminders.value = rems
}

function boundReminder(id: number): CareReminder | undefined {
  return reminders.value.find((r) => r.id === id)
}

async function bind(gardenId: number, reminderId: number | undefined) {
  if (reminderId) {
    await bindReminder(gardenId, reminderId)
    ElMessage.success('已绑定养护提醒')
  }
  await loadAll()
}
async function remove(id: number) {
  await removeGarden(id)
  await loadAll()
  ElMessage.success('已移除')
}
async function markDone(id: number) {
  if (busyId.value) return
  busyId.value = id
  try {
    const result = await updateReminderStatus(id, 'done')
    await loadAll()
    if (result.next) {
      ElMessage.success(`已完成，花园条目已转到下一周期提醒（#${result.next.id}），共 ${result.rebound} 条`)
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
    const result = await renewReminder(id)
    await loadAll()
    if (result.next) {
      ElMessage.success(`已按原频率补齐下一周期提醒（#${result.next.id}），花园绑定已转移 ${result.rebound} 条`)
    }
  } finally {
    busyId.value = null
  }
}
async function removeReminder(id: number) {
  await deleteReminder(id)
  await loadAll()
}
</script>

<style scoped>
.page { max-width: 1200px; margin: 0 auto; }
.block { margin-top: 16px; }
.garden-name { font-weight: 600; }
.bind-cell { display: flex; align-items: center; gap: 8px; }
.bind-gone { color: var(--el-color-danger); font-size: 12px; }
</style>
