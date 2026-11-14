package handlers

import (
	dto "ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/utils"
	"net/http"
	"strings"
	"time"
)

type VacancyHandler struct{ repo *postgres.VacancyRepository }

func NewVacancyHandler() (*VacancyHandler, error) {
	repo, err := postgres.NewVacancyRepository()
	if err != nil {
		return nil, err
	}
	return &VacancyHandler{repo}, nil
}
func (h *VacancyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.VacancyRequest
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.ServiceType) == "" {
		respondWithError(w, 400, "title and service_type are required")
		return
	}
	slots := req.Slots
	if slots <= 0 {
		slots = 1
	}
	var deadline *time.Time
	if req.DeadlineAt != "" {
		v, e := time.Parse(time.RFC3339, req.DeadlineAt)
		if e != nil {
			respondWithError(w, 400, "deadline_at must be RFC3339")
			return
		}
		deadline = &v
	}
	v := &models.ISPVacancy{ID: utils.GenerateID(), ISPID: userIDFromContext(r.Context()), Title: req.Title, Description: req.Description, ServiceType: req.ServiceType, Slots: slots, DeadlineAt: deadline, Status: "open", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := h.repo.Create(v); err != nil {
		respondWithError(w, 400, "failed to create vacancy")
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: v})
}
func (h *VacancyHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.List(userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 500, "failed to list vacancies")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *VacancyHandler) Close(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/vacancies/")
	if err := h.repo.Close(userIDFromContext(r.Context()), id); err != nil {
		respondWithError(w, 404, "vacancy not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "vacancy closed"})
}
func (h *VacancyHandler) Apply(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/vacancies/")
	id = strings.TrimSuffix(id, "/apply")
	var req dto.VacancyApplicationRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid application")
		return
	}
	if err := h.repo.Apply(id, userIDFromContext(r.Context()), req.Message); err != nil {
		respondWithError(w, 409, "application failed")
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Message: "application submitted"})
}
