package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/utils"
	appnotifications "ispilolite/pkg/notifications"
)

type jobPairingStore interface {
	CreatePairingSession(ctx context.Context, session *models.JobPairingSession) error
	GetPairingSession(ctx context.Context, jobID, key string) (*models.JobPairingSession, error)
	GetActivePairingSession(ctx context.Context, jobID string) (*models.JobPairingSession, error)
	ConfirmPairingSession(ctx context.Context, jobID, sessionID, customerID, notes string, rating int) (*models.JobPairingSession, error)
}

type jobPairingEscrowReleaser interface {
	EscrowForParticipant(ctx context.Context, jobID, userID string) (*models.Escrow, error)
	RequestRelease(ctx context.Context, escrowID, payerID, milestoneID string) (*models.EscrowMilestone, error)
}

type JobPairingHandler struct {
	repo       jobPairingStore
	jobRepo    repository.JobRequestRepository
	escrowRepo jobPairingEscrowReleaser
	notifier   *appnotifications.Service
}

func NewJobPairingHandler(jobRepo repository.JobRequestRepository, escrowRepo jobPairingEscrowReleaser, notifier *appnotifications.Service) (*JobPairingHandler, error) {
	repo, err := postgres.NewJobPairingRepository()
	if err != nil {
		return nil, err
	}
	return &JobPairingHandler{
		repo:       repo,
		jobRepo:    jobRepo,
		escrowRepo: escrowRepo,
		notifier:   notifier,
	}, nil
}

func NewJobPairingHandlerWithStore(repo jobPairingStore, jobRepo repository.JobRequestRepository, escrowRepo jobPairingEscrowReleaser, notifier *appnotifications.Service) *JobPairingHandler {
	return &JobPairingHandler{
		repo:       repo,
		jobRepo:    jobRepo,
		escrowRepo: escrowRepo,
		notifier:   notifier,
	}
}

func extractJobID(r *http.Request) string {
	jobID := pathParam(r.URL.Path, "/api/v1/my/jobs/")
	if jobID == "" {
		jobID = pathParam(r.URL.Path, "/api/v1/jobs/")
	}
	return jobID
}

// GeneratePairingQR generates a signed pairing token and QR payload for technician to present to customer.
func (h *JobPairingHandler) GeneratePairingQR(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobID(r)
	if jobID == "" {
		respondWithError(w, http.StatusBadRequest, "job ID is required in URL path")
		return
	}

	userID := userIDFromContext(r.Context())
	userRole := userRoleFromContext(r.Context())
	if userID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	job, err := h.jobRepo.GetJobRequestByID(jobID)
	if err != nil || job == nil {
		respondWithError(w, http.StatusNotFound, "job not found")
		return
	}

	// Caller must be assigned technician, assigned ISP, target technician/ISP, or admin
	isAssignedTech := job.AssignedTechnicianID == userID || job.TechnicianID == userID
	isAssignedISP := job.AssignedISPID == userID || job.TargetISPID == userID
	if !isAssignedTech && !isAssignedISP && userRole != "admin" {
		respondWithError(w, http.StatusForbidden, "only the assigned technician or ISP can generate the pairing QR")
		return
	}

	if job.Status == "completed" {
		respondWithError(w, http.StatusConflict, "job is already completed")
		return
	}
	if job.Status == "cancelled" || job.Status == "deleted" || job.Status == "lost" {
		respondWithError(w, http.StatusConflict, fmt.Sprintf("cannot pair a %s job", job.Status))
		return
	}

	var req dto.GenerateJobPairingQRRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = decodeJSON(w, r, &req)
	}

	expiresIn := req.ExpiresInSeconds
	if expiresIn <= 0 || expiresIn > 3600 {
		expiresIn = 600 // Default 10 minutes (600s)
	}

	amount := job.Budget
	currency := "KES"
	if h.escrowRepo != nil {
		if esc, err := h.escrowRepo.EscrowForParticipant(r.Context(), jobID, userID); err == nil && esc != nil {
			amount = esc.Amount
			currency = esc.Currency
		}
	}
	if req.Amount > 0 {
		amount = req.Amount
	}

	techID := job.AssignedTechnicianID
	if techID == "" {
		techID = job.TechnicianID
	}
	if techID == "" {
		techID = userID
	}

	nonce := utils.GenerateID()
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(expiresIn) * time.Second)
	expUnix := expiresAt.Unix()
	code := utils.GenerateVerificationCode()
	secret := utils.GetPairingSecret()
	signature := utils.SignJobPairing(secret, job.ID, job.CustomerID, techID, amount, currency, nonce, expUnix)
	deepLink := utils.FormatDeepLink(job.ID, nonce, techID, job.CustomerID, code, signature, amount, currency, expUnix)
	qrPayload := utils.FormatQRPayloadJSON(job.ID, nonce, techID, job.CustomerID, code, signature, amount, currency, expUnix)

	session := &models.JobPairingSession{
		ID:               utils.GenerateID(),
		JobID:            job.ID,
		TechnicianID:     techID,
		CustomerID:       job.CustomerID,
		Amount:           amount,
		Currency:         currency,
		Nonce:            nonce,
		VerificationCode: code,
		Signature:        signature,
		DeepLink:         deepLink,
		QRPayload:        qrPayload,
		Status:           "pending",
		ExpiresAt:        expiresAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := h.repo.CreatePairingSession(r.Context(), session); err != nil {
		respondWithInternalError(w, err)
		return
	}

	recordAudit(r, "job.pairing_qr_generated", "CREATE", "job_pairing", session.ID,
		fmt.Sprintf("Technician %s generated pairing QR for job %s (expires in %ds)", techID, job.ID, expiresIn), true,
		map[string]interface{}{"job_id": job.ID, "expires_in": expiresIn, "amount": amount, "currency": currency})

	resp := dto.JobPairingQRResponse{
		JobID:            job.ID,
		PairingID:        nonce,
		TechnicianID:     techID,
		CustomerID:       job.CustomerID,
		Amount:           amount,
		Currency:         currency,
		VerificationCode: code,
		ExpiresAt:        expiresAt,
		ExpiresAtUnix:    expUnix,
		ExpiresInSeconds: expiresIn,
		Signature:        signature,
		DeepLink:         deepLink,
		QRPayload:        qrPayload,
		Status:           "pending",
	}

	respondWithJSON(w, http.StatusCreated, dto.APIResponse{
		Success: true,
		Message: "Job pairing QR payload and signature generated successfully",
		Data:    resp,
	})
}

// ConfirmPairing confirms pairing when customer scans QR, completing job and triggering escrow release.
func (h *JobPairingHandler) ConfirmPairing(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobID(r)
	if jobID == "" {
		respondWithError(w, http.StatusBadRequest, "job ID is required in URL path")
		return
	}

	var req dto.ConfirmJobPairingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	userID := userIDFromContext(r.Context())
	userRole := userRoleFromContext(r.Context())
	if userID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	job, err := h.jobRepo.GetJobRequestByID(jobID)
	if err != nil || job == nil {
		respondWithError(w, http.StatusNotFound, "job not found")
		return
	}

	// Caller must be job customer or admin
	if job.CustomerID != userID && userRole != "admin" {
		respondWithError(w, http.StatusForbidden, "only the customer who requested this job can confirm pairing")
		return
	}

	lookupKey := strings.TrimSpace(req.PairingID)
	if lookupKey == "" {
		lookupKey = strings.TrimSpace(req.VerificationCode)
	}
	if lookupKey == "" {
		respondWithError(w, http.StatusBadRequest, "pairing_id or verification_code is required")
		return
	}

	session, err := h.repo.GetPairingSession(r.Context(), job.ID, lookupKey)
	if err != nil || session == nil {
		respondWithError(w, http.StatusNotFound, "pairing session not found")
		return
	}

	if session.Status == "confirmed" {
		respondWithError(w, http.StatusConflict, "pairing session has already been confirmed")
		return
	}
	if session.Status == "expired" || time.Now().UTC().After(session.ExpiresAt) {
		respondWithError(w, http.StatusGone, "pairing session has expired; please ask the technician to refresh the QR code")
		return
	}

	// Signature verification: if signature is sent, verify cryptographically
	if req.Signature != "" {
		secret := utils.GetPairingSecret()
		valid := utils.VerifyJobPairing(secret, job.ID, session.CustomerID, session.TechnicianID, session.Amount, session.Currency, session.Nonce, session.ExpiresAt.Unix(), req.Signature)
		if !valid {
			respondWithError(w, http.StatusBadRequest, "invalid cryptographic signature; QR code may be tampered or expired")
			return
		}
	} else if req.VerificationCode != "" {
		// Fallback manual 6-digit code verification
		if req.VerificationCode != session.VerificationCode {
			respondWithError(w, http.StatusBadRequest, "incorrect verification code")
			return
		}
	} else {
		respondWithError(w, http.StatusBadRequest, "signature or verification_code is required to confirm pairing")
		return
	}

	confirmedSession, err := h.repo.ConfirmPairingSession(r.Context(), job.ID, session.ID, userID, req.Notes, req.Rating)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}

	// Escrow release trigger
	escrowReleased := false
	var releasedAmount float64
	if h.escrowRepo != nil {
		if esc, err := h.escrowRepo.EscrowForParticipant(r.Context(), job.ID, userID); err == nil && esc != nil && esc.Status == "held" {
			if _, err := h.escrowRepo.RequestRelease(r.Context(), esc.ID, userID, ""); err == nil {
				escrowReleased = true
				releasedAmount = esc.Amount
			}
		}
	}

	// In-app notification to technician
	if h.notifier != nil {
		_ = h.notifier.SendInApp(session.TechnicianID, "job_pairing_confirmed", "Job Confirmed & Escrow Released!",
			fmt.Sprintf("Customer has signed off on job %s via QR scan. Funds of %.2f %s have been released.", job.ID, session.Amount, session.Currency),
			map[string]interface{}{"job_id": job.ID, "amount": session.Amount, "currency": session.Currency, "escrow_released": escrowReleased})
	}

	recordAudit(r, "job.pairing_confirmed", "UPDATE", "job_pairing", session.ID,
		fmt.Sprintf("Customer %s confirmed QR pairing for job %s (escrow released: %v)", userID, job.ID, escrowReleased), true,
		map[string]interface{}{"job_id": job.ID, "escrow_released": escrowReleased, "amount": session.Amount, "rating": req.Rating})

	confirmedAt := time.Now().UTC()
	if confirmedSession != nil && confirmedSession.ConfirmedAt != nil {
		confirmedAt = *confirmedSession.ConfirmedAt
	}

	resp := dto.ConfirmJobPairingResponse{
		JobID:          job.ID,
		PairingID:      session.Nonce,
		Status:         "confirmed",
		ConfirmedAt:    confirmedAt,
		JobStatus:      "completed",
		EscrowReleased: escrowReleased,
		ReleasedAmount: releasedAmount,
		Currency:       session.Currency,
		Message:        "Job pairing confirmed and handover signed off successfully",
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: "Job pairing confirmed and handover signed off successfully",
		Data:    resp,
	})
}

// GetPairingStatus checks the status of an active or recent pairing session.
func (h *JobPairingHandler) GetPairingStatus(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobID(r)
	if jobID == "" {
		respondWithError(w, http.StatusBadRequest, "job ID is required in URL path")
		return
	}

	userID := userIDFromContext(r.Context())
	userRole := userRoleFromContext(r.Context())
	if userID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	job, err := h.jobRepo.GetJobRequestByID(jobID)
	if err != nil || job == nil {
		respondWithError(w, http.StatusNotFound, "job not found")
		return
	}

	// Participant check
	isParticipant := job.CustomerID == userID ||
		job.AssignedTechnicianID == userID ||
		job.TechnicianID == userID ||
		job.AssignedISPID == userID ||
		job.TargetISPID == userID ||
		userRole == "admin"
	if !isParticipant {
		respondWithError(w, http.StatusForbidden, "you do not have access to this job's pairing status")
		return
	}

	session, err := h.repo.GetActivePairingSession(r.Context(), job.ID)
	if err != nil || session == nil {
		respondWithError(w, http.StatusNotFound, "no pairing session found for this job")
		return
	}

	isExpired := session.Status == "expired" || (session.Status == "pending" && time.Now().UTC().After(session.ExpiresAt))
	status := session.Status
	if isExpired && status == "pending" {
		status = "expired"
	}

	resp := dto.JobPairingStatusResponse{
		JobID:          job.ID,
		PairingID:      session.Nonce,
		Status:         status,
		ExpiresAt:      session.ExpiresAt,
		IsExpired:      isExpired,
		ConfirmedAt:    session.ConfirmedAt,
		ConfirmedBy:    session.ConfirmedBy,
		JobStatus:      job.Status,
		EscrowReleased: session.Status == "confirmed",
		Amount:         session.Amount,
		Currency:       session.Currency,
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    resp,
	})
}
