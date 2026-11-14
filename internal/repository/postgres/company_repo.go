package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type CompanyRepository struct{ db *sql.DB }

var ErrTechnicianEmployed = errors.New("technician is already linked to another ISP")

func NewCompanyRepository() (*CompanyRepository, error) {
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &CompanyRepository{db: writer}, nil
}

func (r *CompanyRepository) LinkTechnician(ispID, technicianID string) (string, error) {
	if _, err := uuid.Parse(ispID); err != nil {
		return "", fmt.Errorf("invalid ISP ID: %w", err)
	}
	if _, err := uuid.Parse(technicianID); err != nil {
		return "", fmt.Errorf("invalid technician ID: %w", err)
	}
	tx, err := r.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Lock the ISP first to allocate company numbers serially, then lock the
	// technician so two ISPs cannot both claim the same independent account.
	var active bool
	if err = tx.QueryRow(`SELECT true FROM users WHERE id=$1 AND role='isp' AND status='active' FOR UPDATE`, ispID).Scan(&active); err != nil {
		return "", err
	}
	var phone, currentISP string
	if err = tx.QueryRow(`SELECT phone,COALESCE(isp_id::text,'') FROM users WHERE id=$1 AND role='technician' AND status='active' FOR UPDATE`, technicianID).Scan(&phone, &currentISP); err != nil {
		return "", err
	}
	if currentISP != "" && currentISP != ispID {
		return "", ErrTechnicianEmployed
	}
	var linkedElsewhere bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM isp_technicians WHERE technician_id=$1 AND isp_id<>$2 AND status='active')`, technicianID, ispID).Scan(&linkedElsewhere); err != nil {
		return "", err
	}
	if linkedElsewhere {
		return "", ErrTechnicianEmployed
	}
	var companyID string
	err = tx.QueryRow(`SELECT company_technician_id FROM isp_technicians WHERE isp_id=$1 AND technician_id=$2`, ispID, technicianID).Scan(&companyID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, sql.ErrNoRows) {
		// The suffix is the component AFTER -TECH-, not the second hyphen
		// segment (which is literally "TECH"). btrim handles old padded IDs.
		var number int
		err = tx.QueryRow(`SELECT COALESCE(MAX(CASE WHEN btrim(split_part(company_technician_id,'-TECH-',2)) ~ '^[0-9]+$' THEN btrim(split_part(company_technician_id,'-TECH-',2))::int END),0)+1 FROM isp_technicians WHERE isp_id=$1`, ispID).Scan(&number)
		if err != nil {
			return "", err
		}
		companyID = fmt.Sprintf("%s-TECH-%04d", ispID[:8], number)
		if _, err = tx.Exec(`INSERT INTO isp_technicians(id,isp_id,technician_id,company_technician_id,phone_snapshot) VALUES($1,$2,$3,$4,$5)`, uuid.NewString(), ispID, technicianID, companyID, phone); err != nil {
			return "", err
		}
	} else {
		if _, err = tx.Exec(`UPDATE isp_technicians SET status='active',ended_at=NULL,phone_snapshot=$3 WHERE isp_id=$1 AND technician_id=$2`, ispID, technicianID, phone); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(`UPDATE users SET isp_id=$1,updated_at=now() WHERE id=$2 AND role='technician'`, ispID, technicianID); err != nil {
		return "", err
	}
	return companyID, tx.Commit()
}
func (r *CompanyRepository) ListCompanyTechnicians(ispID string) ([]*models.User, error) {
	rows, err := r.db.Query(`SELECT u.id,u.phone,u.name,u.email,u.role,u.is_verified,u.isp_id,link.company_technician_id,link.started_at,link.ended_at FROM users u JOIN isp_technicians link ON link.technician_id=u.id WHERE link.isp_id=$1 AND link.status='active' ORDER BY u.name`, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.User{}
	for rows.Next() {
		u := &models.User{}
		if err := rows.Scan(&u.ID, &u.Phone, &u.Name, &u.Email, &u.Role, &u.IsVerified, &u.ISP_ID, &u.CompanyTechnicianID, &u.CompanyStartedAt, &u.CompanyEndedAt); err != nil {
			return nil, err
		}
		u.CompanyID = ispID
		out = append(out, u)
	}
	return out, rows.Err()
}
func (r *CompanyRepository) EndTechnicianLink(ispID, technicianID string, endedAt time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRow(`SELECT id FROM users WHERE id=$1 AND role='isp' FOR UPDATE`, ispID).Scan(&id); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT id FROM users WHERE id=$1 AND role='technician' FOR UPDATE`, technicianID).Scan(&id); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE isp_technicians SET status='ended',ended_at=$1 WHERE isp_id=$2 AND technician_id=$3 AND status='active'`, endedAt, ispID, technicianID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.Exec(`UPDATE users SET isp_id=NULL,updated_at=now() WHERE id=$1 AND isp_id=$2`, technicianID, ispID); err != nil {
		return err
	}
	return tx.Commit()
}
