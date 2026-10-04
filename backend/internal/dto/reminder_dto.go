package dto

import (
	"time"

	"github.com/gbplantwiki/gbplantwiki/internal/model"
)

// ReminderCreateRequest is the payload for creating a care reminder.
type ReminderCreateRequest struct {
	PlantSpeciesID uint      `json:"plant_species_id"`
	TaskTitle      string    `json:"task_title" binding:"required,max=255"`
	RemindDate     time.Time `json:"remind_date" binding:"required"`
	Frequency      string    `json:"frequency" binding:"omitempty,max=32"`
}

// ReminderStatusRequest carries the new status for a reminder.
type ReminderStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// ReminderCycleResult is returned when a reminder is completed or renewed:
// Reminder is the (possibly updated) source reminder; Next is the generated
// next-cycle reminder, or nil for one-off tasks / non-recurring renewals;
// Rebound is the number of garden entries moved onto the next reminder.
type ReminderCycleResult struct {
	Reminder *CareReminderView `json:"reminder"`
	Next     *CareReminderView `json:"next"`
	Rebound  int64             `json:"rebound"`
}

// CareReminderView mirrors model.CareReminder for response payloads.
type CareReminderView struct {
	ID             uint      `json:"id"`
	UserID         uint      `json:"user_id"`
	PlantSpeciesID uint      `json:"plant_species_id"`
	TaskTitle      string    `json:"task_title"`
	RemindDate     time.Time `json:"remind_date"`
	Frequency      string    `json:"frequency"`
	Status         string    `json:"status"`
	NextReminderID uint      `json:"next_reminder_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// NewCareReminderView projects a reminder model into its response view.
func NewCareReminderView(m *model.CareReminder) *CareReminderView {
	if m == nil {
		return nil
	}
	return &CareReminderView{
		ID:             m.ID,
		UserID:         m.UserID,
		PlantSpeciesID: m.PlantSpeciesID,
		TaskTitle:      m.TaskTitle,
		RemindDate:     m.RemindDate,
		Frequency:      m.Frequency,
		Status:         m.Status,
		NextReminderID: m.NextReminderID,
		CreatedAt:      m.CreatedAt,
	}
}
