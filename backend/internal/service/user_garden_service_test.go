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

	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

func newGardenServiceMock(t *testing.T) (*UserGardenService, sqlmock.Sqlmock) {
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
	svc := NewUserGardenService(repository.NewUserGardenRepository(db), repository.NewCareReminderRepository(db), logger)
	return svc, mock
}

func gardenRow(id, userID uint) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "plant_species_id", "nickname", "owned_since", "location", "care_reminder_id", "created_at"}).
		AddRow(id, userID, 4, "月季", time.Now(), "阳台", 0, time.Now())
}

func gardenReminderRow(id, userID uint) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "plant_species_id", "task_title", "remind_date", "frequency", "status", "next_reminder_id", "created_at"}).
		AddRow(id, userID, 4, "给月季补充缓释肥", time.Now(), "monthly", "pending", 0, time.Now())
}

// Binding succeeds only when both the garden item and the reminder belong to
// the current user.
func TestBindReminderSuccess(t *testing.T) {
	svc, mock := newGardenServiceMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens`")).
		WillReturnRows(gardenRow(5, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(gardenReminderRow(9, 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `user_gardens` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	g, err := svc.BindReminder(1, 5, 9)
	if err != nil {
		t.Fatalf("BindReminder: %v", err)
	}
	if g.CareReminderID != 9 {
		t.Errorf("care_reminder_id = %d, want 9", g.CareReminderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// A reminder owned by someone else must never be bound to my garden item.
func TestBindReminderForbiddenWhenReminderNotOwned(t *testing.T) {
	svc, mock := newGardenServiceMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens`")).
		WillReturnRows(gardenRow(5, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(gardenReminderRow(9, 2))

	_, err := svc.BindReminder(1, 5, 9)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 403 {
		t.Fatalf("expected 403 AppError, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// A garden item owned by someone else must never accept my reminder.
func TestBindReminderForbiddenWhenGardenNotOwned(t *testing.T) {
	svc, mock := newGardenServiceMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens`")).
		WillReturnRows(gardenRow(5, 2))

	_, err := svc.BindReminder(1, 5, 9)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 403 {
		t.Fatalf("expected 403 AppError, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBindReminderNotFound(t *testing.T) {
	svc, mock := newGardenServiceMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens`")).
		WillReturnRows(gardenRow(5, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders`")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := svc.BindReminder(1, 5, 999)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 404 {
		t.Fatalf("expected 404 AppError, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
