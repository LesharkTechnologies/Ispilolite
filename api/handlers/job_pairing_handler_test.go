package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/utils"
)

type fakePairingStore struct {
	sessions map[string]*models.JobPairingSession
}

func newFakePairingStore() *fakePairingStore {
	return &fakePairingStore{sessions: make(map[string]*models.JobPairingSession)}
}

func (f *fakePairingStore) CreatePairingSession(ctx context.Context, session *models.JobPairingSession) error {
	f.sessions[session.ID] = session
	f.sessions[session.Nonce] = session
	if session.VerificationCode != "" {
		f.sessions[session.JobID+":"+session.VerificationCode] = session
	}
	return nil
}

func (f *fakePairingStore) GetPairingSession(ctx context.Context, jobID, key string) (*models.JobPairingSession, error) {
	if s, ok := f.sessions[key]; ok && s.JobID == jobID {
		return s, nil
	}
	if s, ok := f.sessions[jobID+":"+key]; ok {
		return s, nil
	}
	for _, s := range f.sessions {
		if s.JobID == jobID && (s.Nonce == key || s.ID == key || s.VerificationCode == key) {
			return s, nil
		}
	}
	return nil, nil
}

func (f *fakePairingStore) GetActivePairingSession(ctx context.Context, jobID string) (*models.JobPairingSession, error) {
	var latest *models.JobPairingSession
	for _, s := range f.sessions {
		if s.JobID == jobID {
			if latest == nil || s.CreatedAt.After(latest.CreatedAt) {
				latest = s
			}
		}
	}
	return latest, nil
}

func (f *fakePairingStore) ConfirmPairingSession(ctx context.Context, jobID, sessionID, customerID, notes string, rating int) (*models.JobPairingSession, error) {
	s, ok := f.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	now := time.Now().UTC()
	s.Status = "confirmed"
	s.ConfirmedAt = &now
	s.ConfirmedBy = customerID
	s.Notes = notes
	if rating >= 1 && rating <= 5 {
		s.Rating = &rating
	}
	return s, nil
}

type fakeJobRequestRepoForPairing struct {
	jobs map[string]*models.JobRequest
}

func newFakeJobRepo() *fakeJobRequestRepoForPairing {
	return &fakeJobRequestRepoForPairing{jobs: make(map[string]*models.JobRequest)}
}

func (r *fakeJobRequestRepoForPairing) CreateJobRequest(req *models.JobRequest) error {
	r.jobs[req.ID] = req
	return nil
}
func (r *fakeJobRequestRepoForPairing) GetJobRequestByID(id string) (*models.JobRequest, error) {
	return r.jobs[id], nil
}
func (r *fakeJobRequestRepoForPairing) ListForCustomer(customerID string, status string) ([]*models.JobRequest, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) ListForTechnician(technicianID string, status string) ([]*models.JobRequest, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) ListForISP(ispID string, status string) ([]*models.JobRequest, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) GetForParticipant(requestID, participantID string) (*models.JobRequest, error) {
	return r.jobs[requestID], nil
}
func (r *fakeJobRequestRepoForPairing) ListAvailable(county, town, serviceType string) ([]*models.JobRequest, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) ApplyToJob(application *models.JobApplication) error {
	return nil
}
func (r *fakeJobRequestRepoForPairing) ListApplications(requestID, customerID string) ([]*models.JobApplication, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) AssignApplication(requestID, customerID, applicationID string) (*models.JobRequest, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) SetAvailability(requestID, customerID string, available bool) error {
	return nil
}
func (r *fakeJobRequestRepoForPairing) DeleteJobRequest(requestID, customerID string) error {
	return nil
}
func (r *fakeJobRequestRepoForPairing) UpdateJobStatus(requestID, actorID, status string) error {
	if j := r.jobs[requestID]; j != nil {
		j.Status = status
	}
	return nil
}
func (r *fakeJobRequestRepoForPairing) AssignTechnician(requestID, ispID, technicianID string) (*models.JobRequest, error) {
	return nil, nil
}
func (r *fakeJobRequestRepoForPairing) TerminateJob(requestID, actorID, reason string, endedAt time.Time) error {
	return nil
}

type fakeEscrowRepoForPairing struct {
	escrows  map[string]*models.Escrow
	released map[string]bool
}

func newFakeEscrowRepo() *fakeEscrowRepoForPairing {
	return &fakeEscrowRepoForPairing{
		escrows:  make(map[string]*models.Escrow),
		released: make(map[string]bool),
	}
}

func (e *fakeEscrowRepoForPairing) EscrowForParticipant(ctx context.Context, jobID, userID string) (*models.Escrow, error) {
	return e.escrows[jobID], nil
}

func (e *fakeEscrowRepoForPairing) RequestRelease(ctx context.Context, escrowID, payerID, milestoneID string) (*models.EscrowMilestone, error) {
	e.released[escrowID] = true
	return &models.EscrowMilestone{
		ID:     "m-1",
		Amount: 2500,
		Status: "release_pending",
	}, nil
}

func setupTestPairingEnvironment() (*JobPairingHandler, *fakePairingStore, *fakeJobRequestRepoForPairing, *fakeEscrowRepoForPairing) {
	pairingStore := newFakePairingStore()
	jobRepo := newFakeJobRepo()
	escrowRepo := newFakeEscrowRepo()

	jobRepo.jobs["job-101"] = &models.JobRequest{
		ID:                   "job-101",
		CustomerID:           "cust-1",
		AssignedTechnicianID: "tech-1",
		Budget:               2500.0,
		Status:               "in_progress",
	}

	escrowRepo.escrows["job-101"] = &models.Escrow{
		ID:           "escrow-101",
		JobRequestID: "job-101",
		PayerID:      "cust-1",
		PayeeID:      "tech-1",
		Amount:       2500.0,
		Currency:     "KES",
		Status:       "held",
	}

	handler := NewJobPairingHandlerWithStore(pairingStore, jobRepo, escrowRepo, nil)
	return handler, pairingStore, jobRepo, escrowRepo
}

func TestJobPairing_GenerateQR_Success(t *testing.T) {
	handler, _, _, _ := setupTestPairingEnvironment()

	reqBody := `{"expires_in_seconds": 600}`
	req := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", bytes.NewBufferString(reqBody))
	req = req.WithContext(context.WithValue(req.Context(), "userID", "tech-1"))
	req = req.WithContext(context.WithValue(req.Context(), "userRole", "technician"))
	w := httptest.NewRecorder()

	handler.GeneratePairingQR(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got: %d, body: %s", w.Code, w.Body.String())
	}

	var res struct {
		Success bool                     `json:"success"`
		Data    dto.JobPairingQRResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected success=true")
	}
	if res.Data.JobID != "job-101" || res.Data.TechnicianID != "tech-1" || res.Data.CustomerID != "cust-1" {
		t.Fatalf("job or participant mismatch: %+v", res.Data)
	}
	if res.Data.Amount != 2500.0 || res.Data.Currency != "KES" {
		t.Fatalf("amount/currency mismatch: %+v", res.Data)
	}
	if res.Data.Signature == "" || res.Data.PairingID == "" || res.Data.DeepLink == "" || res.Data.QRPayload == "" {
		t.Fatalf("missing required payload fields: %+v", res.Data)
	}

	// Verify crypto signature validity
	secret := utils.GetPairingSecret()
	if !utils.VerifyJobPairing(secret, res.Data.JobID, res.Data.CustomerID, res.Data.TechnicianID, res.Data.Amount, res.Data.Currency, res.Data.PairingID, res.Data.ExpiresAtUnix, res.Data.Signature) {
		t.Fatalf("generated signature failed verification")
	}
}

func TestJobPairing_GenerateQR_ForbiddenForUnassigned(t *testing.T) {
	handler, _, _, _ := setupTestPairingEnvironment()

	req := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", nil)
	req = req.WithContext(context.WithValue(req.Context(), "userID", "wrong-tech-999"))
	req = req.WithContext(context.WithValue(req.Context(), "userRole", "technician"))
	w := httptest.NewRecorder()

	handler.GeneratePairingQR(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 Forbidden, got: %d", w.Code)
	}
}

func TestJobPairing_GenerateQR_CompletedJob_Conflict(t *testing.T) {
	handler, _, jobRepo, _ := setupTestPairingEnvironment()
	jobRepo.jobs["job-101"].Status = "completed"

	req := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", nil)
	req = req.WithContext(context.WithValue(req.Context(), "userID", "tech-1"))
	req = req.WithContext(context.WithValue(req.Context(), "userRole", "technician"))
	w := httptest.NewRecorder()

	handler.GeneratePairingQR(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409 Conflict for completed job, got: %d", w.Code)
	}
}

func TestJobPairing_ConfirmPairing_Success(t *testing.T) {
	handler, _, _, escrowRepo := setupTestPairingEnvironment()

	// 1. Tech generates QR
	genReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", nil)
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userID", "tech-1"))
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userRole", "technician"))
	genW := httptest.NewRecorder()
	handler.GeneratePairingQR(genW, genReq)

	var genRes struct {
		Data dto.JobPairingQRResponse `json:"data"`
	}
	_ = json.Unmarshal(genW.Body.Bytes(), &genRes)

	// 2. Customer scans QR and calls confirm
	confirmPayload := dto.ConfirmJobPairingRequest{
		PairingID: genRes.Data.PairingID,
		Signature: genRes.Data.Signature,
		ExpiresAt: genRes.Data.ExpiresAtUnix,
		Notes:     "Fiber installation tested and speeds verified",
		Rating:    5,
	}
	bodyBytes, _ := json.Marshal(confirmPayload)
	confirmReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/confirm-pairing", bytes.NewBuffer(bodyBytes))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userID", "cust-1"))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userRole", "customer"))
	confirmW := httptest.NewRecorder()

	handler.ConfirmPairing(confirmW, confirmReq)

	if confirmW.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got: %d, body: %s", confirmW.Code, confirmW.Body.String())
	}

	var confirmRes struct {
		Success bool                          `json:"success"`
		Data    dto.ConfirmJobPairingResponse `json:"data"`
	}
	_ = json.Unmarshal(confirmW.Body.Bytes(), &confirmRes)

	if !confirmRes.Success {
		t.Fatalf("expected confirm success=true")
	}
	if confirmRes.Data.Status != "confirmed" || confirmRes.Data.JobStatus != "completed" {
		t.Fatalf("unexpected confirmation state: %+v", confirmRes.Data)
	}
	if !confirmRes.Data.EscrowReleased {
		t.Fatalf("expected escrow_released=true")
	}
	if !escrowRepo.released["escrow-101"] {
		t.Fatalf("escrow milestone was not released in repo")
	}
}

func TestJobPairing_ConfirmPairing_ManualCode_Success(t *testing.T) {
	handler, _, _, escrowRepo := setupTestPairingEnvironment()

	// 1. Tech generates QR
	genReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", nil)
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userID", "tech-1"))
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userRole", "technician"))
	genW := httptest.NewRecorder()
	handler.GeneratePairingQR(genW, genReq)

	var genRes struct {
		Data dto.JobPairingQRResponse `json:"data"`
	}
	_ = json.Unmarshal(genW.Body.Bytes(), &genRes)

	// 2. Customer types the 6-digit verification code directly
	confirmPayload := dto.ConfirmJobPairingRequest{
		VerificationCode: genRes.Data.VerificationCode,
		Notes:            "Manual code sign-off",
		Rating:           4,
	}
	bodyBytes, _ := json.Marshal(confirmPayload)
	confirmReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/confirm-pairing", bytes.NewBuffer(bodyBytes))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userID", "cust-1"))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userRole", "customer"))
	confirmW := httptest.NewRecorder()

	handler.ConfirmPairing(confirmW, confirmReq)

	if confirmW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for manual verification code, got: %d, body: %s", confirmW.Code, confirmW.Body.String())
	}
	if !escrowRepo.released["escrow-101"] {
		t.Fatalf("expected escrow to be released via manual code")
	}
}

func TestJobPairing_ConfirmPairing_TamperedSignature_Rejected(t *testing.T) {
	handler, _, _, _ := setupTestPairingEnvironment()

	// 1. Tech generates QR
	genReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", nil)
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userID", "tech-1"))
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userRole", "technician"))
	genW := httptest.NewRecorder()
	handler.GeneratePairingQR(genW, genReq)

	var genRes struct {
		Data dto.JobPairingQRResponse `json:"data"`
	}
	_ = json.Unmarshal(genW.Body.Bytes(), &genRes)

	// 2. Customer submits modified signature
	confirmPayload := dto.ConfirmJobPairingRequest{
		PairingID: genRes.Data.PairingID,
		Signature: "sha256=tampered00000000000000000000000000000000000000000000000000000000",
		ExpiresAt: genRes.Data.ExpiresAtUnix,
	}
	bodyBytes, _ := json.Marshal(confirmPayload)
	confirmReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/confirm-pairing", bytes.NewBuffer(bodyBytes))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userID", "cust-1"))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userRole", "customer"))
	confirmW := httptest.NewRecorder()

	handler.ConfirmPairing(confirmW, confirmReq)

	if confirmW.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request for tampered signature, got: %d", confirmW.Code)
	}
}

func TestJobPairing_ConfirmPairing_Expired_Rejected(t *testing.T) {
	handler, pairingStore, _, _ := setupTestPairingEnvironment()

	now := time.Now().UTC()
	expiredSession := &models.JobPairingSession{
		ID:           "sess-exp",
		JobID:        "job-101",
		TechnicianID: "tech-1",
		CustomerID:   "cust-1",
		Amount:       2500,
		Currency:     "KES",
		Nonce:        "nonce-exp",
		Status:       "pending",
		ExpiresAt:    now.Add(-10 * time.Minute), // expired in past
		CreatedAt:    now.Add(-20 * time.Minute),
		UpdatedAt:    now.Add(-20 * time.Minute),
	}
	pairingStore.sessions["nonce-exp"] = expiredSession

	confirmPayload := dto.ConfirmJobPairingRequest{
		PairingID: "nonce-exp",
		Signature: "sha256=dummy",
	}
	bodyBytes, _ := json.Marshal(confirmPayload)
	confirmReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/confirm-pairing", bytes.NewBuffer(bodyBytes))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userID", "cust-1"))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userRole", "customer"))
	confirmW := httptest.NewRecorder()

	handler.ConfirmPairing(confirmW, confirmReq)

	if confirmW.Code != http.StatusGone {
		t.Fatalf("expected status 410 Gone for expired session, got: %d", confirmW.Code)
	}
}

func TestJobPairing_ConfirmPairing_Replay_Rejected(t *testing.T) {
	handler, pairingStore, _, _ := setupTestPairingEnvironment()

	now := time.Now().UTC()
	pairingStore.sessions["nonce-used"] = &models.JobPairingSession{
		ID:           "sess-used",
		JobID:        "job-101",
		TechnicianID: "tech-1",
		CustomerID:   "cust-1",
		Amount:       2500,
		Currency:     "KES",
		Nonce:        "nonce-used",
		Status:       "confirmed", // already confirmed
		ExpiresAt:    now.Add(10 * time.Minute),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	confirmPayload := dto.ConfirmJobPairingRequest{
		PairingID: "nonce-used",
		Signature: "sha256=dummy",
	}
	bodyBytes, _ := json.Marshal(confirmPayload)
	confirmReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/confirm-pairing", bytes.NewBuffer(bodyBytes))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userID", "cust-1"))
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), "userRole", "customer"))
	confirmW := httptest.NewRecorder()

	handler.ConfirmPairing(confirmW, confirmReq)

	if confirmW.Code != http.StatusConflict {
		t.Fatalf("expected status 409 Conflict for already confirmed session, got: %d", confirmW.Code)
	}
}

func TestJobPairing_GetStatus(t *testing.T) {
	handler, _, _, _ := setupTestPairingEnvironment()

	// 1. Tech generates QR
	genReq := httptest.NewRequest("POST", "/api/v1/jobs/job-101/pairing-qr", nil)
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userID", "tech-1"))
	genReq = genReq.WithContext(context.WithValue(genReq.Context(), "userRole", "technician"))
	genW := httptest.NewRecorder()
	handler.GeneratePairingQR(genW, genReq)

	// 2. Poll pairing status
	statusReq := httptest.NewRequest("GET", "/api/v1/jobs/job-101/pairing-status", nil)
	statusReq = statusReq.WithContext(context.WithValue(statusReq.Context(), "userID", "tech-1"))
	statusReq = statusReq.WithContext(context.WithValue(statusReq.Context(), "userRole", "technician"))
	statusW := httptest.NewRecorder()

	handler.GetPairingStatus(statusW, statusReq)

	if statusW.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for status check, got: %d, body: %s", statusW.Code, statusW.Body.String())
	}

	var res struct {
		Data dto.JobPairingStatusResponse `json:"data"`
	}
	_ = json.Unmarshal(statusW.Body.Bytes(), &res)

	if res.Data.Status != "pending" || res.Data.IsExpired {
		t.Fatalf("expected active pending session, got: %+v", res.Data)
	}
}
