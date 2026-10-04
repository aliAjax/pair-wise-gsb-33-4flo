import request from '@/utils/request'
import type { CareReminder, ReminderCycleResult } from '@/types/api'

export function listReminders(status?: string) {
  return request.get<never, CareReminder[]>('/reminders', { params: { status } })
}

export function listRemindersByMonth(year: number, month: number) {
  return request.get<never, CareReminder[]>('/reminders/calendar', { params: { year, month } })
}

export function createReminder(payload: { plant_species_id?: number; task_title: string; remind_date: string; frequency?: string }) {
  return request.post<never, CareReminder>('/reminders', payload)
}

export function updateReminderStatus(id: number, status: string) {
  return request.put<never, ReminderCycleResult>(`/reminders/${id}/status`, { status })
}

// Retry generating the next cycle for a completed reminder by its original id.
export function renewReminder(id: number) {
  return request.post<never, ReminderCycleResult>(`/reminders/${id}/renew`)
}

export function deleteReminder(id: number) {
  return request.delete<never, { deleted: boolean }>(`/reminders/${id}`)
}
