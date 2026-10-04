package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/gbplantwiki/gbplantwiki/internal/constants"
	"github.com/gbplantwiki/gbplantwiki/internal/model"
	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

// CareReminderService implements care reminder state machine logic.
// The state machine (pending -> done / overdue) is intentionally mirrored in
// frontend button visibility, log templates, error codes and formatters.
// Completing a recurring reminder (one with a known frequency) continues its
// care cycle: exactly one next reminder is generated and the user's garden
// bindings are transferred to it, all inside one transaction so a failed
// generation can be retried with the original reminder id.
type CareReminderService struct {
	db         *gorm.DB
	repo       *repository.CareReminderRepository
	gardenRepo *repository.UserGardenRepository
	logger     *slog.Logger
}

// NewCareReminderService creates a CareReminderService.
func NewCareReminderService(db *gorm.DB, repo *repository.CareReminderRepository, gardenRepo *repository.UserGardenRepository, logger *slog.Logger) *CareReminderService {
	return &CareReminderService{db: db, repo: repo, gardenRepo: gardenRepo, logger: logger}
}

// Create adds a reminder for the current user.
func (s *CareReminderService) Create(userID uint, m *model.CareReminder) (*model.CareReminder, error) {
	m.UserID = userID
	if m.Status == "" {
		m.Status = model.ReminderPending
	}
	if m.RemindDate.IsZero() {
		return nil, util.NewAppError(422, constants.CodeValidationError,
			fmt.Sprintf("CareReminder[task_title=%s] create failed: remind_date required", m.TaskTitle))
	}
	if err := s.repo.Create(m); err != nil {
		s.logger.Error(fmt.Sprintf(constants.LogReminderCreateFailed, m.TaskTitle), "error", err)
		return nil, fmt.Errorf("care reminder create: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogReminderCreateSuccess, m.TaskTitle), "id", m.ID)
	return m, nil
}

// ListByUser lists reminders with status filter.
func (s *CareReminderService) ListByUser(userID uint, status string) ([]model.CareReminder, error) {
	if _, err := s.repo.MarkOverdue(userID); err != nil {
		s.logger.Warn("care reminder overdue mark failed", "error", err)
	}
	items, err := s.repo.ListByUser(userID, status)
	if err != nil {
		return nil, fmt.Errorf("care reminder list: %w", err)
	}
	return items, nil
}

// ListByMonth lists reminders within a calendar month.
func (s *CareReminderService) ListByMonth(userID uint, year, month int) ([]model.CareReminder, error) {
	items, err := s.repo.ListByMonth(userID, year, month)
	if err != nil {
		return nil, fmt.Errorf("care reminder month list: %w", err)
	}
	return items, nil
}

// UpdateStatus transitions a reminder to a new status.
func (s *CareReminderService) UpdateStatus(userID, id uint, status string) (*model.CareReminder, error) {
	switch status {
	case model.ReminderDone:
		return s.complete(userID, id)
	case model.ReminderPending:
		return s.reopen(userID, id)
	default:
		return nil, util.NewAppError(422, constants.CodeValidationError,
			fmt.Sprintf("CareReminder[id=%d] status=%s invalid transition", id, status))
	}
}

// complete marks a reminder done and, for recurring reminders, continues the
// care cycle. The reminder row is locked for the whole transaction so repeated
// completions or two windows submitting at once persist exactly one next
// reminder; any failure rolls everything back, leaving the original reminder
// untouched so the client can retry with the same reminder id.
func (s *CareReminderService) complete(userID, id uint) (*model.CareReminder, error) {
	var completed *model.CareReminder
	err := s.db.Transaction(func(tx *gorm.DB) error {
		m, err := s.repo.FindByIDForUpdateTx(tx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return util.NewAppError(404, constants.CodeNotFound, fmt.Sprintf("CareReminder[id=%d] not found", id))
			}
			return fmt.Errorf("care reminder complete find: %w", err)
		}
		if m.UserID != userID {
			return util.NewAppError(403, constants.CodeForbidden,
				fmt.Sprintf("CareReminder[id=%d] complete failed: user_id=%d not owner", id, userID))
		}
		_, recurring := frequencyStep(m.Frequency)
		if m.Status == model.ReminderDone && (!recurring || m.NextReminderID != 0) {
			// Repeated completion: the cycle was already continued (or is not
			// recurring at all), so nothing more may be persisted.
			completed = m
			return nil
		}
		m.Status = model.ReminderDone
		if recurring && m.NextReminderID == 0 {
			// Old reminders carry no next id yet; backfill the next occurrence
			// from the original frequency.
			nextDate, _ := nextRemindDate(m.RemindDate, m.Frequency, time.Now())
			next := &model.CareReminder{
				UserID:         m.UserID,
				PlantSpeciesID: m.PlantSpeciesID,
				TaskTitle:      m.TaskTitle,
				RemindDate:     nextDate,
				Frequency:      m.Frequency,
				Status:         model.ReminderPending,
			}
			if err := s.repo.CreateTx(tx, next); err != nil {
				return fmt.Errorf("care reminder cycle next create: %w", err)
			}
			m.NextReminderID = next.ID
			if _, err := s.gardenRepo.TransferReminderTx(tx, userID, m.ID, next.ID); err != nil {
				return fmt.Errorf("care reminder cycle garden transfer: %w", err)
			}
			s.logger.Info(fmt.Sprintf(constants.LogReminderCycleNextCreated, m.ID, next.ID), "id", m.ID, "next_id", next.ID)
		}
		if err := s.repo.UpdateTx(tx, m); err != nil {
			return fmt.Errorf("care reminder complete update: %w", err)
		}
		completed = m
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info(fmt.Sprintf(constants.LogReminderStatusChanged, id, completed.Status), "id", id)
	return completed, nil
}

// reopen moves a reminder back to pending (or overdue when its date passed).
func (s *CareReminderService) reopen(userID, id uint) (*model.CareReminder, error) {
	m, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(404, constants.CodeNotFound, fmt.Sprintf("CareReminder[id=%d] not found", id))
		}
		return nil, fmt.Errorf("care reminder status find: %w", err)
	}
	if m.UserID != userID {
		return nil, util.NewAppError(403, constants.CodeForbidden,
			fmt.Sprintf("CareReminder[id=%d] status change failed: user_id=%d not owner", id, userID))
	}
	if m.RemindDate.Before(time.Now()) {
		m.Status = model.ReminderOverdue
	} else {
		m.Status = model.ReminderPending
	}
	if err := s.repo.Update(m); err != nil {
		return nil, fmt.Errorf("care reminder status update: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogReminderStatusChanged, id, m.Status), "id", id)
	return m, nil
}

// Delete removes a reminder owned by the user.
func (s *CareReminderService) Delete(userID, id uint) error {
	m, err := s.repo.FindByID(id)
	if err != nil {
		return fmt.Errorf("care reminder delete find: %w", err)
	}
	if m.UserID != userID {
		return util.NewAppError(403, constants.CodeForbidden, fmt.Sprintf("CareReminder[id=%d] delete failed: not owner", id))
	}
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("care reminder delete: %w", err)
	}
	return nil
}

// frequencyStep returns the function advancing a date by one cycle of the
// given frequency. ok=false means the frequency is empty or unknown, i.e. the
// reminder is a one-off task without a follow-up cycle.
func frequencyStep(frequency string) (step func(time.Time) time.Time, ok bool) {
	switch frequency {
	case model.FrequencyDaily:
		return func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }, true
	case model.FrequencyWeekly:
		return func(t time.Time) time.Time { return t.AddDate(0, 0, 7) }, true
	case model.FrequencyMonthly:
		return func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }, true
	case model.FrequencyYearly:
		return func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }, true
	default:
		return nil, false
	}
}

// nextRemindDate computes the date of the next cycle occurrence: the original
// date advanced by whole frequency intervals until it reaches today, so an
// overdue reminder catches up to a future date while keeping its cadence.
func nextRemindDate(from time.Time, frequency string, now time.Time) (time.Time, bool) {
	step, ok := frequencyStep(frequency)
	if !ok {
		return time.Time{}, false
	}
	next := step(from)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for next.Before(today) {
		next = step(next)
	}
	return next, true
}
