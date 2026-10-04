package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gbplantwiki/gbplantwiki/internal/constants"
	"github.com/gbplantwiki/gbplantwiki/internal/model"
	"github.com/gbplantwiki/gbplantwiki/internal/repository"
	"github.com/gbplantwiki/gbplantwiki/internal/util"
)

// UserGardenService implements "my garden" list logic.
type UserGardenService struct {
	repo         *repository.UserGardenRepository
	reminderRepo *repository.CareReminderRepository
	logger       *slog.Logger
}

// NewUserGardenService creates a UserGardenService.
func NewUserGardenService(repo *repository.UserGardenRepository, reminderRepo *repository.CareReminderRepository, logger *slog.Logger) *UserGardenService {
	return &UserGardenService{repo: repo, reminderRepo: reminderRepo, logger: logger}
}

// Add adds a plant to a user's garden.
func (s *UserGardenService) Add(userID uint, g *model.UserGarden) (*model.UserGarden, error) {
	g.UserID = userID
	if g.OwnedSince.IsZero() {
		g.OwnedSince = time.Now()
	}
	if err := s.repo.Create(g); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, util.NewAppError(409, constants.CodeConflict,
				fmt.Sprintf("UserGarden[user_id=%d plant_id=%d] add failed: already in garden", userID, g.PlantSpeciesID))
		}
		s.logger.Error(fmt.Sprintf(constants.LogGardenAddFailed, g.PlantSpeciesID, userID), "error", err)
		return nil, fmt.Errorf("user garden add: %w", err)
	}
	s.logger.Info(fmt.Sprintf(constants.LogGardenAddSuccess, g.PlantSpeciesID, userID), "id", g.ID)
	return g, nil
}

// List returns a user's garden items.
func (s *UserGardenService) List(userID uint) ([]model.UserGarden, error) {
	items, err := s.repo.ListByUser(userID)
	if err != nil {
		return nil, fmt.Errorf("user garden list: %w", err)
	}
	return items, nil
}

// Remove deletes a garden item owned by the user.
func (s *UserGardenService) Remove(userID, id uint) error {
	g, err := s.repo.Find(userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return util.NewAppError(404, constants.CodeNotFound, fmt.Sprintf("UserGarden[id=%d] not found", id))
		}
		return fmt.Errorf("user garden remove find: %w", err)
	}
	if err := s.repo.Delete(g.ID); err != nil {
		return fmt.Errorf("user garden remove: %w", err)
	}
	s.logger.Info("user garden item removed", "user_id", userID, "id", id)
	return nil
}

// BindReminder associates a care reminder with a garden item. Both sides must
// belong to the current user: a reminder or a garden item owned by someone
// else can never be bound.
func (s *UserGardenService) BindReminder(userID, gardenID, reminderID uint) (*model.UserGarden, error) {
	item, err := s.repo.FindByID(gardenID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(404, constants.CodeNotFound, fmt.Sprintf("UserGarden[id=%d] not found", gardenID))
		}
		return nil, fmt.Errorf("user garden bind find: %w", err)
	}
	if item.UserID != userID {
		return nil, util.NewAppError(403, constants.CodeForbidden, fmt.Sprintf("UserGarden[id=%d] bind failed: not owner", gardenID))
	}
	reminder, err := s.reminderRepo.FindByID(reminderID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(404, constants.CodeNotFound, fmt.Sprintf("CareReminder[id=%d] not found", reminderID))
		}
		return nil, fmt.Errorf("user garden bind reminder find: %w", err)
	}
	if reminder.UserID != userID {
		return nil, util.NewAppError(403, constants.CodeForbidden,
			fmt.Sprintf("UserGarden[id=%d] bind failed: CareReminder[id=%d] user_id=%d not owner", gardenID, reminderID, userID))
	}
	item.CareReminderID = reminderID
	if err := s.repo.Update(item); err != nil {
		return nil, fmt.Errorf("user garden bind update: %w", err)
	}
	return item, nil
}
