package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
)

type cardsStore interface {
	ListISPCards(ctx context.Context, county string, limit int) ([]*models.ISPCard, error)
	ListTechnicianCards(ctx context.Context, county, town, skill string, limit int) ([]*models.TechnicianCard, error)
	ListCustomerWithJobsCards(ctx context.Context, county string, limit int) ([]*models.CustomerWithJobsCard, error)
	ListJobCards(ctx context.Context, county, town, serviceType string, limit int) ([]*models.JobCard, error)
	GetMarketplaceCardsOverview(ctx context.Context, county string, limit int) (*models.MarketplaceCardsOverview, error)
}

type CardsHandler struct {
	repo cardsStore
}

func NewCardsHandler() (*CardsHandler, error) {
	repo, err := postgres.NewCardsRepository()
	if err != nil {
		return nil, err
	}
	return &CardsHandler{repo: repo}, nil
}

func newCardsHandlerWithStore(store cardsStore) *CardsHandler {
	return &CardsHandler{repo: store}
}

func parseLimit(r *http.Request, defaultVal, maxVal int) int {
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= maxVal {
			return val
		}
	}
	return defaultVal
}

// ListISPs returns active ISPs formatted as e-commerce product cards.
func (h *CardsHandler) ListISPs(w http.ResponseWriter, r *http.Request) {
	county := strings.TrimSpace(r.URL.Query().Get("county"))
	limit := parseLimit(r, 20, 100)

	cards, err := h.repo.ListISPCards(r.Context(), county, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    cards,
	})
}

// ListTechnicians returns active technicians formatted as e-commerce cards with display names and identity labels.
func (h *CardsHandler) ListTechnicians(w http.ResponseWriter, r *http.Request) {
	county := strings.TrimSpace(r.URL.Query().Get("county"))
	town := strings.TrimSpace(r.URL.Query().Get("town"))
	skill := strings.TrimSpace(r.URL.Query().Get("skill"))
	limit := parseLimit(r, 20, 100)

	cards, err := h.repo.ListTechnicianCards(r.Context(), county, town, skill, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    cards,
	})
}

// ListCustomersWithJobs returns customers who currently have posted jobs.
func (h *CardsHandler) ListCustomersWithJobs(w http.ResponseWriter, r *http.Request) {
	county := strings.TrimSpace(r.URL.Query().Get("county"))
	limit := parseLimit(r, 20, 100)

	cards, err := h.repo.ListCustomerWithJobsCards(r.Context(), county, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    cards,
	})
}

// ListJobs returns open/available service jobs formatted as e-commerce cards with customer identity.
func (h *CardsHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	county := strings.TrimSpace(r.URL.Query().Get("county"))
	town := strings.TrimSpace(r.URL.Query().Get("town"))
	serviceType := strings.TrimSpace(r.URL.Query().Get("service_type"))
	limit := parseLimit(r, 20, 100)

	cards, err := h.repo.ListJobCards(r.Context(), county, town, serviceType, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    cards,
	})
}

// Overview returns an aggregated marketplace catalog containing all 4 card categories.
func (h *CardsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	county := strings.TrimSpace(r.URL.Query().Get("county"))
	limit := parseLimit(r, 10, 50)

	overview, err := h.repo.GetMarketplaceCardsOverview(r.Context(), county, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    overview,
	})
}
