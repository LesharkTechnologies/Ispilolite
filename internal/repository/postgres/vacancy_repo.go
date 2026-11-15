package postgres

import (
	"database/sql"
	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type VacancyRepository struct{ db *sql.DB }

func NewVacancyRepository() (*VacancyRepository, error) {
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &VacancyRepository{writer}, nil
}
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
	result, err := r.db.Exec(`INSERT INTO isp_vacancy_applications(id,vacancy_id,technician_id,message)
		SELECT $1,v.id,$3,$4 FROM isp_vacancies v WHERE v.id=$2 AND v.status='open' AND (v.deadline_at IS NULL OR v.deadline_at>now())
		ON CONFLICT(vacancy_id,technician_id) DO UPDATE SET message=EXCLUDED.message,status='pending',updated_at=now()`, uuid.NewString(), vacancyID, techID, message)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *VacancyRepository) Applications(ispID, vacancyID string) ([]*models.VacancyApplication, error) {
	rows, err := r.db.Query(`SELECT a.id,a.vacancy_id,a.technician_id,u.name,u.rating,a.message,a.status,a.created_at,a.updated_at
		FROM isp_vacancy_applications a JOIN isp_vacancies v ON v.id=a.vacancy_id JOIN users u ON u.id=a.technician_id
		WHERE a.vacancy_id=$1 AND v.isp_id=$2 ORDER BY a.created_at`, vacancyID, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.VacancyApplication, 0)
	for rows.Next() {
		item := &models.VacancyApplication{}
		if err := rows.Scan(&item.ID, &item.VacancyID, &item.TechnicianID, &item.TechnicianName, &item.Rating, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *VacancyRepository) Decide(ispID, vacancyID, applicationID, status string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var slots int
	if err = tx.QueryRow(`SELECT slots FROM isp_vacancies WHERE id=$1 AND isp_id=$2 AND status='open' FOR UPDATE`, vacancyID, ispID).Scan(&slots); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE isp_vacancy_applications SET status=$1,updated_at=now() WHERE id=$2 AND vacancy_id=$3 AND status='pending'`, status, applicationID, vacancyID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if status == "accepted" {
		var accepted int
		if err = tx.QueryRow(`SELECT count(*) FROM isp_vacancy_applications WHERE vacancy_id=$1 AND status='accepted'`, vacancyID).Scan(&accepted); err != nil {
			return err
		}
		if accepted >= slots {
			if _, err = tx.Exec(`UPDATE isp_vacancies SET status='closed',updated_at=now() WHERE id=$1`, vacancyID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (r *VacancyRepository) Withdraw(vacancyID, technicianID string) error {
	result, err := r.db.Exec(`UPDATE isp_vacancy_applications SET status='withdrawn',updated_at=now() WHERE vacancy_id=$1 AND technician_id=$2 AND status='pending'`, vacancyID, technicianID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
