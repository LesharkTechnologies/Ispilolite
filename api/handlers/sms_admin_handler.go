package handlers

import (
	"net/http"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/services/adminsms"
	"ispilolite/internal/services/notification"
)

type SMSAdminHandler struct {
	service  *adminsms.Service
	talksasa *notification.TalksasaSender
}

func NewSMSAdminHandler() (*SMSAdminHandler, error) {
	userRepo, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	sender := notification.NewSenderFromEnv()
	talksasa, _ := sender.(*notification.TalksasaSender)
	return &SMSAdminHandler{
		service:  adminsms.New(userRepo, sender),
		talksasa: talksasa,
	}, nil
}

func (h *SMSAdminHandler) Balance(w http.ResponseWriter, r *http.Request) {
	if h.talksasa == nil {
		respondWithError(w, http.StatusServiceUnavailable, "Talksasa SMS is not configured")
		return
	}
	bal, err := h.talksasa.GetBalance(r.Context())
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadGateway, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]string{"remaining_balance": bal}})
}

func (h *SMSAdminHandler) Profile(w http.ResponseWriter, r *http.Request) {
	if h.talksasa == nil {
		respondWithError(w, http.StatusServiceUnavailable, "Talksasa SMS is not configured")
		return
	}
	prof, err := h.talksasa.GetProfile(r.Context())
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadGateway, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: prof})
}

func (h *SMSAdminHandler) ContactGroups(w http.ResponseWriter, r *http.Request) {
	if h.talksasa == nil {
		respondWithError(w, http.StatusServiceUnavailable, "Talksasa SMS is not configured")
		return
	}
	data, err := h.talksasa.ListContactGroups(r.Context())
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadGateway, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: data})
}

func (h *SMSAdminHandler) StoreContact(w http.ResponseWriter, r *http.Request) {
	if h.talksasa == nil {
		respondWithError(w, http.StatusServiceUnavailable, "Talksasa SMS is not configured")
		return
	}
	groupID := pathParam(r.URL.Path, "/api/v1/admin/sms/contacts/")
	if groupID == "" {
		respondWithError(w, http.StatusBadRequest, "group_id is required")
		return
	}
	var req struct {
		Phone     string `json:"phone"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.Phone) == "" {
		respondWithError(w, http.StatusBadRequest, "phone is required")
		return
	}
	data, err := h.talksasa.StoreContact(r.Context(), groupID, req.Phone, req.FirstName, req.LastName)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: data})
}

func (h *SMSAdminHandler) Send(w http.ResponseWriter, r *http.Request) {
	var request struct {
		UserIDs []string `json:"user_ids"`
		Role    string   `json:"role"`
		All     bool     `json:"all"`
		Message string   `json:"message"`
	}
	if decodeJSON(w, r, &request) != nil || strings.TrimSpace(request.Message) == "" {
		respondWithError(w, http.StatusBadRequest, "message and a valid target are required")
		return
	}
	if !request.All && strings.TrimSpace(request.Role) == "" && len(request.UserIDs) == 0 {
		respondWithError(w, http.StatusBadRequest, "set all, role, or user_ids")
		return
	}
	if request.All {
		request.Role, request.UserIDs = "", nil
	}
	count, err := h.service.Send(r.Context(), request.Role, request.UserIDs, request.Message)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusAccepted, dto.APIResponse{Success: true, Message: "SMS accepted", Data: map[string]int{"recipients": count}})
}
