package model

import "time"

// ReminderStatus values.
const (
	ReminderPending = "pending"
	ReminderDone    = "done"
	ReminderOverdue = "overdue"
)

// CareReminder is a scheduled gardening task owned by a user.
type CareReminder struct {
	ID uint `gorm:"primaryKey" json:"id"`

	UserID         uint      `gorm:"index;not null" json:"user_id"`
	PlantSpeciesID uint      `gorm:"index" json:"plant_species_id"`
	TaskTitle      string    `gorm:"size:255;not null" json:"task_title"`
	RemindDate     time.Time `gorm:"type:date;index" json:"remind_date"`
	Frequency      string    `gorm:"size:32" json:"frequency"`
	Status         string    `gorm:"size:16;default:pending;index" json:"status"`

	// NextReminderID chains the reminder to the next cycle generated when this
	// one is completed. It is 0 until a next-cycle reminder exists, and also
	// guards idempotency for duplicate/renewal completion requests.
	NextReminderID uint `gorm:"index" json:"next_reminder_id"`

	CreatedAt time.Time `json:"created_at"`
}
