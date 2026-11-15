package postgres

import (
	"database/sql"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type installationRepo struct {
	dbReader *sql.DB
	dbWriter *sql.DB
}

func NewInstallationRepo() (*installationRepo, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &installationRepo{
		dbReader: reader,
		dbWriter: writer,
	}, nil
}

func (r *installationRepo) CreateInstallation(installation *models.Installation) error {
	query := `INSERT INTO service_requests(id,customer_id,requester_role,request_type,mode,target_isp_id,assigned_isp_id,assigned_technician_id,location_id,service_type,description,preferred_date,budget,status,is_available,created_at,updated_at)
		VALUES($1,$8,'customer','internet',CASE WHEN NULLIF($9,'')::uuid IS NULL THEN 'broadcast' ELSE 'direct' END,NULLIF($9,'')::uuid,NULLIF($9,'')::uuid,NULLIF($10,'')::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$7='open',$11,$12)`
	_, err := r.dbWriter.Exec(
		query,
		installation.ID,
		installation.LocationID,
		installation.ServiceType,
		installation.Description,
		installation.PreferredDate,
		installation.Budget,
		installation.Status,
		installation.ClientID,
		installation.IspID,
		installation.TechnicianID,
		installation.CreatedAt,
		installation.UpdatedAt,
	)
	return err
}

func (r *installationRepo) GetInstallationsByClientID(clientID string) ([]*models.Installation, error) {
	query := installationSelect + ` WHERE customer_id=$1 AND request_type='internet' AND deleted_at IS NULL ORDER BY created_at DESC`
	rows, err := r.dbReader.Query(query, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var installations []*models.Installation
	for rows.Next() {
		var installation models.Installation
		err := rows.Scan(
			&installation.ID,
			&installation.LocationID,
			&installation.ServiceType,
			&installation.Description,
			&installation.PreferredDate,
			&installation.Budget,
			&installation.Status,
			&installation.ClientID,
			&installation.IspID,
			&installation.TechnicianID,
			&installation.CreatedAt,
			&installation.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		installations = append(installations, &installation)
	}

	return installations, nil
}

func (r *installationRepo) GetInstallationsByISPID(ispID string) ([]*models.Installation, error) {
	query := installationSelect + ` WHERE request_type='internet' AND (target_isp_id=$1 OR assigned_isp_id=$1) AND deleted_at IS NULL ORDER BY created_at DESC`
	rows, err := r.dbReader.Query(query, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var installations []*models.Installation
	for rows.Next() {
		var installation models.Installation
		err := rows.Scan(
			&installation.ID,
			&installation.LocationID,
			&installation.ServiceType,
			&installation.Description,
			&installation.PreferredDate,
			&installation.Budget,
			&installation.Status,
			&installation.ClientID,
			&installation.IspID,
			&installation.TechnicianID,
			&installation.CreatedAt,
			&installation.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		installations = append(installations, &installation)
	}

	return installations, nil
}

func (r *installationRepo) GetInstallationByID(installationID string) (*models.Installation, error) {
	query := installationSelect + ` WHERE id=$1 AND request_type='internet' AND deleted_at IS NULL`
	installation := &models.Installation{}
	err := r.dbReader.QueryRow(query, installationID).Scan(
		&installation.ID,
		&installation.LocationID,
		&installation.ServiceType,
		&installation.Description,
		&installation.PreferredDate,
		&installation.Budget,
		&installation.Status,
		&installation.ClientID,
		&installation.IspID,
		&installation.TechnicianID,
		&installation.CreatedAt,
		&installation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return installation, nil
}

func (r *installationRepo) UpdateInstallation(installation *models.Installation) error {
	allowed := map[string]string{"accepted": "open,viewed,quoted", "assigned": "open,accepted,assigned", "scheduled": "open,accepted,assigned", "in_progress": "accepted,assigned,scheduled", "completed": "in_progress", "cancelled": "open,viewed,quoted,accepted,assigned,scheduled,in_progress"}[installation.Status]
	if allowed == "" {
		return sql.ErrNoRows
	}
	completionGate := ""
	if installation.Status == "completed" {
		completionGate = " AND EXISTS(SELECT 1 FROM installation_proofs p WHERE p.request_id=service_requests.id AND p.customer_confirmed_at IS NOT NULL AND jsonb_array_length(p.photo_urls)>=1 AND p.device_serial<>'' AND p.checklist <> '{}'::jsonb)"
	}
	result, err := r.dbWriter.Exec(`UPDATE service_requests SET status=$1,assigned_isp_id=$5,assigned_technician_id=COALESCE(NULLIF($2,'')::uuid,assigned_technician_id),is_available=false,completed_at=CASE WHEN $1='completed' THEN now() ELSE completed_at END,updated_at=$3 WHERE id=$4 AND (target_isp_id=$5 OR assigned_isp_id=$5) AND request_type='internet' AND status=ANY(string_to_array($6,',')) AND deleted_at IS NULL`+completionGate, installation.Status, installation.TechnicianID, installation.UpdatedAt, installation.ID, installation.IspID, allowed)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	if installation.Status == "completed" {
		_, _ = r.dbWriter.Exec(`UPDATE technicians SET jobs_done=jobs_done+1,updated_at=now() WHERE user_id=(SELECT assigned_technician_id FROM service_requests WHERE id=$1)`, installation.ID)
		_, _ = r.dbWriter.Exec(`UPDATE work_engagements SET status='completed',ended_at=now() WHERE request_id=$1 AND status='active'`, installation.ID)
	}
	return nil
}

const installationSelect = `SELECT id,COALESCE(location_id::text,''),service_type,description,preferred_date,budget,status,customer_id::text,COALESCE(assigned_isp_id::text,target_isp_id::text,''),COALESCE(assigned_technician_id::text,target_technician_id::text,''),created_at,updated_at FROM service_requests`
