package model

import "time"

// ReminderStatus values.
const (
	ReminderPending = "pending"
	ReminderDone    = "done"
	ReminderOverdue = "overdue"
)

// Reminder frequency values. A reminder with one of these frequencies is a
// recurring cycle task to be continued by the next occurrence on completion.
const (
	FrequencyDaily   = "daily"
	FrequencyWeekly  = "weekly"
	FrequencyMonthly = "monthly"
	FrequencyYearly  = "yearly"
)

// CareReminder is a scheduled gardening task owned by a user.
type CareReminder struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"index;not null" json:"user_id"`
	PlantSpeciesID uint      `gorm:"index" json:"plant_species_id"`
	TaskTitle      string    `gorm:"size:255;not null" json:"task_title"`
	RemindDate     time.Time `gorm:"type:date;index" json:"remind_date"`
	Frequency      string    `gorm:"size:32" json:"frequency"`
	Status         string    `gorm:"size:16;default:pending;index" json:"status"`
	// NextReminderID links to the reminder generated for the next cycle;
	// 0 means the next occurrence has not been generated yet.
	NextReminderID uint      `json:"next_reminder_id"`
	CreatedAt      time.Time `json:"created_at"`
}
