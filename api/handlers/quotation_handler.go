package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/repository/postgres"
	quotationsvc "ispilolite/internal/services/quotation"
)

type QuotationHandler struct{ service *quotationsvc.Service }

func NewQuotationHandler() (*QuotationHandler, error) {
	users, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	quotes, err := postgres.NewQuotationRepository()
	if err != nil {
		return nil, err
	}
	return &QuotationHandler{service: quotationsvc.NewService(quotes, users)}, nil
}

func (h *QuotationHandler) Finalize(w http.ResponseWriter, r *http.Request) {
	role := userRoleFromContext(r.Context())
	if role != "isp" && role != "technician" {
		respondWithError(w, 403, "only ISPs and technicians can create quotations")
		return
	}
	var req dto.FinalizeQuotationRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid quotation payload")
		return
	}
	q, err := h.service.Finalize(userIDFromContext(r.Context()), role, req)
	if err != nil {
		h.respondError(w, err)
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Message: "quotation finalized", Data: map[string]interface{}{"quotation": q, "public_url": "https://quotations.ispilo.co.ke/" + q.PublicCode}})
}
func (h *QuotationHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.List(userIDFromContext(r.Context()), userRoleFromContext(r.Context()), r.URL.Query().Get("status"), limit)
	if err != nil {
		respondWithError(w, 500, "failed to list quotations")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *QuotationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/quotations/")
	q, err := h.service.GetForUser(id, userIDFromContext(r.Context()), userRoleFromContext(r.Context()))
	if err != nil {
		h.respondError(w, err)
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: q})
}

func (h *QuotationHandler) DownloadDocument(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/documents/")
	id = strings.TrimSuffix(id, "/download")
	document, err := h.service.GetDocument(id, userIDFromContext(r.Context()), false)
	if err != nil {
		h.respondError(w, err)
		return
	}

	// Path containment: ensure the storage path is within the document root.
	docRoot := strings.TrimSpace(os.Getenv("DOCUMENT_STORAGE_ROOT"))
	if docRoot == "" {
		docRoot = "storage/documents"
	}
	if !isPathWithin(document.StoragePath, docRoot) {
		respondWithError(w, http.StatusForbidden, "document path is not accessible")
		return
	}

	file, err := os.Open(document.StoragePath)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "document file not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", document.ContentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+document.FileName+`"`)
	http.ServeContent(w, r, document.FileName, document.CreatedAt, file)
}

// isPathWithin returns true if childPath is resolved under parentDir.
func isPathWithin(childPath, parentDir string) bool {
	absChild, err := filepath.Abs(childPath)
	if err != nil {
		return false
	}
	absParent, err := filepath.Abs(parentDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absParent, absChild)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}
func (h *QuotationHandler) Respond(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/quotations/")
	id = strings.TrimSuffix(id, "/respond")
	var req dto.QuotationStatusRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid response")
		return
	}
	if err := h.service.Respond(userIDFromContext(r.Context()), id, req.Status); err != nil {
		h.respondError(w, err)
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "quotation status updated"})
}
func (h *QuotationHandler) Public(w http.ResponseWriter, r *http.Request) {
	code := pathParam(r.URL.Path, "/api/v1/public/quotations/")
	q, err := h.service.GetPublic(code)
	if err != nil {
		respondWithError(w, 404, "quotation not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: q})
}
func (h *QuotationHandler) Units(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.Units(userIDFromContext(r.Context()), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondWithError(w, 500, "failed to list units")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *QuotationHandler) CreateUnit(w http.ResponseWriter, r *http.Request) {
	role := userRoleFromContext(r.Context())
	if role != "isp" && role != "technician" {
		respondWithError(w, 403, "forbidden")
		return
	}
	var req dto.CustomUnitRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid unit")
		return
	}
	unit, err := h.service.CreateUnit(userIDFromContext(r.Context()), req)
	if err != nil {
		h.respondError(w, err)
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: unit})
}
func (h *QuotationHandler) respondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, quotationsvc.ErrForbidden):
		respondWithError(w, 403, err.Error())
	case errors.Is(err, quotationsvc.ErrNotFound):
		respondWithError(w, 404, err.Error())
	case errors.Is(err, quotationsvc.ErrInvalidQuotation):
		respondWithError(w, 400, err.Error())
	default:
		respondWithError(w, 500, "quotation operation failed")
	}
}
