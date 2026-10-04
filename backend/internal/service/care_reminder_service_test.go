package service

import (
	"errors"
	"io"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gbplantwiki/gbplantwiki/internal/model"
	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

func newReminderServiceMock(t *testing.T) (*CareReminderService, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewCareReminderService(db, repository.NewCareReminderRepository(db), repository.NewUserGardenRepository(db), logger)
	return svc, mock
}

func reminderRow(id, userID uint, status, frequency string, nextID uint) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "plant_species_id", "task_title", "remind_date", "frequency", "status", "next_reminder_id", "created_at"}).
		AddRow(id, userID, 4, "给月季补充缓释肥", time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), frequency, status, nextID, time.Now())
}

func TestNextRemindDate(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		from   time.Time
		freq   string
		want   time.Time
		wantOK bool
	}{
		{"daily catches up to today", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), model.FrequencyDaily, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), true},
		{"weekly rolls forward", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), model.FrequencyWeekly, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), true},
		{"monthly keeps day of month", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), model.FrequencyMonthly, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), true},
		{"yearly rolls forward", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), model.FrequencyYearly, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), true},
		{"future date advances one interval", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), model.FrequencyMonthly, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"empty frequency is one-off", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "", time.Time{}, false},
		{"unknown frequency is one-off", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "hourly", time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := nextRemindDate(c.from, c.freq, now)
			if ok != c.wantOK {
				t.Fatalf("nextRemindDate ok = %v, want %v", ok, c.wantOK)
			}
			if c.wantOK && !got.Equal(c.want) {
				t.Errorf("nextRemindDate = %s, want %s", got, c.want)
			}
		})
	}
}

// Completing a recurring reminder generates exactly one next reminder and
// transfers the user's garden bindings to it inside one transaction.
func TestCompleteGeneratesNextAndTransfersGarden(t *testing.T) {
	svc, mock := newReminderServiceMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(reminderRow(7, 1, model.ReminderPending, model.FrequencyWeekly, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `care_reminders`")).
		WillReturnResult(sqlmock.NewResult(100, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `user_gardens` SET `care_reminder_id`=?")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m, err := svc.UpdateStatus(1, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if m.Status != model.ReminderDone {
		t.Errorf("status = %s, want done", m.Status)
	}
	if m.NextReminderID != 100 {
		t.Errorf("next_reminder_id = %d, want 100", m.NextReminderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// Repeating a completion after the cycle was already continued persists
// nothing: the second submission (e.g. a double click or a second window)
// must not create another next reminder.
func TestCompleteIdempotentWhenCycleContinued(t *testing.T) {
	svc, mock := newReminderServiceMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(reminderRow(7, 1, model.ReminderDone, model.FrequencyWeekly, 100))
	mock.ExpectCommit()

	m, err := svc.UpdateStatus(1, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if m.NextReminderID != 100 {
		t.Errorf("next_reminder_id = %d, want 100", m.NextReminderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// A failed generation rolls everything back, so the client can retry with the
// original reminder id and still get exactly one next reminder.
func TestCompleteRetryWithSameReminderAfterFailure(t *testing.T) {
	svc, mock := newReminderServiceMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(reminderRow(7, 1, model.ReminderPending, model.FrequencyWeekly, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `care_reminders`")).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	if _, err := svc.UpdateStatus(1, 7, model.ReminderDone); err == nil {
		t.Fatal("expected error from failed generation")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(reminderRow(7, 1, model.ReminderPending, model.FrequencyWeekly, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `care_reminders`")).
		WillReturnResult(sqlmock.NewResult(100, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `user_gardens` SET `care_reminder_id`=?")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m, err := svc.UpdateStatus(1, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("retry UpdateStatus: %v", err)
	}
	if m.NextReminderID != 100 {
		t.Errorf("next_reminder_id = %d, want 100", m.NextReminderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// An old one-off reminder (no frequency) simply flips to done without
// generating a follow-up reminder.
func TestCompleteOneOffReminder(t *testing.T) {
	svc, mock := newReminderServiceMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(reminderRow(7, 1, model.ReminderPending, "", 0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `care_reminders` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m, err := svc.UpdateStatus(1, 7, model.ReminderDone)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if m.Status != model.ReminderDone || m.NextReminderID != 0 {
		t.Errorf("unexpected reminder: status=%s next=%d", m.Status, m.NextReminderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCompleteForbiddenForNonOwner(t *testing.T) {
	svc, mock := newReminderServiceMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(reminderRow(7, 2, model.ReminderPending, model.FrequencyWeekly, 0))
	mock.ExpectRollback()

	_, err := svc.UpdateStatus(1, 7, model.ReminderDone)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 403 {
		t.Fatalf("expected 403 AppError, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCompleteNotFound(t *testing.T) {
	svc, mock := newReminderServiceMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	_, err := svc.UpdateStatus(1, 999, model.ReminderDone)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 404 {
		t.Fatalf("expected 404 AppError, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
