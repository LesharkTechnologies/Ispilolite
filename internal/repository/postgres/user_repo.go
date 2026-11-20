package postgres

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"

	"github.com/lib/pq"
)

func (r *userRepo) CreateRefreshSession(sessionID, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.dbWriter.Exec(`INSERT INTO auth_sessions (id,user_id,token_hash,expires_at,created_at,last_used_at) VALUES ($1,$2,$3,$4,now(),now())`, sessionID, userID, tokenHash, expiresAt)
	return err
}

func (r *userRepo) ListUsersForMessaging(role string, userIDs []string) ([]*models.User, error) {
	rows, err := r.dbReader.Query(`SELECT id,phone,name,role FROM users WHERE status='active' AND phone<>'' AND ($1='' OR role=$1) AND (cardinality($2::uuid[])=0 OR id=ANY($2::uuid[])) ORDER BY id`, role, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []*models.User{}
	for rows.Next() {
		user := &models.User{}
		if err := rows.Scan(&user.ID, &user.Phone, &user.Name, &user.Role); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}
func (r *userRepo) RefreshSessionActive(sessionID, tokenHash string) (bool, error) {
	var ok bool
	err := r.dbWriter.QueryRow(`UPDATE auth_sessions SET last_used_at=now() WHERE id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>now() RETURNING true`, sessionID, tokenHash).Scan(&ok)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return ok, err
}

// RevokeRefreshSession invalidates one rotated refresh credential without
// signing the user out on their other devices.
func (r *userRepo) RevokeRefreshSession(sessionID string) error {
	_, err := r.dbWriter.Exec(`UPDATE auth_sessions SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, sessionID)
	return err
}
func (r *userRepo) SanitizeAndDeleteUser(userID string) error {
	// In accordance with Kenya Data Protection Act 2019 (KDPA) Section 39 & 40,
	// telecommunications regulations, and financial audit laws:
	// 1. Soft-delete user account with status='deleted' and a 2-year statutory retention period.
	// 2. Identity, skills, and transaction history are PRESERVED in the DB for 2 years.
	// 3. Immediately cut off all consumer access, revoke active sessions, and hide from search.
	now := time.Now().UTC()
	retentionUntil := now.AddDate(2, 0, 0)
	query := `
		UPDATE users
		SET status = 'deleted',
			deleted_at = $1,
			retention_until = $2,
			updated_at = $1
		WHERE id = $3
	`
	if _, err := r.dbWriter.Exec(query, now, retentionUntil, userID); err != nil {
		return err
	}
	_, _ = r.dbWriter.Exec(`UPDATE technicians SET is_available = false WHERE id = $1 OR user_id = $1`, userID)
	_, _ = r.dbWriter.Exec(`UPDATE technician_profiles SET is_available = false WHERE technician_id = $1`, userID)
	_, _ = r.dbWriter.Exec(`UPDATE isps SET is_active = false WHERE id = $1`, userID)
	return r.RevokeAllRefreshSessions(userID)
}

func (r *userRepo) PurgeExpiredUsers() (int64, error) {
	// Statutory retention period has elapsed (2 years post-deletion).
	// Anonymize personal identifiers to comply with KDPA erasure requirements.
	now := time.Now().UTC()
	query := `
		UPDATE users
		SET phone = 'deleted-' || id::text,
			username = '',
			name = 'Deleted User',
			email = '',
			password_hash = '',
			latitude = NULL,
			longitude = NULL,
			avatar_url = '',
			updated_at = $1
		WHERE status = 'deleted' AND retention_until IS NOT NULL AND retention_until <= $1
	`
	res, err := r.dbWriter.Exec(query, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *userRepo) RevokeAllRefreshSessions(userID string) error {
	_, err := r.dbWriter.Exec(`UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

type userRepo struct {
	dbReader *sql.DB
	dbWriter *sql.DB
}

func NewUserRepo() (*userRepo, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &userRepo{
		dbReader: reader,
		dbWriter: writer,
	}, nil
}

func (r *userRepo) CreateUser(user *models.User) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `
		INSERT INTO users (id, phone, username, name, email, role, password_hash, is_verified, rating, total_ratings, joined, created_at, updated_at, isp_id, town, county, referral_code, locale)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULLIF($14,'')::uuid, $15, $16, NULLIF($17,''), $18)
	`
	_, err = tx.Exec(
		query,
		user.ID,
		user.Phone,
		user.Username,
		user.Name,
		user.Email,
		user.Role,
		user.PasswordHash,
		user.IsVerified,
		user.Rating,
		user.TotalRatings,
		user.Joined,
		user.CreatedAt,
		user.UpdatedAt,
		user.ISP_ID,
		user.Town,
		user.County,
		user.ReferralCode,
		user.Locale,
	)
	if err != nil {
		return err
	}
	// Role-specific records are part of account creation, not optional profile
	// setup. Keeping them in this transaction prevents valid users whose public
	// profile, packages, or search records can never be resolved.
	switch user.Role {
	case models.RoleISP:
		_, err = tx.Exec(`INSERT INTO isps (id,name,avatar_url,logo_url,customer_care_number,rating,review_count,is_active,county,created_at,updated_at)
			VALUES($1,$2,$3,$3,$4,$5,$6,true,$7,$8,$9) ON CONFLICT(id) DO NOTHING`, user.ID, user.Name, user.AvatarURL, user.Phone, user.Rating, user.TotalRatings, user.County, user.CreatedAt, user.UpdatedAt)
	case models.RoleTechnician:
		_, err = tx.Exec(`INSERT INTO technicians (id,user_id,name,rating,jobs_done,is_available,county,lat,lon,created_at,updated_at)
			VALUES($1,$1,$2,$3,0,true,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, user.ID, user.Name, user.Rating, user.County, nullableCoordinate(user.Location, true), nullableCoordinate(user.Location, false), user.CreatedAt, user.UpdatedAt)
		if err == nil {
			_, err = tx.Exec(`INSERT INTO technician_profiles(technician_id,county,town,is_available,updated_at)
				VALUES($1,$2,$3,true,$4) ON CONFLICT(technician_id) DO NOTHING`, user.ID, user.County, user.Town, user.UpdatedAt)
		}
	}
	if err != nil {
		return err
	}
	if user.ReferredByCode != "" {
		result, err := tx.Exec(`INSERT INTO referrals(id,referrer_id,referred_id,code,status,created_at)
			SELECT gen_random_uuid(),id,$2,upper($1),'pending',now() FROM users WHERE upper(referral_code)=upper($1) AND id<>$2`, user.ReferredByCode, user.ID)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}

func nullableCoordinate(location *models.Coordinates, latitude bool) interface{} {
	if location == nil {
		return nil
	}
	if latitude {
		return location.Lat
	}
	return location.Lng
}

var ErrRoleChangeCooldown = errors.New("account type can only be changed once every 24 hours")

func (r *userRepo) GetUserByUsername(username string) (*models.User, error) {
	query := `SELECT id, phone, username, name, email, role, password_hash, is_verified, rating, total_ratings, joined, created_at, updated_at, COALESCE(isp_id::text,''), latitude, longitude, town, county, COALESCE(referral_code,''),credit_balance,locale,status,COALESCE(avatar_url,''),COALESCE(identity_status,'unverified'),COALESCE(verified_name,''),COALESCE(display_name,''),role_changed_at,COALESCE(secondary_role,'') FROM users WHERE lower(username) = lower($1)`
	return r.scanUser(r.dbReader.QueryRow(query, username))
}

func (r *userRepo) GetUserByPhone(phone string) (*models.User, error) {
	query := `
		SELECT id, phone, username, name, email, role, password_hash, is_verified, rating, total_ratings, joined, created_at, updated_at, COALESCE(isp_id::text,''), latitude, longitude, town, county, COALESCE(referral_code,''),credit_balance,locale,status,COALESCE(avatar_url,''),COALESCE(identity_status,'unverified'),COALESCE(verified_name,''),COALESCE(display_name,''),role_changed_at,COALESCE(secondary_role,'')
		FROM users
		WHERE phone = $1
	`
	return r.scanUser(r.dbReader.QueryRow(query, phone))
}

func (r *userRepo) scanUser(row *sql.Row) (*models.User, error) {
	user := &models.User{}
	var lat, lng sql.NullFloat64
	var roleChangedAt sql.NullTime
	var secondaryRole string
	err := row.Scan(
		&user.ID,
		&user.Phone,
		&user.Username,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.PasswordHash,
		&user.IsVerified,
		&user.Rating,
		&user.TotalRatings,
		&user.Joined,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.ISP_ID,
		&lat,
		&lng,
		&user.Town,
		&user.County,
		&user.ReferralCode,
		&user.CreditBalance,
		&user.Locale,
		&user.Status,
		&user.AvatarURL,
		&user.IdentityStatus,
		&user.VerifiedName,
		&user.DisplayName,
		&roleChangedAt,
		&secondaryRole,
	)
	if err != nil {
		return nil, err
	}
	if lat.Valid && lng.Valid {
		user.Location = &models.Coordinates{Lat: lat.Float64, Lng: lng.Float64}
	}
	if roleChangedAt.Valid {
		user.RoleChangedAt = &roleChangedAt.Time
	}
	user.SecondaryRole = secondaryRole

	return user, nil
}

func (r *userRepo) GetUserByID(userID string) (*models.User, error) {
	query := `
		SELECT id, phone, username, name, email, role, password_hash, is_verified, rating, total_ratings, joined, created_at, updated_at, COALESCE(isp_id::text,''), latitude, longitude, town, county, COALESCE(referral_code,''),credit_balance,locale,status,COALESCE(avatar_url,''),COALESCE(identity_status,'unverified'),COALESCE(verified_name,''),COALESCE(display_name,''),role_changed_at,COALESCE(secondary_role,'')
		FROM users
		WHERE id = $1
	`
	user := &models.User{}
	var lat, lng sql.NullFloat64
	var roleChangedAt sql.NullTime
	var secondaryRole string
	err := r.dbReader.QueryRow(query, userID).Scan(
		&user.ID,
		&user.Phone,
		&user.Username,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.PasswordHash,
		&user.IsVerified,
		&user.Rating,
		&user.TotalRatings,
		&user.Joined,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.ISP_ID,
		&lat,
		&lng,
		&user.Town,
		&user.County,
		&user.ReferralCode,
		&user.CreditBalance,
		&user.Locale,
		&user.Status,
		&user.AvatarURL,
		&user.IdentityStatus,
		&user.VerifiedName,
		&user.DisplayName,
		&roleChangedAt,
		&secondaryRole,
	)
	if err != nil {
		return nil, err
	}
	if lat.Valid && lng.Valid {
		user.Location = &models.Coordinates{Lat: lat.Float64, Lng: lng.Float64}
	}
	if roleChangedAt.Valid {
		user.RoleChangedAt = &roleChangedAt.Time
	}
	user.SecondaryRole = secondaryRole
	return user, nil
}

func (r *userRepo) AccountActive(userID, role string) bool {
	var active bool
	err := r.dbWriter.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND (role=$2 OR role='both' OR $2='admin' OR $2='') AND status='active')`, userID, role).Scan(&active)
	return err == nil && active
}

func (r *userRepo) QualifyReferral(userID string, reward float64) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, referrer, referred string
	err = tx.QueryRow(`UPDATE referrals SET status='qualified',reward_amount=$2,qualified_at=now()
		WHERE referred_id=$1 AND status='pending' RETURNING id,referrer_id,referred_id`, userID, reward).Scan(&id, &referrer, &referred)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range []struct{ user, kind string }{{referrer, "referrer_reward"}, {referred, "referee_reward"}} {
		reference := "referral:" + id + ":" + entry.kind
		result, err := tx.Exec(`INSERT INTO referral_credit_ledger(id,user_id,referral_id,entry_type,amount,reference)
			VALUES(gen_random_uuid(),$1,$2,$3,$4,$5) ON CONFLICT(user_id,reference) DO NOTHING`, entry.user, id, entry.kind, reward, reference)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n > 0 {
			if _, err = tx.Exec(`UPDATE users SET credit_balance=credit_balance+$1,updated_at=now() WHERE id=$2`, reward, entry.user); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (r *userRepo) UpdateUser(user *models.User) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE users SET name=$1,email=$2,is_verified=$3,updated_at=$4,
		isp_id=NULLIF($5,'')::uuid,latitude=$6,longitude=$7,town=$8,county=$9,locale=$10,avatar_url=CASE WHEN $11='' THEN avatar_url ELSE $11 END,
		display_name=CASE WHEN $12='' THEN display_name ELSE $12 END
		WHERE id=$13`
	var lat, lng sql.NullFloat64
	if user.Location != nil {
		lat.Float64 = user.Location.Lat
		lat.Valid = true
		lng.Float64 = user.Location.Lng
		lng.Valid = true
	}
	result, err := tx.Exec(
		query,
		user.Name,
		user.Email,
		user.IsVerified,
		user.UpdatedAt,
		user.ISP_ID,
		lat,
		lng,
		user.Town,
		user.County,
		user.Locale,
		user.AvatarURL,
		user.DisplayName,
		user.ID,
	)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	switch user.Role {
	case models.RoleISP:
		_, err = tx.Exec(`INSERT INTO isps(id,name,avatar_url,logo_url,customer_care_number,county,created_at,updated_at,display_name)
			VALUES($1,$2,$3,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,avatar_url=CASE WHEN EXCLUDED.avatar_url='' THEN isps.avatar_url ELSE EXCLUDED.avatar_url END,logo_url=CASE WHEN EXCLUDED.logo_url='' THEN isps.logo_url ELSE EXCLUDED.logo_url END,county=EXCLUDED.county,display_name=CASE WHEN EXCLUDED.display_name='' THEN isps.display_name ELSE EXCLUDED.display_name END,updated_at=EXCLUDED.updated_at`, user.ID, user.Name, user.AvatarURL, user.Phone, user.County, user.CreatedAt, user.UpdatedAt, user.DisplayName)
	case models.RoleTechnician:
		_, err = tx.Exec(`INSERT INTO technicians(id,user_id,name,avatar_url,rating,is_available,county,lat,lon,created_at,updated_at,display_name)
			VALUES($1,$1,$2,$3,$4,true,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,avatar_url=CASE WHEN EXCLUDED.avatar_url='' THEN technicians.avatar_url ELSE EXCLUDED.avatar_url END,county=EXCLUDED.county,lat=EXCLUDED.lat,lon=EXCLUDED.lon,display_name=CASE WHEN EXCLUDED.display_name='' THEN technicians.display_name ELSE EXCLUDED.display_name END,updated_at=EXCLUDED.updated_at`, user.ID, user.Name, user.AvatarURL, user.Rating, user.County, lat, lng, user.CreatedAt, user.UpdatedAt, user.DisplayName)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *userRepo) SetDisplayName(userID, displayName string) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	displayName = strings.TrimSpace(displayName)
	result, err := tx.Exec(`UPDATE users SET display_name=$1, updated_at=now() WHERE id=$2`, displayName, userID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	_, _ = tx.Exec(`UPDATE technicians SET display_name=$1, updated_at=now() WHERE id=$2 OR user_id=$2`, displayName, userID)
	_, _ = tx.Exec(`UPDATE technician_profiles SET display_name=$1, updated_at=now() WHERE technician_id=$2`, displayName, userID)
	_, _ = tx.Exec(`UPDATE isps SET display_name=$1, updated_at=now() WHERE id=$2`, displayName, userID)
	return tx.Commit()
}

func (r *userRepo) ChangeAccountType(userID, targetRole string, bypassCooldown bool) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentRole string
	var roleChangedAt sql.NullTime
	var name, phone, county, town, avatarURL string
	var rating float64
	var totalRatings int
	var lat, lon sql.NullFloat64

	err = tx.QueryRow(`SELECT role, role_changed_at, name, phone, county, town, COALESCE(avatar_url,''), rating, total_ratings, latitude, longitude
		FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(
		&currentRole, &roleChangedAt, &name, &phone, &county, &town, &avatarURL, &rating, &totalRatings, &lat, &lon,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}

	if currentRole == targetRole {
		return nil
	}

	if !bypassCooldown && roleChangedAt.Valid {
		if time.Since(roleChangedAt.Time) < 24*time.Hour {
			return ErrRoleChangeCooldown
		}
	}

	now := time.Now().UTC()

	// Update user role and role_changed_at
	_, err = tx.Exec(`UPDATE users SET role = $1, role_changed_at = $2, updated_at = $2 WHERE id = $3`, targetRole, now, userID)
	if err != nil {
		return err
	}

	// Ensure role-specific records exist
	switch targetRole {
	case models.RoleTechnician:
		_, _ = tx.Exec(`INSERT INTO technicians(id, user_id, name, avatar_url, rating, jobs_done, is_available, county, lat, lon, created_at, updated_at)
			VALUES($1, $1, $2, $3, $4, 0, true, $5, $6, $7, $8, $8)
			ON CONFLICT(id) DO UPDATE SET is_available = true, updated_at = $8`,
			userID, name, avatarURL, rating, county, lat, lon, now)
		_, _ = tx.Exec(`INSERT INTO technician_profiles(technician_id, county, town, is_available, updated_at)
			VALUES($1, $2, $3, true, $4)
			ON CONFLICT(technician_id) DO UPDATE SET is_available = true, updated_at = $4`,
			userID, county, town, now)

	case models.RoleISP:
		_, _ = tx.Exec(`INSERT INTO isps(id, name, avatar_url, logo_url, customer_care_number, rating, review_count, is_active, county, created_at, updated_at)
			VALUES($1, $2, $3, $3, $4, $5, $6, true, $7, $8, $8)
			ON CONFLICT(id) DO UPDATE SET is_active = true, updated_at = $8`,
			userID, name, avatarURL, phone, rating, totalRatings, county, now)

	case models.RoleBoth:
		// Ensure BOTH technician and ISP records are active!
		_, _ = tx.Exec(`INSERT INTO technicians(id, user_id, name, avatar_url, rating, jobs_done, is_available, county, lat, lon, created_at, updated_at)
			VALUES($1, $1, $2, $3, $4, 0, true, $5, $6, $7, $8, $8)
			ON CONFLICT(id) DO UPDATE SET is_available = true, updated_at = $8`,
			userID, name, avatarURL, rating, county, lat, lon, now)
		_, _ = tx.Exec(`INSERT INTO technician_profiles(technician_id, county, town, is_available, updated_at)
			VALUES($1, $2, $3, true, $4)
			ON CONFLICT(technician_id) DO UPDATE SET is_available = true, updated_at = $4`,
			userID, county, town, now)
		_, _ = tx.Exec(`INSERT INTO isps(id, name, avatar_url, logo_url, customer_care_number, rating, review_count, is_active, county, created_at, updated_at)
			VALUES($1, $2, $3, $3, $4, $5, $6, true, $7, $8, $8)
			ON CONFLICT(id) DO UPDATE SET is_active = true, updated_at = $8`,
			userID, name, avatarURL, phone, rating, totalRatings, county, now)

	case models.RoleCustomer:
		_, _ = tx.Exec(`UPDATE technicians SET is_available = false, updated_at = $1 WHERE user_id = $2 OR id = $2`, now, userID)
		_, _ = tx.Exec(`UPDATE technician_profiles SET is_available = false, updated_at = $1 WHERE technician_id = $2`, now, userID)
		_, _ = tx.Exec(`UPDATE isps SET is_active = false, updated_at = $1 WHERE id = $2`, now, userID)
	}

	return tx.Commit()
}

func (r *userRepo) SetUserStatus(userID, status string) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`UPDATE users SET status = $1, updated_at = now() WHERE id = $2`, status, userID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}

	if status == "suspended" || status == "inactive" {
		_, _ = tx.Exec(`UPDATE technicians SET is_available = false, updated_at = now() WHERE id = $1 OR user_id = $1`, userID)
		_, _ = tx.Exec(`UPDATE technician_profiles SET is_available = false, updated_at = now() WHERE technician_id = $1`, userID)
		_, _ = tx.Exec(`UPDATE isps SET is_active = false, updated_at = now() WHERE id = $1`, userID)
		_, _ = tx.Exec(`UPDATE auth_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	}

	return tx.Commit()
}

func (r *userRepo) SetISPTechnicianStatus(ispID, technicianID, status string) error {
	tx, err := r.dbWriter.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	err = tx.QueryRow(`
		SELECT count(*) FROM users u
		WHERE u.id = $1 AND (u.isp_id = $2 OR EXISTS(SELECT 1 FROM isp_technicians it WHERE it.technician_id = u.id AND it.isp_id = $2))
	`, technicianID, ispID).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}

	_, err = tx.Exec(`UPDATE users SET status = $1, updated_at = now() WHERE id = $2`, status, technicianID)
	if err != nil {
		return err
	}

	if status == "suspended" || status == "inactive" {
		_, _ = tx.Exec(`UPDATE technicians SET is_available = false, updated_at = now() WHERE id = $1 OR user_id = $1`, technicianID)
		_, _ = tx.Exec(`UPDATE technician_profiles SET is_available = false, updated_at = now() WHERE technician_id = $1`, technicianID)
		_, _ = tx.Exec(`UPDATE auth_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, technicianID)
		_, _ = tx.Exec(`UPDATE isp_technicians SET status = 'suspended', ended_at = now() WHERE technician_id = $1 AND isp_id = $2`, technicianID, ispID)
	} else if status == "active" {
		_, _ = tx.Exec(`UPDATE technicians SET is_available = true, updated_at = now() WHERE id = $1 OR user_id = $1`, technicianID)
		_, _ = tx.Exec(`UPDATE technician_profiles SET is_available = true, updated_at = now() WHERE technician_id = $1`, technicianID)
	}

	return tx.Commit()
}

func (r *userRepo) GetUsersByStatus(status string) ([]*models.User, error) {
	query := `
		SELECT id, phone, username, name, email, role, password_hash, is_verified, rating, total_ratings, joined, created_at, updated_at, COALESCE(isp_id::text,''), latitude, longitude, town, county
		FROM users
		WHERE status = $1
	`
	rows, err := r.dbReader.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		var lat, lng sql.NullFloat64
		err := rows.Scan(
			&user.ID,
			&user.Phone,
			&user.Username,
			&user.Name,
			&user.Email,
			&user.Role,
			&user.PasswordHash,
			&user.IsVerified,
			&user.Rating,
			&user.TotalRatings,
			&user.Joined,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.ISP_ID,
			&lat,
			&lng,
			&user.Town,
			&user.County,
		)
		if err != nil {
			return nil, err
		}
		if lat.Valid && lng.Valid {
			user.Location = &models.Coordinates{Lat: lat.Float64, Lng: lng.Float64}
		}
		users = append(users, user)
	}

	return users, nil
}

func (r *userRepo) GetTechniciansByISPID(ispID string) ([]*models.User, error) {
	query := `
		SELECT id, phone, username, name, email, role, password_hash, is_verified, rating, total_ratings, joined, created_at, updated_at, COALESCE(isp_id::text,''), latitude, longitude, town, county
		FROM users
		WHERE isp_id = $1 AND role = 'technician'
	`

	rows, err := r.dbReader.Query(query, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		var lat, lng sql.NullFloat64
		err := rows.Scan(
			&user.ID,
			&user.Phone,
			&user.Username,
			&user.Name,
			&user.Email,
			&user.Role,
			&user.PasswordHash,
			&user.IsVerified,
			&user.Rating,
			&user.TotalRatings,
			&user.Joined,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.ISP_ID,
			&lat,
			&lng,
			&user.Town,
			&user.County,
		)
		if err != nil {
			return nil, err
		}
		if lat.Valid && lng.Valid {
			user.Location = &models.Coordinates{Lat: lat.Float64, Lng: lng.Float64}
		}
		users = append(users, user)
	}

	return users, nil
}

func (r *userRepo) RequestDeleteUser(userID string, status string) error {
	now := time.Now().UTC()
	retentionUntil := now.AddDate(2, 0, 0)
	query := `
		UPDATE users
		SET status = $1,
			deleted_at = CASE WHEN $1 = 'deleted' THEN $2 ELSE deleted_at END,
			retention_until = CASE WHEN $1 = 'deleted' THEN $3 ELSE retention_until END,
			updated_at = $2
		WHERE id = $4
	`
	if _, err := r.dbWriter.Exec(query, status, now, retentionUntil, userID); err != nil {
		return err
	}
	if status == "deleted" {
		_, _ = r.dbWriter.Exec(`UPDATE technicians SET is_available = false WHERE id = $1 OR user_id = $1`, userID)
		_, _ = r.dbWriter.Exec(`UPDATE technician_profiles SET is_available = false WHERE technician_id = $1`, userID)
		_, _ = r.dbWriter.Exec(`UPDATE isps SET is_active = false WHERE id = $1`, userID)
		return r.RevokeAllRefreshSessions(userID)
	}
	return nil
}
