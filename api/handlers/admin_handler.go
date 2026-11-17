package handlers

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository"
	"ispilolite/internal/repository/postgres"
	authsvc "ispilolite/internal/services/auth"
	"ispilolite/internal/utils"
	"ispilolite/pkg/audit"
	"ispilolite/pkg/database"
)

type AdminHandler struct {
	db    *sql.DB
	audit *audit.Store
	users repository.UserRepository
}

func NewAdminHandler() (*AdminHandler, error) {
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	users, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	return &AdminHandler{db: writer, audit: audit.Configure(writer, os.Getenv("AUDIT_ARCHIVE_ROOT")), users: users}, nil
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Phone, Name, Email, Username, Password, Role string
		IsVerified                                   *bool `json:"is_verified,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid account payload")
		return
	}
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if !map[string]bool{"isp": true, "technician": true, "admin": true}[input.Role] || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Username) == "" || len(input.Password) < 8 {
		respondWithError(w, http.StatusBadRequest, "role, name, username, and an 8-character password are required")
		return
	}
	if err := authsvc.ValidatePhone(input.Phone); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := utils.HashPassword(input.Password)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	verified := true
	if input.IsVerified != nil {
		verified = *input.IsVerified
	}
	now := time.Now().UTC()
	user := &models.User{ID: utils.GenerateID(), Phone: strings.TrimSpace(input.Phone), Name: strings.TrimSpace(input.Name), Email: strings.TrimSpace(input.Email), Username: strings.TrimSpace(input.Username), Role: input.Role, PasswordHash: hash, IsVerified: verified, Status: "active", Joined: now, CreatedAt: now, UpdatedAt: now, Locale: "en"}
	if err := h.users.CreateUser(user); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			respondWithError(w, http.StatusConflict, "phone or username already exists")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	recordAudit(r, "admin.user_create", "CREATE", "user", user.ID, "Admin created "+user.Role+" user account for "+user.Username, true, map[string]interface{}{"username": user.Username, "role": user.Role})
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: user})
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	role, status := strings.TrimSpace(r.URL.Query().Get("role")), strings.TrimSpace(r.URL.Query().Get("status"))
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,phone,username,name,email,role,is_verified,status,created_at,updated_at,count(*) OVER()
		FROM users WHERE ($1='' OR role=$1) AND ($2='' OR status=$2) AND ($3='' OR name ILIKE '%'||$3||'%' OR username ILIKE '%'||$3||'%' OR phone ILIKE '%'||$3||'%')
		ORDER BY created_at DESC LIMIT $4 OFFSET $5`, role, status, query, limit, offset)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	total := 0
	for rows.Next() {
		var id, phone, username, name, email, userRole, userStatus string
		var verified bool
		var created, updated time.Time
		if err := rows.Scan(&id, &phone, &username, &name, &email, &userRole, &verified, &userStatus, &created, &updated, &total); err != nil {
			respondWithInternalError(w, err)
			return
		}
		items = append(items, map[string]interface{}{"id": id, "phone": phone, "username": username, "name": name, "email": email, "role": userRole, "is_verified": verified, "status": userStatus, "created_at": created, "updated_at": updated})
	}
	if err := rows.Err(); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items, "total": total, "limit": limit, "offset": offset}})
}

func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/users/")
	var input struct {
		Status     string `json:"status,omitempty"`
		IsVerified *bool  `json:"is_verified,omitempty"`
	}
	if id == "" || decodeJSON(w, r, &input) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid user update")
		return
	}
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status != "" && !map[string]bool{"active": true, "suspended": true}[input.Status] {
		respondWithError(w, http.StatusBadRequest, "status must be active or suspended")
		return
	}
	if input.Status == "" && input.IsVerified == nil {
		respondWithError(w, http.StatusBadRequest, "status or is_verified is required")
		return
	}
	result, err := h.db.ExecContext(r.Context(), `UPDATE users SET status=CASE WHEN $2='' THEN status ELSE $2 END,is_verified=COALESCE($3,is_verified),updated_at=now() WHERE id=$1`, id, input.Status, input.IsVerified)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		respondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	if input.Status != "" {
		_, _ = h.db.ExecContext(r.Context(), `UPDATE isps SET is_active=$2,updated_at=now() WHERE id=$1`, id, input.Status == "active")
		if input.Status != "active" {
			_, _ = h.db.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
		}
	}
	recordAudit(r, "admin.user_update", "UPDATE", "user", id, "Admin updated user status="+input.Status, true, map[string]interface{}{"user_id": id, "status": input.Status})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "user updated"})
}

func (h *AdminHandler) SettlementOutbox(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,escrow_id,COALESCE(milestone_id::text,''),action,amount,currency,status,attempts,last_error,next_attempt_at,created_at,updated_at FROM payment_settlement_outbox WHERE ($1='' OR status=$1) ORDER BY created_at LIMIT $2`, status, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, escrow, milestone, action, currency, itemStatus, lastError string
		var amount float64
		var attempts int
		var next, created, updated time.Time
		if err := rows.Scan(&id, &escrow, &milestone, &action, &amount, &currency, &itemStatus, &attempts, &lastError, &next, &created, &updated); err != nil {
			respondWithInternalError(w, err)
			return
		}
		items = append(items, map[string]interface{}{"id": id, "escrow_id": escrow, "milestone_id": milestone, "action": action, "amount": amount, "currency": currency, "status": itemStatus, "attempts": attempts, "last_error": lastError, "next_attempt_at": next, "created_at": created, "updated_at": updated})
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}
func (h *AdminHandler) RetrySettlement(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/settlements/outbox/")
	result, err := h.db.ExecContext(r.Context(), `UPDATE payment_settlement_outbox SET status='pending',next_attempt_at=now(),last_error='',updated_at=now() WHERE id=$1 AND status='failed'`, id)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		respondWithError(w, 404, "failed settlement item not found")
		return
	}
	recordAudit(r, "admin.settlement_retry", "UPDATE", "payment_settlement", id, "Admin queued failed settlement for retry", true, map[string]interface{}{"settlement_id": id})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "settlement queued for retry"})
}

func (h *AdminHandler) GetDeletionRequests(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,phone,name,email,role,status,updated_at FROM users WHERE status='deletion_requested' ORDER BY updated_at`)
	if err != nil {
		respondWithError(w, 500, "failed to list deletion requests")
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, phone, name, email, role, status string
		var at time.Time
		if err := rows.Scan(&id, &phone, &name, &email, &role, &status, &at); err != nil {
			respondWithError(w, 500, "failed to read deletion requests")
			return
		}
		items = append(items, map[string]interface{}{"id": id, "phone": phone, "name": name, "email": email, "role": role, "status": status, "requested_at": at})
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *AdminHandler) ApproveDeletion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	userID := pathParam(r.URL.Path, "/api/v1/admin/users/")
	if userID == "" {
		userID = pathParam(r.URL.Path, "/api/v1/admin/deletion-requests/")
	}
	if userID == "" && r.Body != nil {
		_ = decodeJSON(w, r, &req)
		userID = req.UserID
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		respondWithError(w, 400, "user_id is required")
		return
	}
	// Under Kenya Data Protection Act 2019 (KDPA) Sec 39 & 40 and statutory audit laws,
	// user data is preserved in the database for 2 years while consumer visibility/access
	// is immediately revoked.
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE users 
		SET status='deleted', 
			deleted_at=now(), 
			retention_until=now() + INTERVAL '2 years', 
			updated_at=now() 
		WHERE id=$1 AND (status='deletion_requested' OR status != 'deleted')`, userID)
	if err != nil {
		respondWithError(w, 500, "failed to approve deletion")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondWithError(w, 404, "deletion request or user not found")
		return
	}
	// Immediately revoke all auth sessions and hide from public listings
	_, _ = h.db.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	_, _ = h.db.ExecContext(r.Context(), `UPDATE technicians SET is_available=false WHERE id=$1 OR user_id=$1`, userID)
	_, _ = h.db.ExecContext(r.Context(), `UPDATE technician_profiles SET is_available=false WHERE technician_id=$1`, userID)
	_, _ = h.db.ExecContext(r.Context(), `UPDATE isps SET is_active=false WHERE id=$1`, userID)

	recordAudit(r, "admin.user_delete", "DELETE", "user", userID, "Admin approved account soft-deletion for user "+userID, true, map[string]interface{}{"user_id": userID})
	respondWithJSON(w, 200, dto.APIResponse{
		Success: true,
		Message: "account soft-deleted; consumer access revoked and data retained for 2 years per KDPA 2019",
	})
}

// PurgeExpiredRetention anonymizes user personal data after the 2-year KDPA retention window.
func (h *AdminHandler) PurgeExpiredRetention(w http.ResponseWriter, r *http.Request) {
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE users
		SET phone='deleted-'||id::text,
			username='',
			name='Deleted User',
			email='',
			password_hash='',
			latitude=NULL,
			longitude=NULL,
			avatar_url='',
			updated_at=now()
		WHERE status='deleted' AND retention_until IS NOT NULL AND retention_until <= now()
	`)
	if err != nil {
		respondWithError(w, 500, "failed to purge expired retention records")
		return
	}
	count, _ := res.RowsAffected()
	respondWithJSON(w, 200, dto.APIResponse{
		Success: true,
		Data:    map[string]int64{"purged": count},
	})
}

func (h *AdminHandler) DeleteTaxRate(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/tax-rates/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/admin/tax-rates/")
	}
	if id == "" {
		respondWithError(w, 400, "tax rate id is required")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `UPDATE tax_rates SET is_active=false WHERE id=$1`, id)
	if err != nil {
		respondWithError(w, 500, "failed to delete tax rate")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondWithError(w, 404, "tax rate not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "tax rate deleted"})
}

func (h *AdminHandler) TaxRates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,name,rate,is_default FROM tax_rates WHERE is_active=true ORDER BY name`)
	if err != nil {
		respondWithError(w, 500, "failed to list tax rates")
		return
	}
	defer rows.Close()
	out := []models.TaxRate{}
	for rows.Next() {
		v := models.TaxRate{}
		if err := rows.Scan(&v.ID, &v.Name, &v.Rate, &v.IsDefault); err != nil {
			respondWithError(w, 500, "failed to read tax rates")
			return
		}
		out = append(out, v)
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: out})
}
func (h *AdminHandler) CreateTaxRate(w http.ResponseWriter, r *http.Request) {
	var v models.TaxRate
	if decodeJSON(w, r, &v) != nil || strings.TrimSpace(v.Name) == "" || v.Rate < 0 || v.Rate > 100 {
		respondWithError(w, 400, "name and rate between 0 and 100 are required")
		return
	}
	if v.IsDefault {
		_, _ = h.db.ExecContext(r.Context(), `UPDATE tax_rates SET is_default=false WHERE is_default=true`)
	}
	err := h.db.QueryRowContext(r.Context(), `INSERT INTO tax_rates(id,name,rate,is_active,is_default,created_by,owner_id,scope) VALUES(gen_random_uuid(),$1,$2,true,$3,$4,$4,'SYSTEM') RETURNING id`, v.Name, v.Rate, v.IsDefault, userIDFromContext(r.Context())).Scan(&v.ID)
	if err != nil {
		respondWithError(w, 400, "failed to create tax rate")
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: v})
}
func (h *AdminHandler) CreateCustomTaxRate(w http.ResponseWriter, r *http.Request) {
	var v models.TaxRate
	if decodeJSON(w, r, &v) != nil || strings.TrimSpace(v.Name) == "" || v.Rate < 0 || v.Rate > 100 {
		respondWithError(w, 400, "name and rate between 0 and 100 are required")
		return
	}
	v.IsDefault = false
	err := h.db.QueryRowContext(r.Context(), `INSERT INTO tax_rates(id,name,rate,is_active,is_default,created_by,owner_id,scope) VALUES(gen_random_uuid(),$1,$2,true,false,$3,$3,'CUSTOM') RETURNING id`, v.Name, v.Rate, userIDFromContext(r.Context())).Scan(&v.ID)
	if err != nil {
		respondWithError(w, 409, "custom tax rate already exists")
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: v})
}
func (h *AdminHandler) CreateSystemUnit(w http.ResponseWriter, r *http.Request) {
	var unit models.QuotationUnit
	if decodeJSON(w, r, &unit) != nil || strings.TrimSpace(unit.Name) == "" || strings.TrimSpace(unit.SingularName) == "" || strings.TrimSpace(unit.PluralName) == "" {
		respondWithError(w, http.StatusBadRequest, "name, singular_name, and plural_name are required")
		return
	}
	err := h.db.QueryRowContext(r.Context(), `INSERT INTO quotation_units(id,name,singular_name,plural_name,symbol,is_system,issuer_id) VALUES(gen_random_uuid(),$1,$2,$3,$4,true,NULL) RETURNING id`, strings.TrimSpace(unit.Name), strings.TrimSpace(unit.SingularName), strings.TrimSpace(unit.PluralName), strings.TrimSpace(unit.Symbol)).Scan(&unit.ID)
	if err != nil {
		respondWithError(w, http.StatusConflict, "system unit already exists")
		return
	}
	unit.IsSystem = true
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: unit})
}
func (h *AdminHandler) MyTaxRates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,name,rate,is_default FROM tax_rates WHERE is_active=true AND (scope='SYSTEM' OR owner_id=$1) ORDER BY scope,name`, userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 500, "failed to list tax rates")
		return
	}
	defer rows.Close()
	out := []models.TaxRate{}
	for rows.Next() {
		v := models.TaxRate{}
		if err := rows.Scan(&v.ID, &v.Name, &v.Rate, &v.IsDefault); err != nil {
			respondWithError(w, 500, "failed to read tax rates")
			return
		}
		out = append(out, v)
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: out})
}
func (h *AdminHandler) BusinessProfile(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/business-profiles/")
	var p map[string]interface{}
	if decodeJSON(w, r, &p) != nil {
		respondWithError(w, 400, "invalid business profile")
		return
	}
	_, err := h.db.ExecContext(r.Context(), `INSERT INTO business_profiles(user_id,legal_name,registration_number,tax_number,address,phone,email,status) VALUES($1,$2,$3,$4,$5,$6,$7,'approved') ON CONFLICT(user_id) DO UPDATE SET legal_name=EXCLUDED.legal_name,registration_number=EXCLUDED.registration_number,tax_number=EXCLUDED.tax_number,address=EXCLUDED.address,phone=EXCLUDED.phone,email=EXCLUDED.email,updated_at=now()`, id, p["legal_name"], p["registration_number"], p["tax_number"], p["address"], p["phone"], p["email"])
	if err != nil {
		respondWithError(w, 400, "failed to save business profile")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "business profile saved"})
}
func (h *AdminHandler) MyBusinessProfile(w http.ResponseWriter, r *http.Request) {
	id := userIDFromContext(r.Context())
	if r.Method == http.MethodGet {
		var legal, reg, tax, address, phone, email, status string
		err := h.db.QueryRowContext(r.Context(), `SELECT legal_name,registration_number,tax_number,address,phone,email,status FROM business_profiles WHERE user_id=$1`, id).Scan(&legal, &reg, &tax, &address, &phone, &email, &status)
		if err != nil {
			respondWithError(w, 404, "business profile not found")
			return
		}
		respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: map[string]string{"legal_name": legal, "registration_number": reg, "tax_number": tax, "address": address, "phone": phone, "email": email, "status": status}})
		return
	}
	var p map[string]interface{}
	if decodeJSON(w, r, &p) != nil {
		respondWithError(w, 400, "invalid business profile")
		return
	}
	_, err := h.db.ExecContext(r.Context(), `INSERT INTO business_profiles(user_id,legal_name,registration_number,tax_number,address,phone,email,status) VALUES($1,$2,$3,$4,$5,$6,$7,'pending') ON CONFLICT(user_id) DO UPDATE SET legal_name=EXCLUDED.legal_name,registration_number=EXCLUDED.registration_number,tax_number=EXCLUDED.tax_number,address=EXCLUDED.address,phone=EXCLUDED.phone,email=EXCLUDED.email,status='pending',updated_at=now()`, id, p["legal_name"], p["registration_number"], p["tax_number"], p["address"], p["phone"], p["email"])
	if err != nil {
		respondWithError(w, 400, "failed to save business profile")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "business profile submitted"})
}
func (h *AdminHandler) ModeratePackage(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/packages/")
	var req struct{ Status, Note string }
	if decodeJSON(w, r, &req) != nil || !map[string]bool{"approved": true, "rejected": true, "suspended": true}[strings.ToLower(req.Status)] {
		respondWithError(w, 400, "invalid moderation status")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `UPDATE isp_packages SET moderation_status=$1,moderation_note=$2,moderated_by=$3,moderated_at=now(),is_active=($1='approved'),updated_at=now() WHERE id=$4`, strings.ToLower(req.Status), req.Note, userIDFromContext(r.Context()), id)
	if err != nil {
		respondWithError(w, 500, "failed to moderate package")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondWithError(w, 404, "package not found")
		return
	}
	recordAudit(r, "admin.package_moderate", "UPDATE", "isp_package", id, fmt.Sprintf("Admin moderated package %s status=%s", id, strings.ToLower(req.Status)), true, map[string]interface{}{"package_id": id, "status": req.Status, "note": req.Note})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "package moderated"})
}
func (h *AdminHandler) AuditExport(w http.ResponseWriter, r *http.Request) {
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	if err != nil {
		respondWithError(w, 400, "start must be RFC3339")
		return
	}
	end, err := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
	if err != nil {
		respondWithError(w, 400, "end must be RFC3339")
		return
	}
	path, count, err := h.audit.Export(r.Context(), r.URL.Query().Get("period"), start, end, userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	recordAudit(r, "admin.audit_export", "EXPORT", "audit_archive", path, fmt.Sprintf("Admin exported audit archive (%d records)", count), true, map[string]interface{}{"path": path, "events": count})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: map[string]interface{}{"path": path, "events": count}})
}
func (h *AdminHandler) AuditDelete(w http.ResponseWriter, r *http.Request) {
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	if err != nil {
		respondWithError(w, 400, "invalid start")
		return
	}
	end, err := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
	if err != nil {
		respondWithError(w, 400, "invalid end")
		return
	}
	count, err := h.audit.DeleteCovered(r.Context(), start, end, r.URL.Query().Get("event"))
	if err != nil {
		respondWithError(w, 409, err.Error())
		return
	}
	recordAudit(r, "admin.audit_delete", "DELETE", "audit_events", "", fmt.Sprintf("Admin purged %d archived audit records", count), true, map[string]interface{}{"deleted": count})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: map[string]int64{"deleted": count}})
}
func (h *AdminHandler) AuditList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	event := strings.TrimSpace(r.URL.Query().Get("event"))
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	if role == "" {
		role = strings.TrimSpace(r.URL.Query().Get("category"))
	}
	actorID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if actorID == "" {
		actorID = strings.TrimSpace(r.URL.Query().Get("actor_id"))
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip == "" {
		ip = strings.TrimSpace(r.URL.Query().Get("ip_address"))
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	var startTime, endTime *time.Time
	if s := strings.TrimSpace(r.URL.Query().Get("start")); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			startTime = &t
		}
	}
	if e := strings.TrimSpace(r.URL.Query().Get("end")); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			endTime = &t
		}
	}

	filter := audit.ListFilter{
		Event:     event,
		ActorRole: role,
		ActorID:   actorID,
		IPAddress: ip,
		Status:    status,
		Search:    q,
		StartTime: startTime,
		EndTime:   endTime,
		Limit:     limit,
		Offset:    offset,
	}

	items, total, err := h.audit.List(r.Context(), filter)
	if err != nil {
		respondWithError(w, 500, "failed to list audit events")
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	accept := r.Header.Get("Accept")
	if format == "html" || (strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")) {
		h.renderAuditHTML(w, r, items, total, filter)
		return
	}

	respondWithJSON(w, 200, dto.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"items":      items,
			"total":      total,
			"limit":      limit,
			"offset":     offset,
			"categories": []string{"admin", "client", "isp", "technicians"},
		},
	})
}

func (h *AdminHandler) DatabaseOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := make(map[string]interface{})

	var totalUsers, activeUsers, clientCount, ispCount, techCount, adminCount int
	_ = h.db.QueryRowContext(ctx, `SELECT 
		count(*),
		count(*) FILTER (WHERE status='active'),
		count(*) FILTER (WHERE role='customer' OR role='client'),
		count(*) FILTER (WHERE role='isp'),
		count(*) FILTER (WHERE role='technician'),
		count(*) FILTER (WHERE role='admin')
	FROM users`).Scan(&totalUsers, &activeUsers, &clientCount, &ispCount, &techCount, &adminCount)

	stats["users"] = map[string]interface{}{
		"total":       totalUsers,
		"active":      activeUsers,
		"clients":     clientCount,
		"isps":        ispCount,
		"technicians": techCount,
		"admins":      adminCount,
	}

	var auditCount int
	_ = h.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events`).Scan(&auditCount)
	stats["audit_events_total"] = auditCount

	var totalJobs, pendingJobs, inProgressJobs, completedJobs int
	_ = h.db.QueryRowContext(ctx, `SELECT 
		count(*),
		count(*) FILTER (WHERE status='pending'),
		count(*) FILTER (WHERE status='in_progress' OR status='assigned'),
		count(*) FILTER (WHERE status='completed')
	FROM job_requests`).Scan(&totalJobs, &pendingJobs, &inProgressJobs, &completedJobs)

	stats["jobs"] = map[string]interface{}{
		"total":       totalJobs,
		"pending":     pendingJobs,
		"in_progress": inProgressJobs,
		"completed":   completedJobs,
	}

	var totalPackages, activePackages, pendingModeration int
	_ = h.db.QueryRowContext(ctx, `SELECT 
		count(*),
		count(*) FILTER (WHERE is_active=true),
		count(*) FILTER (WHERE moderation_status='pending')
	FROM isp_packages`).Scan(&totalPackages, &activePackages, &pendingModeration)

	stats["packages"] = map[string]interface{}{
		"total":              totalPackages,
		"active":             activePackages,
		"pending_moderation": pendingModeration,
	}

	var totalDisputes, openDisputes, resolvedDisputes int
	_ = h.db.QueryRowContext(ctx, `SELECT 
		count(*),
		count(*) FILTER (WHERE status='open' OR status='pending'),
		count(*) FILTER (WHERE status='resolved')
	FROM disputes`).Scan(&totalDisputes, &openDisputes, &resolvedDisputes)

	stats["disputes"] = map[string]interface{}{
		"total":    totalDisputes,
		"open":     openDisputes,
		"resolved": resolvedDisputes,
	}

	var dbSize string
	_ = h.db.QueryRowContext(ctx, `SELECT pg_size_pretty(pg_database_size(current_database()))`).Scan(&dbSize)
	stats["database_size"] = dbSize

	recordAudit(r, "admin.database_view", "GET", "system", "", "Admin viewed system database overview", true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: stats})
}

func (h *AdminHandler) renderAuditHTML(w http.ResponseWriter, r *http.Request, items []audit.EventView, total int, filter audit.ListFilter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	htmlTmpl := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Ispilolite System Audit Logs — Audit Officer Portal</title>
<style>
  :root {
    --bg: #0f172a;
    --card-bg: #1e293b;
    --border: #334155;
    --text: #f8fafc;
    --text-muted: #94a3b8;
    --primary: #3b82f6;
    --primary-hover: #2563eb;
    --success: #10b981;
    --danger: #ef4444;
    --admin: #ef4444;
    --client: #10b981;
    --isp: #8b5cf6;
    --tech: #06b6d4;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, Cantarell, sans-serif; background: var(--bg); color: var(--text); padding: 24px; line-height: 1.5; }
  .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
  .title { font-size: 1.5rem; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 10px; }
  .badge-role { display: inline-block; padding: 3px 8px; border-radius: 9999px; font-size: 0.75rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; }
  .role-admin { background: rgba(239, 68, 68, 0.2); color: #f87171; border: 1px solid rgba(239, 68, 68, 0.4); }
  .role-client { background: rgba(16, 185, 129, 0.2); color: #34d399; border: 1px solid rgba(16, 185, 129, 0.4); }
  .role-isp { background: rgba(139, 92, 246, 0.2); color: #a78bfa; border: 1px solid rgba(139, 92, 246, 0.4); }
  .role-technicians { background: rgba(6, 182, 212, 0.2); color: #22d3ee; border: 1px solid rgba(6, 182, 212, 0.4); }
  .role-anon { background: rgba(148, 163, 184, 0.2); color: #cbd5e1; border: 1px solid rgba(148, 163, 184, 0.4); }
  .badge-status { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 0.75rem; font-weight: 700; }
  .status-success { background: rgba(16, 185, 129, 0.15); color: #10b981; }
  .status-failed { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
  .badge-ip { font-family: monospace; font-size: 0.8rem; background: #0f172a; padding: 2px 6px; border-radius: 4px; border: 1px solid var(--border); color: #cbd5e1; }
  .filter-card { background: var(--card-bg); border: 1px solid var(--border); border-radius: 8px; padding: 18px; margin-bottom: 20px; }
  .filter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 12px; }
  .filter-input { width: 100%; background: #0f172a; border: 1px solid var(--border); color: #fff; padding: 8px 12px; border-radius: 6px; font-size: 0.875rem; }
  .filter-input:focus { outline: none; border-color: var(--primary); }
  .btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 8px 16px; border-radius: 6px; font-size: 0.875rem; font-weight: 600; cursor: pointer; text-decoration: none; border: none; }
  .btn-primary { background: var(--primary); color: #fff; }
  .btn-primary:hover { background: var(--primary-hover); }
  .btn-secondary { background: var(--border); color: var(--text); }
  .btn-secondary:hover { background: #475569; }
  .stats-bar { display: flex; gap: 16px; margin-bottom: 16px; font-size: 0.875rem; color: var(--text-muted); align-items: center; }
  .stats-badge { background: #0f172a; border: 1px solid var(--border); padding: 4px 10px; border-radius: 6px; color: #fff; font-weight: 600; }
  .table-container { background: var(--card-bg); border: 1px solid var(--border); border-radius: 8px; overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; text-align: left; font-size: 0.875rem; }
  th { background: #0f172a; padding: 12px 14px; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border); }
  td { padding: 12px 14px; border-bottom: 1px solid var(--border); vertical-align: top; }
  tr:hover td { background: rgba(255, 255, 255, 0.02); }
  .user-cell { display: flex; flex-direction: column; gap: 2px; }
  .user-name { font-weight: 600; color: #fff; }
  .user-sub { font-size: 0.75rem; color: var(--text-muted); font-family: monospace; }
  .desc-cell { max-width: 380px; word-break: break-word; color: #e2e8f0; }
  .pagination { display: flex; justify-content: space-between; align-items: center; margin-top: 18px; }
</style>
</head>
<body>
  <div class="header">
    <div>
      <div class="title">🛡️ Ispilolite Audit Officer Portal</div>
      <div style="font-size:0.875rem; color:var(--text-muted); margin-top:4px;">
        Comprehensive system activity monitoring: user ID, category (admin, client, isp, technician), event, status, timestamp, IP address & action.
      </div>
    </div>
    <div style="display:flex; gap:10px;">
      <a href="/api/v1/admin/audit?format=json" class="btn btn-secondary">Raw JSON</a>
      <a href="/api/v1/admin/database/overview" class="btn btn-secondary">Database Overview</a>
    </div>
  </div>

  <div class="filter-card">
    <form method="GET" action="/api/v1/admin/audit">
      <input type="hidden" name="format" value="html">
      <div class="filter-grid">
        <div>
          <label style="font-size:0.75rem; color:var(--text-muted); display:block; margin-bottom:4px;">Search Activity / Actor</label>
          <input type="text" name="q" value="{{.Filter.Search}}" placeholder="e.g. login, payment, admin, IP" class="filter-input">
        </div>
        <div>
          <label style="font-size:0.75rem; color:var(--text-muted); display:block; margin-bottom:4px;">User Category</label>
          <select name="category" class="filter-input">
            <option value="" {{if eq .Filter.ActorRole ""}}selected{{end}}>All Categories</option>
            <option value="admin" {{if eq .Filter.ActorRole "admin"}}selected{{end}}>Admin</option>
            <option value="client" {{if or (eq .Filter.ActorRole "client") (eq .Filter.ActorRole "customer")}}selected{{end}}>Client (Customer)</option>
            <option value="isp" {{if eq .Filter.ActorRole "isp"}}selected{{end}}>ISP</option>
            <option value="technicians" {{if or (eq .Filter.ActorRole "technician") (eq .Filter.ActorRole "technicians")}}selected{{end}}>Technicians</option>
          </select>
        </div>
        <div>
          <label style="font-size:0.75rem; color:var(--text-muted); display:block; margin-bottom:4px;">Status</label>
          <select name="status" class="filter-input">
            <option value="" {{if eq .Filter.Status ""}}selected{{end}}>All Statuses</option>
            <option value="success" {{if eq .Filter.Status "success"}}selected{{end}}>SUCCESS</option>
            <option value="failed" {{if eq .Filter.Status "failed"}}selected{{end}}>FAILED</option>
          </select>
        </div>
        <div>
          <label style="font-size:0.75rem; color:var(--text-muted); display:block; margin-bottom:4px;">Event Name</label>
          <input type="text" name="event" value="{{.Filter.Event}}" placeholder="e.g. user.login" class="filter-input">
        </div>
        <div>
          <label style="font-size:0.75rem; color:var(--text-muted); display:block; margin-bottom:4px;">IP Address</label>
          <input type="text" name="ip" value="{{.Filter.IPAddress}}" placeholder="e.g. 197.232.45.10" class="filter-input">
        </div>
      </div>
      <div style="display:flex; justify-content:space-between; align-items:center;">
        <div style="display:flex; gap:8px;">
          <button type="submit" class="btn btn-primary">Apply Filters</button>
          <a href="/api/v1/admin/audit?format=html" class="btn btn-secondary">Reset</a>
        </div>
        <div style="font-size:0.875rem; color:var(--text-muted);">
          Total Logged Activities: <span class="stats-badge">{{.Total}}</span>
        </div>
      </div>
    </form>
  </div>

  <div class="table-container">
    <table>
      <thead>
        <tr>
          <th>Time (UTC)</th>
          <th>User Category</th>
          <th>Who Did This</th>
          <th>IP Address</th>
          <th>Event</th>
          <th>Action</th>
          <th>Status</th>
          <th>Description of Event</th>
        </tr>
      </thead>
      <tbody>
        {{if eq (len .Items) 0}}
        <tr>
          <td colspan="8" style="text-align:center; padding:32px; color:var(--text-muted);">
            No audit records match the current filter criteria.
          </td>
        </tr>
        {{end}}
        {{range .Items}}
        <tr>
          <td style="white-space:nowrap; font-family:monospace; font-size:0.8rem; color:var(--text-muted);">
            {{.Time}}
          </td>
          <td>
            {{if eq .UserCategory "admin"}}
              <span class="badge-role role-admin">Admin</span>
            {{else if eq .UserCategory "client"}}
              <span class="badge-role role-client">Client</span>
            {{else if eq .UserCategory "isp"}}
              <span class="badge-role role-isp">ISP</span>
            {{else if eq .UserCategory "technicians"}}
              <span class="badge-role role-technicians">Technician</span>
            {{else}}
              <span class="badge-role role-anon">{{.UserCategory}}</span>
            {{end}}
          </td>
          <td>
            <div class="user-cell">
              {{if .ActorUsername}}
                <span class="user-name">@{{.ActorUsername}}</span>
                {{if .ActorName}}<span style="font-size:0.8rem; color:#cbd5e1;">{{.ActorName}}</span>{{end}}
              {{else if .ActorName}}
                <span class="user-name">{{.ActorName}}</span>
              {{else if .ActorID}}
                <span class="user-name">User</span>
              {{else}}
                <span class="user-name" style="color:var(--text-muted);">Anonymous / System</span>
              {{end}}
              {{if .ActorID}}
                <span class="user-sub" title="Actor ID">{{.ActorID}}</span>
              {{end}}
            </div>
          </td>
          <td>
            {{if .IPAddress}}
              <span class="badge-ip">{{.IPAddress}}</span>
            {{else}}
              <span style="color:var(--text-muted); font-size:0.8rem;">-</span>
            {{end}}
          </td>
          <td style="font-weight:600; color:#fff;">
            {{.Event}}
          </td>
          <td>
            <span style="font-size:0.75rem; background:rgba(255,255,255,0.06); padding:2px 6px; border-radius:4px; font-weight:600;">
              {{.Action}}
            </span>
          </td>
          <td>
            {{if .Success}}
              <span class="badge-status status-success">SUCCESS</span>
            {{else}}
              <span class="badge-status status-failed">FAILED</span>
            {{end}}
          </td>
          <td class="desc-cell">
            {{.Description}}
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</body>
</html>`

	tmpl, err := template.New("audit_dashboard").Parse(htmlTmpl)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}

	data := struct {
		Items  []audit.EventView
		Total  int
		Filter audit.ListFilter
	}{
		Items:  items,
		Total:  total,
		Filter: filter,
	}

	_ = tmpl.Execute(w, data)
}
