package postgres

import (
	"database/sql"
	"encoding/json"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
	"strings"
	"time"
)

type technicianRepo struct{ reader, writer *sql.DB }

func NewTechnicianRepository() (*technicianRepo, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &technicianRepo{reader, writer}, nil
}
func (r *technicianRepo) UpsertProfile(p *models.TechnicianProfile) error {
	tx, err := r.writer.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO technician_profiles(technician_id,bio,experience_years,price_per_hour,is_available,county,town,village,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(technician_id) DO UPDATE SET bio=EXCLUDED.bio,experience_years=EXCLUDED.experience_years,price_per_hour=EXCLUDED.price_per_hour,is_available=EXCLUDED.is_available,county=EXCLUDED.county,town=EXCLUDED.town,village=EXCLUDED.village,updated_at=now()`, p.TechnicianID, p.Bio, p.ExperienceYears, p.PricePerHour, p.IsAvailable, p.County, p.Town, p.Village)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM technician_profile_skills WHERE technician_id=$1`, p.TechnicianID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM technician_skills WHERE technician_id IN (SELECT id FROM technicians WHERE id=$1 OR user_id=$1)`, p.TechnicianID); err != nil {
		return err
	}
	for _, skill := range p.Skills {
		if strings.TrimSpace(skill) != "" {
			if _, err = tx.Exec(`INSERT INTO technician_profile_skills(technician_id,skill) VALUES($1,$2) ON CONFLICT DO NOTHING`, p.TechnicianID, strings.TrimSpace(skill)); err != nil {
				return err
			}
			if _, err = tx.Exec(`INSERT INTO technician_skills(technician_id,skill) SELECT id,$2 FROM technicians WHERE id=$1 OR user_id=$1 ON CONFLICT DO NOTHING`, p.TechnicianID, strings.TrimSpace(skill)); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`UPDATE technicians SET is_available=$1,county=$2,updated_at=now() WHERE id=$3 OR user_id=$3`, p.IsAvailable, p.County, p.TechnicianID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *technicianRepo) GetProfile(id string) (*models.TechnicianProfile, error) {
	p := &models.TechnicianProfile{TechnicianID: id}
	err := r.reader.QueryRow(`SELECT technician_id,bio,experience_years,price_per_hour,is_available,county,town,village,updated_at,COALESCE(identity_status,'unverified'),COALESCE(verified_name,'') FROM technician_profiles WHERE technician_id=$1`, id).Scan(&p.TechnicianID, &p.Bio, &p.ExperienceYears, &p.PricePerHour, &p.IsAvailable, &p.County, &p.Town, &p.Village, &p.UpdatedAt, &p.IdentityStatus, &p.VerifiedName)
	if err != nil {
		return nil, err
	}
	p.Skills, err = r.GetSkills(id)
	return p, err
}

func (r *technicianRepo) GetCompanyLinks(id string) ([]map[string]interface{}, error) {
	rows, err := r.reader.Query(`SELECT l.isp_id,l.company_technician_id,l.started_at,l.ended_at,u.name,l.status FROM isp_technicians l JOIN users u ON u.id=l.isp_id WHERE l.technician_id=$1 ORDER BY l.started_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var ispID, companyID, name, status string
		var start time.Time
		var end *time.Time
		if err := rows.Scan(&ispID, &companyID, &start, &end, &name, &status); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"isp_id": ispID, "company_technician_id": companyID, "company_name": name, "status": status, "started_at": start, "ended_at": end})
	}
	return out, rows.Err()
}
func (r *technicianRepo) AddSkill(id, skill string) error {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return sql.ErrNoRows
	}
	tx, err := r.writer.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='technician' AND status='active')`, id).Scan(&exists); err != nil || !exists {
		return sql.ErrNoRows
	}
	if _, err = tx.Exec(`INSERT INTO technician_profile_skills(technician_id,skill) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, skill); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO technician_skills(technician_id,skill) SELECT id,$2 FROM technicians WHERE id=$1 OR user_id=$1 ON CONFLICT DO NOTHING`, id, skill); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *technicianRepo) RemoveSkill(id, skill string) error {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return sql.ErrNoRows
	}
	tx, err := r.writer.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM technician_profile_skills WHERE technician_id=$1 AND skill=$2`, id, skill); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM technician_skills WHERE technician_id IN (SELECT id FROM technicians WHERE id=$1 OR user_id=$1) AND skill=$2`, id, skill); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *technicianRepo) SetSkills(id string, skills []string) error {
	p, err := r.GetProfile(id)
	if err != nil {
		p = &models.TechnicianProfile{TechnicianID: id}
	}
	p.Skills = skills
	return r.UpsertProfile(p)
}
func (r *technicianRepo) GetSkills(id string) ([]string, error) {
	rows, err := r.reader.Query(`SELECT skill FROM technician_profile_skills WHERE technician_id=$1 ORDER BY skill`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *technicianRepo) SearchTechnicians(skill string, available bool, limit int) ([]*models.TechnicianProfile, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.reader.Query(`SELECT p.technician_id,p.bio,p.experience_years,p.price_per_hour,p.is_available,p.county,p.town,p.village,p.updated_at FROM technician_profiles p WHERE ($1='' OR EXISTS(SELECT 1 FROM technician_profile_skills s WHERE s.technician_id=p.technician_id AND s.skill ILIKE '%'||$1||'%')) AND ($2=false OR p.is_available=true) ORDER BY p.price_per_hour,p.experience_years DESC LIMIT $3`, skill, available, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.TechnicianProfile{}
	for rows.Next() {
		p := &models.TechnicianProfile{}
		if err := rows.Scan(&p.TechnicianID, &p.Bio, &p.ExperienceYears, &p.PricePerHour, &p.IsAvailable, &p.County, &p.Town, &p.Village, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Skills, _ = r.GetSkills(p.TechnicianID)
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *technicianRepo) CreatePost(p *models.TechnicianPost) error {
	media, _ := json.Marshal(p.MediaURLs)
	_, err := r.writer.Exec(`INSERT INTO technician_posts(id,technician_id,title,description,service_type,media_urls,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, p.ID, p.TechnicianID, p.Title, p.Description, p.ServiceType, media, p.Status, p.CreatedAt)
	return err
}
func (r *technicianRepo) UpdatePost(p *models.TechnicianPost) error {
	media, _ := json.Marshal(p.MediaURLs)
	res, err := r.writer.Exec(`UPDATE technician_posts SET title=$1,description=$2,service_type=$3,media_urls=$4,status=$5,updated_at=now() WHERE id=$6 AND technician_id=$7`, p.Title, p.Description, p.ServiceType, media, p.Status, p.ID, p.TechnicianID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *technicianRepo) DeletePost(postID, technicianID string) error {
	res, err := r.writer.Exec(`DELETE FROM technician_posts WHERE id=$1 AND technician_id=$2`, postID, technicianID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *technicianRepo) GetPostByID(id string) (*models.TechnicianPost, error) {
	p := &models.TechnicianPost{}
	var media []byte
	err := r.reader.QueryRow(`SELECT id,technician_id,title,description,service_type,media_urls,status,created_at,updated_at,(SELECT count(*) FROM user_reactions WHERE target_type='technician_post' AND target_id=technician_posts.id AND reaction_type='like') FROM technician_posts WHERE id=$1`, id).Scan(&p.ID, &p.TechnicianID, &p.Title, &p.Description, &p.ServiceType, &media, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.LikeCount)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(media, &p.MediaURLs)
	return p, nil
}
func (r *technicianRepo) GetPostsByTechnician(id string) ([]*models.TechnicianPost, error) {
	return r.posts(`technician_id=$1`, id)
}
func (r *technicianRepo) ListPublishedPosts(kind string, limit int) ([]*models.TechnicianPost, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return r.posts(`status='published' AND ($1='' OR service_type=$1)`, kind, limit)
}
func (r *technicianRepo) posts(where string, args ...interface{}) ([]*models.TechnicianPost, error) {
	rows, err := r.reader.Query(`SELECT id,technician_id,title,description,service_type,media_urls,status,created_at,updated_at,(SELECT count(*) FROM user_reactions WHERE target_type='technician_post' AND target_id=technician_posts.id AND reaction_type='like') FROM technician_posts WHERE `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.TechnicianPost{}
	for rows.Next() {
		p := &models.TechnicianPost{}
		var media []byte
		if err := rows.Scan(&p.ID, &p.TechnicianID, &p.Title, &p.Description, &p.ServiceType, &media, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.LikeCount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(media, &p.MediaURLs)
		out = append(out, p)
	}
	return out, rows.Err()
}

var _ = time.Now
