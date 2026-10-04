package service

import (
	"errors"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

var gardenCols = []string{"id", "user_id", "plant_species_id", "nickname", "owned_since", "location", "care_reminder_id", "created_at"}

func newGardenService(t *testing.T) (*UserGardenService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := newServiceDB(t)
	svc := NewUserGardenService(repository.NewUserGardenRepository(db), repository.NewCareReminderRepository(db), newTestLogger())
	return svc, mock
}

// TestBindReminderOwnReminder: a garden entry may bind the user's own reminder.
func TestBindReminderOwnReminder(t *testing.T) {
	svc, mock := newGardenService(t)
	now := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens` WHERE `user_gardens`.`id` = ? ORDER BY `user_gardens`.`id` LIMIT ?")).
		WithArgs(uint64(3), 1).
		WillReturnRows(sqlmock.NewRows(gardenCols).AddRow(3, 42, 9, "月月", now, "阳台", 0, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders` WHERE `care_reminders`.`id` = ? ORDER BY `care_reminders`.`id` LIMIT ?")).
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).AddRow(7, 42, 9, "施肥", now, "weekly", "pending", 0, now))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `user_gardens` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	item, err := svc.BindReminder(42, 3, 7)
	if err != nil {
		t.Fatalf("BindReminder: %v", err)
	}
	if item.CareReminderID != 7 {
		t.Errorf("care_reminder_id = %d, want 7", item.CareReminderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sql expectations: %v", err)
	}
}

// TestBindReminderForeignForbidden: a garden entry cannot bind another user's
// reminder.
func TestBindReminderForeignForbidden(t *testing.T) {
	svc, mock := newGardenService(t)
	now := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens` WHERE `user_gardens`.`id` = ? ORDER BY `user_gardens`.`id` LIMIT ?")).
		WithArgs(uint64(3), 1).
		WillReturnRows(sqlmock.NewRows(gardenCols).AddRow(3, 42, 9, "月月", now, "阳台", 0, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `care_reminders` WHERE `care_reminders`.`id` = ? ORDER BY `care_reminders`.`id` LIMIT ?")).
		WithArgs(uint64(7), 1).
		WillReturnRows(sqlmock.NewRows(reminderCols).AddRow(7, 99, 9, "施肥", now, "weekly", "pending", 0, now))

	_, err := svc.BindReminder(42, 3, 7)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sql expectations: %v", err)
	}
}

// TestBindReminderForeignGardenForbidden: only the garden entry owner may bind.
func TestBindReminderForeignGardenForbidden(t *testing.T) {
	svc, mock := newGardenService(t)
	now := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `user_gardens` WHERE `user_gardens`.`id` = ? ORDER BY `user_gardens`.`id` LIMIT ?")).
		WithArgs(uint64(3), 1).
		WillReturnRows(sqlmock.NewRows(gardenCols).AddRow(3, 99, 9, "月月", now, "阳台", 0, now))

	_, err := svc.BindReminder(42, 3, 7)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sql expectations: %v", err)
	}
}
