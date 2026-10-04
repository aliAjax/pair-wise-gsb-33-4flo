package service

import (
	"errors"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/gbplantwiki/gbplantwiki/internal/constants"
	"github.com/gbplantwiki/gbplantwiki/internal/model"
	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

var reminderCols = []string{"id", "user_id", "plant_species_id", "task_title", "remind_date", "frequency", "status", "next_reminder_id", "created_at"}

func newReminderService(t *testing.T) (*CareReminderService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := newServiceDB(t)
	svc := NewCareReminderService(db, repository.NewCareReminderRepository(db), repository.NewUserGardenRepository(db), newTestLogger())
	return svc, mock
}

func TestNextRemindDate(t *testing.T) {
	from := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local)
	cases := []struct {
		freq   string
		want   time.Time
		repeat bool
	}{
		{constants.FrequencyDaily, from.AddDate(0, 0, 1), true},
		{constants.FrequencyWeekly, from.AddDate(0, 0, 7), true},
		{constants.FrequencyMonthly, from.AddDate(0, 1, 0), true},
		{constants.FrequencyYearly, from.AddDate(1, 0, 0), true},
		{"", time.Time{}, false},
		{"once", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := NextRemindDate(from, c.freq)
		if ok != c.repeat || !got.Equal(c.want) {
			t.Errorf("NextRemindDate(%q) = (%v, %v), want (%v, %v)", c.freq, got, ok, c.want, c.repeat)
		}
	}
}

// TestCompleteGeneratesNextCycleAndRebinds covers the happy path: completing a
// recurring reminder creates exactly one pending next-cycle reminder, links it
// on the source reminder and rebinds garden entries in the same transaction.
func TestCompleteGeneratesNextCycleAndRebinds(t *testing.T) {
	svc, mock := newReminderService(t)
	remindDate := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(7, 42, 9, "给月季施肥", remindDate, "weekly", "pending", 0, remindDate))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `care_reminders`")).
		WillReturnResult(sqlmock.NewResult(8, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `user_gardens` SET")).
		WithArgs(uint64(8), uint64(42), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	result, err := svc.UpdateStatus(42, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if result.Reminder.Status != model.ReminderDone || result.Reminder.NextReminderID != 8 {
		t.Errorf("source reminder not completed/linked: %+v", result.Reminder)
	}
	if result.Next == nil || result.Next.ID != 8 || result.Next.Status != model.ReminderPending {
		t.Fatalf("next reminder wrong: %+v", result.Next)
	}
	if result.Next.RemindDate != remindDate.AddDate(0, 0, 7) {
		t.Errorf("next remind_date = %v, want +7d", result.Next.RemindDate)
	}
	if result.Next.PlantSpeciesID != 9 || result.Next.Frequency != "weekly" {
		t.Errorf("next reminder lost plant/frequency: %+v", result.Next)
	}
	if result.Rebound != 2 {
		t.Errorf("rebound = %d, want 2", result.Rebound)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sql expectations: %v", err)
	}
}

// TestCompleteOneOffSkipsNextCycle: an empty frequency ends the cycle without
// inserting another reminder.
func TestCompleteOneOffSkipsNextCycle(t *testing.T) {
	svc, mock := newReminderService(t)
	remindDate := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(7, 42, 9, "换盆", remindDate, "", "pending", 0, remindDate))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := svc.UpdateStatus(42, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if result.Next != nil || result.Rebound != 0 {
		t.Errorf("one-off task should not renew: %+v", result)
	}
}

// TestCompleteDuplicateLandsOnce: a second completion while the cycle already
// exists must return the stored next reminder and insert nothing.
func TestCompleteDuplicateLandsOnce(t *testing.T) {
	svc, mock := newReminderService(t)
	remindDate := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local)
	nextDate := remindDate.AddDate(0, 1, 0)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(7, 42, 9, "施肥", remindDate, "monthly", "done", 8, remindDate))
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(8), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(8, 42, 9, "施肥", nextDate, "monthly", "pending", 0, remindDate))
	mock.ExpectCommit()

	result, err := svc.UpdateStatus(42, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("duplicate UpdateStatus: %v", err)
	}
	if result.Next == nil || result.Next.ID != 8 {
		t.Errorf("expected existing next reminder #8, got %+v", result.Next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sql expectations: %v", err)
	}
}

// TestRenewFillsLegacyReminder: a legacy done reminder without NextReminderID
// is filled according to its original frequency.
func TestRenewFillsLegacyReminder(t *testing.T) {
	svc, mock := newReminderService(t)
	remindDate := time.Date(2026, 1, 31, 0, 0, 0, 0, time.Local)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(7, 42, 9, "施肥", remindDate, "monthly", "done", 0, remindDate))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `care_reminders`")).
		WillReturnResult(sqlmock.NewResult(10, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `user_gardens` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := svc.Renew(42, 7)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if result.Next == nil || result.Next.ID != 10 {
		t.Fatalf("expected renewed reminder #10, got %+v", result.Next)
	}
	if !result.Next.RemindDate.Equal(remindDate.AddDate(0, 1, 0)) {
		t.Errorf("next remind_date = %v, want %v", result.Next.RemindDate, remindDate.AddDate(0, 1, 0))
	}
	if result.Reminder.NextReminderID != 10 {
		t.Errorf("legacy reminder not linked to next id: %+v", result.Reminder)
	}
}

// TestRenewNonDoneConflict: renewal is only valid for completed reminders.
func TestRenewNonDoneConflict(t *testing.T) {
	svc, mock := newReminderService(t)
	remindDate := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(7, 42, 9, "施肥", remindDate, "monthly", "pending", 0, remindDate))
	mock.ExpectRollback()

	_, err := svc.Renew(42, 7)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %v", err)
	}
}

// TestCompleteForbidden: a reminder owned by another user cannot be completed.
func TestCompleteForbidden(t *testing.T) {
	svc, mock := newReminderService(t)
	remindDate := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).
			AddRow(7, 99, 9, "施肥", remindDate, "weekly", "pending", 0, remindDate))
	mock.ExpectRollback()

	_, err := svc.UpdateStatus(42, 7, model.ReminderDone)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
}
