package referral

import (
	"context"
	"ispilolite/internal/models"
)

type Repository interface {
	Summary(context.Context, string) (string, float64, []*models.Referral, error)
}
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) Summary(ctx context.Context, userID string) (map[string]interface{}, error) {
	code, balance, items, err := s.repo.Summary(ctx, userID)
	if err != nil {
		return nil, err
	}
	qualified := 0
	for _, item := range items {
		if item.Status == "qualified" {
			qualified++
		}
	}
	return map[string]interface{}{"referral_code": code, "share_text": "Join Ispilo Lite with code " + code, "credit_balance": balance, "qualified_referrals": qualified, "referrals": items}, nil
}
