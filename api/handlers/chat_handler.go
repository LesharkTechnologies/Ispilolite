package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"ispilolite/api/dto"
	chatsvc "ispilolite/internal/services/chat"
)

type ChatHandler struct{ service *chatsvc.Service }

func NewChatHandler(service *chatsvc.Service) *ChatHandler { return &ChatHandler{service: service} }

func (h *ChatHandler) Send(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	var input struct {
		Body           string   `json:"body"`
		AttachmentURLs []string `json:"attachment_urls,omitempty"`
	}
	if jobID == "" || decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid message")
		return
	}
	message, err := h.service.Send(r.Context(), jobID, userIDFromContext(r.Context()), input.Body, input.AttachmentURLs)
	if err != nil {
		switch {
		case errors.Is(err, chatsvc.ErrInvalid):
			respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, chatsvc.ErrForbidden):
			respondWithErrorRaw(w, http.StatusForbidden, err.Error())
		default:
			respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: message})
}

func (h *ChatHandler) List(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			respondWithErrorRaw(w, http.StatusBadRequest, "before must be RFC3339")
			return
		}
		before = parsed
	}
	items, err := h.service.List(r.Context(), jobID, userIDFromContext(r.Context()), before, limit)
	if err != nil {
		if errors.Is(err, chatsvc.ErrForbidden) {
			respondWithErrorRaw(w, http.StatusForbidden, err.Error())
		} else {
			respondWithInternalError(w, err)
		}
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *ChatHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	if err := h.service.MarkRead(r.Context(), jobID, userIDFromContext(r.Context())); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "messages marked read"})
}
