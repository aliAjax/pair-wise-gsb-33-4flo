package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/gbplantwiki/gbplantwiki/internal/constants"
	"github.com/gbplantwiki/gbplantwiki/internal/dto"
	"github.com/gbplantwiki/gbplantwiki/internal/model"
	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

// CareReminderService implements care reminder state machine logic.
// The state machine (pending -> done / overdue) is intentionally mirrored in
// frontend button visibility, log templates, error codes and formatters.
//
// Completing a recurring reminder extends the care cycle: exactly one
// next-cycle reminder is generated for the same plant and every bound
// "my garden" entry is moved onto it. NextReminderID on the source reminder
// makes completion idempotent, so duplicate clicks or concurrent submissions
// land only once; Renew retries generation by the original reminder id.
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

// UpdateStatus transitions a reminder to a new status. Moving to "done"
// completes the current cycle and, for recurring reminders, generates exactly
// one next-cycle reminder and rebinds garden entries. The transition runs in
// one row-locked transaction, so repeated clicks and two concurrent windows
// produce only one next reminder; when one already exists the stored result is
// returned as-is.
func (s *CareReminderService) UpdateStatus(userID, id uint, status string) (*dto.ReminderCycleResult, error) {
	switch status {
	case model.ReminderDone:
		return s.complete(userID, id)
	case model.ReminderPending:
		m, err := s.reopen(userID, id)
		if err != nil {
			return nil, err
		}
		return &dto.ReminderCycleResult{Reminder: dto.NewCareReminderView(m)}, nil
	default:
		return nil, util.NewAppError(422, constants.CodeValidationError,
			fmt.Sprintf("CareReminder[id=%d] status=%s invalid transition", id, status))
	}
}

// Renew retries next-cycle generation for an already completed reminder by its
// original id. It is idempotent: reminders that already carry NextReminderID
// return the existing cycle, and legacy done reminders without a next id are
// filled according to their original frequency.
func (s *CareReminderService) Renew(userID, id uint) (*dto.ReminderCycleResult, error) {
	var result *dto.ReminderCycleResult
	err := s.db.Transaction(func(tx *gorm.DB) error {
		m, appErr := s.lockOwnedReminder(tx, userID, id)
		if appErr != nil {
			return appErr
		}
		if m.Status != model.ReminderDone {
			return util.NewAppError(409, constants.CodeConflict,
				fmt.Sprintf("CareReminder[id=%d] renew failed: status=%s not done", id, m.Status))
		}
		var inner error
		result, inner = s.ensureNextCycle(tx, m)
		return inner
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// complete marks the reminder done and extends the care cycle.
func (s *CareReminderService) complete(userID, id uint) (*dto.ReminderCycleResult, error) {
	var result *dto.ReminderCycleResult
	err := s.db.Transaction(func(tx *gorm.DB) error {
		m, appErr := s.lockOwnedReminder(tx, userID, id)
		if appErr != nil {
			return appErr
		}
		// Idempotent completion: a generated cycle means this reminder was
		// already completed (duplicate click or concurrent submission).
		if m.Status == model.ReminderDone {
			var inner error
			result, inner = s.ensureNextCycle(tx, m)
			return inner
		}
		m.Status = model.ReminderDone
		if err := tx.Save(m).Error; err != nil {
			return fmt.Errorf("care reminder status update: %w", err)
		}
		var inner error
		result, inner = s.ensureNextCycle(tx, m)
		return inner
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info(fmt.Sprintf(constants.LogReminderStatusChanged, id, model.ReminderDone), "id", id)
	return result, nil
}

// reopen moves a reminder back to pending/overdue without touching the cycle.
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

// ensureNextCycle generates the single next-cycle reminder for a done
// reminder and rebinds garden entries onto it. It is idempotent: when the
// reminder already has NextReminderID, or its frequency is not recurring,
// nothing is written.
func (s *CareReminderService) ensureNextCycle(tx *gorm.DB, m *model.CareReminder) (*dto.ReminderCycleResult, error) {
	result := &dto.ReminderCycleResult{Reminder: dto.NewCareReminderView(m)}
	if m.NextReminderID != 0 {
		next, err := s.repo.FindByIDForUpdate(tx, m.NextReminderID)
		if err == nil {
			result.Next = dto.NewCareReminderView(next)
			return result, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("care reminder next find: %w", err)
		}
		// The generated next reminder was deleted afterwards: drop the dangling
		// link and regenerate the cycle below.
		s.logger.Warn("care reminder next missing, regenerating cycle", "id", m.ID, "missing_next_id", m.NextReminderID)
	}
	nextDate, recurring := NextRemindDate(m.RemindDate, m.Frequency)
	if !recurring {
		s.logger.Info(fmt.Sprintf(constants.LogReminderRenewSkipped, m.ID, "not_recurring"), "id", m.ID)
		return result, nil
	}
	danglingID := m.NextReminderID
	next := &model.CareReminder{
		UserID:         m.UserID,
		PlantSpeciesID: m.PlantSpeciesID,
		TaskTitle:      m.TaskTitle,
		RemindDate:     nextDate,
		Frequency:      m.Frequency,
		Status:         model.ReminderPending,
	}
	if err := s.repo.CreateTx(tx, next); err != nil {
		return nil, fmt.Errorf("care reminder next create: %w", err)
	}
	m.NextReminderID = next.ID
	if err := tx.Save(m).Error; err != nil {
		return nil, fmt.Errorf("care reminder next link: %w", err)
	}
	rebound, err := s.gardenRepo.RebindReminderTx(tx, m.UserID, m.ID, next.ID)
	if err != nil {
		return nil, fmt.Errorf("care reminder garden rebind: %w", err)
	}
	// Move entries still pointing at the deleted previous next as well.
	if danglingID != 0 {
		n, err := s.gardenRepo.RebindReminderTx(tx, m.UserID, danglingID, next.ID)
		if err != nil {
			return nil, fmt.Errorf("care reminder garden rebind dangling: %w", err)
		}
		rebound += n
	}
	s.logger.Info(fmt.Sprintf(constants.LogReminderRenewed, m.ID, next.ID), "id", m.ID, "next_id", next.ID)
	if rebound > 0 {
		s.logger.Info(fmt.Sprintf(constants.LogReminderGardenRebound, m.ID, next.ID, rebound),
			"from_id", m.ID, "to_id", next.ID, "count", rebound)
	}
	result.Reminder = dto.NewCareReminderView(m)
	result.Next = dto.NewCareReminderView(next)
	result.Rebound = rebound
	return result, nil
}

// lockOwnedReminder loads a reminder with a row lock and enforces ownership.
func (s *CareReminderService) lockOwnedReminder(tx *gorm.DB, userID, id uint) (*model.CareReminder, error) {
	m, err := s.repo.FindByIDForUpdate(tx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(404, constants.CodeNotFound, fmt.Sprintf("CareReminder[id=%d] not found", id))
		}
		return nil, fmt.Errorf("care reminder find: %w", err)
	}
	if m.UserID != userID {
		return nil, util.NewAppError(403, constants.CodeForbidden,
			fmt.Sprintf("CareReminder[id=%d] status change failed: user_id=%d not owner", id, userID))
	}
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

// NextRemindDate shifts a reminder date by its frequency. The second return
// value is false for empty/unknown frequencies, i.e. one-off reminders.
func NextRemindDate(from time.Time, frequency string) (time.Time, bool) {
	switch frequency {
	case constants.FrequencyDaily:
		return from.AddDate(0, 0, 1), true
	case constants.FrequencyWeekly:
		return from.AddDate(0, 0, 7), true
	case constants.FrequencyMonthly:
		return from.AddDate(0, 1, 0), true
	case constants.FrequencyYearly:
		return from.AddDate(1, 0, 0), true
	default:
		return time.Time{}, false
	}
}
