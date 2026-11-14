package earnings

import (
	"context"
	"ispilolite/internal/models"
	"time"
)

type Repository interface {
	Dashboard(context.Context, string, time.Time, string, string) (*models.EarningsDashboard, error)
}
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) Dashboard(ctx context.Context, technicianID, period string) (*models.EarningsDashboard, error) {
	now := time.Now().UTC()
	from, bucket := now.AddDate(0, 0, -30), "day"
	switch period {
	case "week":
		from = now.AddDate(0, 0, -7)
	case "year":
		from = now.AddDate(-1, 0, 0)
		bucket = "month"
	case "month", "":
		period = "month"
	default:
		period = "month"
	}
	return s.repo.Dashboard(ctx, technicianID, from, bucket, period)
}
