package postgres

import (
	"database/sql"
	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type VacancyRepository struct{ db *sql.DB }

func NewVacancyRepository() *VacancyRepository { return &VacancyRepository{database.GetWriter()} }
func (r *VacancyRepository) Create(v *models.ISPVacancy) error {
	_, err := r.db.Exec(`INSERT INTO isp_vacancies(id,isp_id,title,description,service_type,slots,deadline_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.ID, v.ISPID, v.Title, v.Description, v.ServiceType, v.Slots, v.DeadlineAt)
	return err
}
func (r *VacancyRepository) List(ispID string) ([]*models.ISPVacancy, error) {
	if _, err := r.db.Exec(`UPDATE isp_vacancies SET status='expired',updated_at=now() WHERE deadline_at<=now() AND status='open'`); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`SELECT id,isp_id,title,description,service_type,slots,deadline_at,status,created_at,updated_at FROM isp_vacancies WHERE isp_id=$1 AND status<>'deleted' ORDER BY created_at DESC`, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.ISPVacancy{}
	for rows.Next() {
		v := &models.ISPVacancy{}
		if err := rows.Scan(&v.ID, &v.ISPID, &v.Title, &v.Description, &v.ServiceType, &v.Slots, &v.DeadlineAt, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *VacancyRepository) Close(ispID, id string) error {
	res, err := r.db.Exec(`UPDATE isp_vacancies SET status='closed',updated_at=now() WHERE id=$1 AND isp_id=$2 AND status='open'`, id, ispID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *VacancyRepository) Apply(vacancyID, techID, message string) error {
	_, err := r.db.Exec(`INSERT INTO isp_vacancy_applications(id,vacancy_id,technician_id,message) VALUES($1,$2,$3,$4) ON CONFLICT(vacancy_id,technician_id) DO UPDATE SET message=EXCLUDED.message,status='pending',updated_at=now()`, uuid.NewString(), vacancyID, techID, message)
	return err
}
