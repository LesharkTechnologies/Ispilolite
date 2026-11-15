package handlers

import (
	"database/sql"
	"encoding/json"
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
	if decodeJSON(w, r, &req) != nil || req.UserID == "" {
		respondWithError(w, 400, "user_id is required")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `UPDATE users SET phone='deleted-'||id::text,username='',name='Deleted user',email='',password_hash='',status='deleted',updated_at=now() WHERE id=$1 AND status='deletion_requested'`, req.UserID)
	if err != nil {
		respondWithError(w, 500, "failed to approve deletion")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondWithError(w, 404, "deletion request not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "user data sanitized"})
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
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: map[string]int64{"deleted": count}})
}
func (h *AdminHandler) AuditList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,COALESCE(actor_id::text,''),actor_role,event,action,resource_type,resource_id,description,success,metadata,occurred_at FROM audit_events WHERE ($1='' OR event=$1) ORDER BY occurred_at DESC LIMIT $2`, r.URL.Query().Get("event"), limit)
	if err != nil {
		respondWithError(w, 500, "failed to list audit events")
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, actor, role, event, action, resourceType, resourceID, description string
		var success bool
		var metadata []byte
		var at time.Time
		if err := rows.Scan(&id, &actor, &role, &event, &action, &resourceType, &resourceID, &description, &success, &metadata, &at); err != nil {
			respondWithError(w, 500, "failed to read audit events")
			return
		}
		var data map[string]interface{}
		_ = json.Unmarshal(metadata, &data)
		items = append(items, map[string]interface{}{"id": id, "actor_id": actor, "actor_role": role, "event": event, "action": action, "resource_type": resourceType, "resource_id": resourceID, "description": description, "success": success, "metadata": data, "occurred_at": at})
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
