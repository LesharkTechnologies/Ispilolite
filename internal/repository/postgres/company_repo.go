package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type CompanyRepository struct{ db *sql.DB }

func NewCompanyRepository() *CompanyRepository { return &CompanyRepository{db: database.GetWriter()} }

func (r *CompanyRepository) LinkTechnician(ispID, technicianID string) (string, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var phone string
	if err = tx.QueryRow(`SELECT phone FROM users WHERE id=$1 AND role='technician' AND status='active'`, technicianID).Scan(&phone); err != nil {
		return "", err
	}
	var number string
	err = tx.QueryRow(`SELECT COALESCE(MAX(split_part(company_technician_id,'-',2)::int),0)+1 FROM isp_technicians WHERE isp_id=$1`, ispID).Scan(&number)
	if err != nil {
		return "", err
	}
	companyID := fmt.Sprintf("%s-TECH-%04s", ispID[:8], number)
	_, err = tx.Exec(`INSERT INTO isp_technicians(id,isp_id,technician_id,company_technician_id,phone_snapshot) VALUES($1,$2,$3,$4,$5) ON CONFLICT(isp_id,technician_id) DO UPDATE SET status='active',ended_at=NULL,phone_snapshot=EXCLUDED.phone_snapshot`, uuid.NewString(), ispID, technicianID, companyID, phone)
	if err != nil {
		return "", err
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
	if _, err = tx.Exec(`UPDATE isp_technicians SET status='ended',ended_at=$1 WHERE isp_id=$2 AND technician_id=$3 AND status='active'`, endedAt, ispID, technicianID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE users SET isp_id=NULL,updated_at=now() WHERE id=$1 AND isp_id=$2`, technicianID, ispID); err != nil {
		return err
	}
	return tx.Commit()
}
