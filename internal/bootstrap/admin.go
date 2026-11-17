package bootstrap

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strings"

	"ispilolite/internal/utils"
	"ispilolite/pkg/audit"
)

// EnsureAdminAccount ensures an administrator account configured via environment variables
// (ADMIN_USERNAME, ADMIN_PASSWORD) exists in the database with role 'admin' and active status.
// If the password or credentials in the environment change, it automatically synchronizes them.
func EnsureAdminAccount(ctx context.Context, db *sql.DB, logger *log.Logger) error {
	if logger == nil {
		logger = log.Default()
	}
	if db == nil {
		return nil
	}

	username := strings.TrimSpace(os.Getenv("ADMIN_USERNAME"))
	password := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD"))
	if username == "" || password == "" {
		logger.Printf("admin account bootstrap skipped: ADMIN_USERNAME or ADMIN_PASSWORD not configured")
		return nil
	}

	email := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	if email == "" {
		email = "admin@ispilo.co.ke"
	}
	phone := strings.TrimSpace(os.Getenv("ADMIN_PHONE"))
	if phone == "" {
		phone = "+254700000000"
	}
	name := strings.TrimSpace(os.Getenv("ADMIN_NAME"))
	if name == "" {
		name = "Super Administrator"
	}

	var existingID, existingHash, existingRole, existingStatus string
	err := db.QueryRowContext(ctx, `SELECT id, password_hash, role, status FROM users WHERE lower(username) = lower($1)`, username).Scan(&existingID, &existingHash, &existingRole, &existingStatus)
	if err == sql.ErrNoRows {
		// Does not exist: insert new super admin user
		hash, err := utils.HashPassword(password)
		if err != nil {
			logger.Printf("admin account bootstrap failed to hash password: %v", err)
			return err
		}
		adminID := utils.GenerateID()
		_, err = db.ExecContext(ctx, `
			INSERT INTO users (
				id, phone, username, name, email, role, password_hash,
				is_verified, rating, total_ratings, joined, created_at, updated_at,
				status, credit_balance, locale
			) VALUES (
				$1, $2, $3, $4, $5, 'admin', $6,
				true, 5.0, 0, now(), now(), now(),
				'active', 0, 'en'
			)
			ON CONFLICT (phone) DO UPDATE SET
				username = EXCLUDED.username,
				role = 'admin',
				password_hash = EXCLUDED.password_hash,
				is_verified = true,
				status = 'active',
				updated_at = now()
		`, adminID, phone, username, name, email, hash)
		if err != nil {
			logger.Printf("admin account bootstrap creation failed: %v", err)
			return err
		}

		_ = audit.Record(ctx, audit.Event{
			ActorID:      adminID,
			ActorRole:    "admin",
			Event:        "admin.provision",
			Action:       "CREATE",
			ResourceType: "user",
			ResourceID:   adminID,
			Description:  "Super admin account provisioned from environment variables",
			Success:      true,
			IPAddress:    "127.0.0.1",
		})
		logger.Printf("admin account %q successfully provisioned with full system access", username)
		return nil
	} else if err != nil {
		logger.Printf("admin account lookup error: %v", err)
		return err
	}

	// User exists: verify if password or role needs updating
	match, _ := utils.VerifyPassword(password, existingHash)
	if !match || existingRole != "admin" || existingStatus != "active" {
		hash, err := utils.HashPassword(password)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `
			UPDATE users SET 
				password_hash = $1, 
				role = 'admin', 
				status = 'active', 
				is_verified = true, 
				updated_at = now() 
			WHERE id = $2
		`, hash, existingID)
		if err != nil {
			logger.Printf("admin account credential sync failed: %v", err)
			return err
		}
		_ = audit.Record(ctx, audit.Event{
			ActorID:      existingID,
			ActorRole:    "admin",
			Event:        "admin.sync",
			Action:       "UPDATE",
			ResourceType: "user",
			ResourceID:   existingID,
			Description:  "Super admin account credentials synchronized from environment variables",
			Success:      true,
			IPAddress:    "127.0.0.1",
		})
		logger.Printf("admin account %q credentials/role synchronized from environment", username)
	} else {
		logger.Printf("admin account %q is active and verified", username)
	}

	return nil
}
