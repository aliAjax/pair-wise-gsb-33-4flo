import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listReminders, updateReminderStatus, createReminder, renewReminder } from '@/api/reminder'
import type { CareReminder, ReminderCycleResult } from '@/types/api'

export const useReminderStore = defineStore('reminder', () => {
  const reminders = ref<CareReminder[]>([])

  async function load(status?: string) {
    reminders.value = await listReminders(status)
  }

  async function create(payload: { plant_species_id?: number; task_title: string; remind_date: string; frequency?: string }) {
    await createReminder(payload)
    await load()
  }

  async function setStatus(id: number, status: string): Promise<ReminderCycleResult> {
    const result = await updateReminderStatus(id, status)
    await load()
    return result
  }

  // Retry next-cycle generation for a completed reminder by its original id.
  async function renew(id: number): Promise<ReminderCycleResult> {
    const result = await renewReminder(id)
    await load()
    return result
  }

  return { reminders, load, create, setStatus, renew }
})
