package user

import (
	"ispilolite/internal/models"
	"ispilolite/internal/repository"
)

type UserService struct{ repo repository.UserRepository }

func NewUserService(repo repository.UserRepository) *UserService   { return &UserService{repo: repo} }
func (s *UserService) GetUserByID(id string) (*models.User, error) { return s.repo.GetUserByID(id) }
func (s *UserService) UpdateUser(user *models.User) error          { return s.repo.UpdateUser(user) }
func (s *UserService) SetDisplayName(id, displayName string) error { return s.repo.SetDisplayName(id, displayName) }
func (s *UserService) ChangeAccountType(id, targetRole string, bypassCooldown bool) error {
	return s.repo.ChangeAccountType(id, targetRole, bypassCooldown)
}
func (s *UserService) SetUserStatus(id, status string) error {
	return s.repo.SetUserStatus(id, status)
}
func (s *UserService) SetISPTechnicianStatus(ispID, technicianID, status string) error {
	return s.repo.SetISPTechnicianStatus(ispID, technicianID, status)
}
func (s *UserService) GetTechniciansByISPID(id string) ([]*models.User, error) {
	return s.repo.GetTechniciansByISPID(id)
}
func (s *UserService) GetUsersByStatus(status string) ([]*models.User, error) {
	return s.repo.GetUsersByStatus(status)
}
func (s *UserService) RequestDeleteUser(id, status string) error {
	return s.repo.RequestDeleteUser(id, status)
}
func (s *UserService) SanitizeAndDeleteUser(id string) error { return s.repo.SanitizeAndDeleteUser(id) }
func (s *UserService) PurgeExpiredUsers() (int64, error)     { return s.repo.PurgeExpiredUsers() }
