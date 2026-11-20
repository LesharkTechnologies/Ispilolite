package postgres

import (
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type JobRepository struct {
	reader *sqlx.DB
	writer *sqlx.DB
}

func NewJobRequestRepo() (*JobRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &JobRepository{
		reader: sqlx.NewDb(reader, "postgres"),
		writer: sqlx.NewDb(writer, "postgres"),
	}, nil
}

const jobColumns = `id,customer_id,requester_role,request_type,mode,target_isp_id,target_technician_id,assigned_isp_id,assigned_technician_id,location_id,county,town,village,service_type,speed_mbps,description,photo_urls,budget,preferred_date,status,is_available,created_at,updated_at,deleted_at`
const jobSelectColumns = `id,customer_id,requester_role,request_type,mode,COALESCE(target_isp_id::text,''),COALESCE(target_technician_id::text,''),COALESCE(assigned_isp_id::text,''),COALESCE(assigned_technician_id::text,''),COALESCE(location_id::text,''),county,town,village,service_type,COALESCE(speed_mbps,0),description,COALESCE(photo_urls,'{}'::text[]),budget,preferred_date,status,is_available,created_at,updated_at,deleted_at,warranty_days,warranty_expires_at,response_due_at,quote_due_at,installation_due_at,viewed_at,accepted_at,scheduled_at,completed_at,loss_reason`

func scanJob(row interface{ Scan(...interface{}) error }) (*models.JobRequest, error) {
	job := &models.JobRequest{}
	err := row.Scan(&job.ID, &job.CustomerID, &job.RequesterRole, &job.RequestType, &job.Mode, &job.TargetISPID, &job.TechnicianID, &job.AssignedISPID, &job.AssignedTechnicianID, &job.LocationID, &job.County, &job.Town, &job.Village, &job.ServiceType, &job.SpeedMbps, &job.Description, pq.Array(&job.PhotoURLs), &job.Budget, &job.PreferredDate, &job.Status, &job.IsAvailable, &job.CreatedAt, &job.UpdatedAt, &job.DeletedAt, &job.WarrantyDays, &job.WarrantyExpiresAt, &job.ResponseDueAt, &job.QuoteDueAt, &job.InstallationDueAt, &job.ViewedAt, &job.AcceptedAt, &job.ScheduledAt, &job.CompletedAt, &job.LossReason)
	return job, err
}

func (r *JobRepository) CreateJobRequest(job *models.JobRequest) error {
	_, err := r.writer.Exec(`INSERT INTO service_requests (`+jobColumns+`) VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,NULLIF($10,'')::uuid,$11,$12,$13,$14,NULLIF($15,0),$16,$17,$18,$19,$20,$21,$22,$23,$24)`, job.ID, job.CustomerID, job.RequesterRole, job.RequestType, job.Mode, job.TargetISPID, job.TechnicianID, job.AssignedISPID, job.AssignedTechnicianID, job.LocationID, job.County, job.Town, job.Village, job.ServiceType, job.SpeedMbps, job.Description, pq.Array(job.PhotoURLs), job.Budget, job.PreferredDate, job.Status, job.IsAvailable, job.CreatedAt, job.UpdatedAt, job.DeletedAt)
	return err
}

func (r *JobRepository) GetJobRequestByID(id string) (*models.JobRequest, error) {
	return scanJob(r.reader.QueryRow(`SELECT `+jobSelectColumns+` FROM service_requests WHERE id=$1 AND deleted_at IS NULL`, id))
}

func (r *JobRepository) ListForCustomer(id, status string) ([]*models.JobRequest, error) {
	return r.list(`customer_id=$1 AND ($2='' OR status=$2)`, id, status)
}
func (r *JobRepository) ListForTechnician(id, status string) ([]*models.JobRequest, error) {
	return r.list(`(target_technician_id=$1 OR assigned_technician_id=$1 OR EXISTS (SELECT 1 FROM service_request_applications a WHERE a.request_id=service_requests.id AND a.applicant_id=$1)) AND ($2='' OR status=$2)`, id, status)
}
func (r *JobRepository) ListForISP(id, status string) ([]*models.JobRequest, error) {
	return r.list(`(target_isp_id=$1 OR assigned_isp_id=$1 OR (customer_id=$1 AND requester_role='isp') OR EXISTS (SELECT 1 FROM service_request_applications a WHERE a.request_id=service_requests.id AND a.applicant_id=$1)) AND ($2='' OR status=$2)`, id, status)
}
func (r *JobRepository) GetForParticipant(requestID, participantID string) (*models.JobRequest, error) {
	return scanJob(r.reader.QueryRow(`SELECT `+jobSelectColumns+` FROM service_requests WHERE id=$1 AND deleted_at IS NULL AND (customer_id=$2 OR target_isp_id=$2 OR assigned_isp_id=$2 OR target_technician_id=$2 OR assigned_technician_id=$2)`, requestID, participantID))
}
func (r *JobRepository) ListAvailable(county, town, serviceType string) ([]*models.JobRequest, error) {
	return r.list(`mode='broadcast' AND is_available=true AND status='open' AND ($1='' OR lower(county)=lower($1)) AND ($2='' OR lower(town)=lower($2)) AND ($3='' OR service_type=$3)`, county, town, serviceType)
}
func (r *JobRepository) list(where string, args ...interface{}) ([]*models.JobRequest, error) {
	rows, err := r.reader.Query(`SELECT `+jobSelectColumns+` FROM service_requests WHERE deleted_at IS NULL AND `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.JobRequest{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r *JobRepository) ApplyToJob(a *models.JobApplication) error {
	result, err := r.writer.Exec(`INSERT INTO service_request_applications (id,request_id,applicant_id,applicant_role,message,proposed_price,status,created_at,updated_at) SELECT $1,$2,$3,$4,$5,$6,'pending',now(),now() FROM service_requests WHERE id=$2 AND customer_id<>$3 AND mode='broadcast' AND status='open' AND is_available=true AND deleted_at IS NULL ON CONFLICT (request_id,applicant_id) DO UPDATE SET message=EXCLUDED.message,proposed_price=EXCLUDED.proposed_price,status='pending',updated_at=now()`, a.ID, a.RequestID, a.ApplicantID, a.ApplicantRole, a.Message, a.ProposedPrice)
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
	return nil
}

func (r *JobRepository) ListApplications(requestID, customerID string) ([]*models.JobApplication, error) {
	rows, err := r.reader.Query(`SELECT a.id,a.request_id,a.applicant_id,a.applicant_role,a.message,a.proposed_price,a.status,a.created_at,a.updated_at,COALESCE(u.name,''),COALESCE(u.rating,0) FROM service_request_applications a JOIN service_requests j ON j.id=a.request_id JOIN users u ON u.id=a.applicant_id WHERE a.request_id=$1 AND j.customer_id=$2 ORDER BY a.created_at`, requestID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.JobApplication{}
	for rows.Next() {
		a := &models.JobApplication{}
		if err := rows.Scan(&a.ID, &a.RequestID, &a.ApplicantID, &a.ApplicantRole, &a.Message, &a.ProposedPrice, &a.Status, &a.CreatedAt, &a.UpdatedAt, &a.ApplicantName, &a.Rating); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *JobRepository) AssignApplication(requestID, customerID, applicationID string) (*models.JobRequest, error) {
	tx, err := r.writer.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var applicant, role string
	if err = tx.QueryRow(`SELECT applicant_id,applicant_role FROM service_request_applications WHERE id=$1 AND request_id=$2 AND status='pending'`, applicationID, requestID).Scan(&applicant, &role); err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE service_requests SET status='assigned',is_available=false,assigned_technician_id=CASE WHEN $3='technician' THEN $1 ELSE assigned_technician_id END,assigned_isp_id=CASE WHEN $3='isp' THEN $1 ELSE assigned_isp_id END,updated_at=now() WHERE id=$2 AND customer_id=$4 AND is_available=true`, applicant, requestID, role, customerID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	if _, err = tx.Exec(`UPDATE service_request_applications SET status=CASE WHEN id=$1 THEN 'accepted' ELSE 'rejected' END,updated_at=now() WHERE request_id=$2`, applicationID, requestID); err != nil {
		return nil, err
	}
	_, _ = tx.Exec(`UPDATE matches SET status=CASE WHEN provider_id=$1 THEN 'assigned' ELSE 'declined' END,responded_at=now() WHERE request_id=$2 AND status IN ('notified','seen')`, applicant, requestID)
	if role == "technician" {
		if _, err = tx.Exec(`INSERT INTO work_engagements(id,customer_id,isp_id,technician_id,request_id,status)
			SELECT gen_random_uuid(),customer_id,(SELECT isp_id FROM users WHERE id=$1),$1,id,'active' FROM service_requests WHERE id=$2
			AND NOT EXISTS(SELECT 1 FROM work_engagements WHERE request_id=$2 AND technician_id=$1 AND status='active')`, applicant, requestID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return scanJob(r.writer.QueryRow(`SELECT `+jobSelectColumns+` FROM service_requests WHERE id=$1 AND deleted_at IS NULL`, requestID))
}

func (r *JobRepository) SetAvailability(id, customerID string, available bool) error {
	result, err := r.writer.Exec(`UPDATE service_requests SET is_available=$1,status=CASE WHEN $1 THEN 'open' ELSE 'unavailable' END,updated_at=now() WHERE id=$2 AND customer_id=$3 AND deleted_at IS NULL`, available, id, customerID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *JobRepository) DeleteJobRequest(id, customerID string) error {
	result, err := r.writer.Exec(`UPDATE service_requests SET deleted_at=now(),status='deleted',is_available=false,updated_at=now() WHERE id=$1 AND customer_id=$2 AND deleted_at IS NULL`, id, customerID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *JobRepository) UpdateJobStatus(id, actor, status string) error {
	if status == "rejected" {
		result, err := r.writer.Exec(`UPDATE service_requests SET status=CASE WHEN mode='broadcast' THEN 'open' ELSE 'cancelled' END,is_available=(mode='broadcast'),
			assigned_technician_id=CASE WHEN assigned_technician_id=$1 THEN NULL ELSE assigned_technician_id END,
			assigned_isp_id=CASE WHEN assigned_isp_id=$1 THEN NULL ELSE assigned_isp_id END,updated_at=now()
			WHERE id=$2 AND status IN ('open','assigned') AND (target_technician_id=$1 OR target_isp_id=$1 OR assigned_technician_id=$1 OR assigned_isp_id=$1) AND deleted_at IS NULL`, actor, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	}
	allowedFrom := map[string]string{"accepted": "open,viewed,quoted,assigned,scheduled", "in_progress": "accepted,scheduled", "completed": "in_progress"}[status]
	if allowedFrom == "" {
		return sql.ErrNoRows
	}
	completionGate := ""
	if status == "completed" {
		completionGate = " AND EXISTS(SELECT 1 FROM installation_proofs p WHERE p.request_id=service_requests.id AND p.customer_confirmed_at IS NOT NULL AND jsonb_array_length(p.photo_urls)>=1 AND p.device_serial<>'' AND p.checklist <> '{}'::jsonb)"
	}
	result, err := r.writer.Exec(`UPDATE service_requests SET status=$1,is_available=false,completed_at=CASE WHEN $1='completed' THEN now() ELSE completed_at END,updated_at=now()
		WHERE id=$2 AND status=ANY(string_to_array($4,',')) AND (target_technician_id=$3 OR target_isp_id=$3 OR assigned_technician_id=$3 OR assigned_isp_id=$3) AND deleted_at IS NULL`+completionGate, status, id, actor, allowedFrom)
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
	if status == "accepted" {
		_, _ = r.writer.Exec(`INSERT INTO work_engagements(id,customer_id,isp_id,technician_id,request_id,status)
			SELECT gen_random_uuid(),customer_id,COALESCE(assigned_isp_id,target_isp_id),COALESCE(assigned_technician_id,target_technician_id),id,'active'
			FROM service_requests WHERE id=$1 AND COALESCE(assigned_technician_id,target_technician_id) IS NOT NULL
			AND NOT EXISTS(SELECT 1 FROM work_engagements WHERE request_id=$1 AND technician_id=COALESCE(service_requests.assigned_technician_id,service_requests.target_technician_id) AND status='active')`, id)
	}
	if status == "completed" {
		_, _ = r.writer.Exec(`UPDATE technicians SET jobs_done=jobs_done+1,updated_at=now() WHERE user_id=(SELECT assigned_technician_id FROM service_requests WHERE id=$1)`, id)
		_, _ = r.writer.Exec(`UPDATE work_engagements SET status='completed',ended_at=now() WHERE request_id=$1 AND status='active'`, id)
	}
	return nil
}

func (r *JobRepository) ActivateWarranty(id, actor string, days int) error {
	result, err := r.writer.Exec(`UPDATE service_requests SET warranty_days=$1,
		warranty_expires_at=CASE WHEN $1>0 THEN now()+($1||' days')::interval ELSE NULL END,updated_at=now()
		WHERE id=$2 AND status='completed' AND (target_isp_id=$3 OR assigned_isp_id=$3 OR target_technician_id=$3 OR assigned_technician_id=$3)`, days, id, actor)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *JobRepository) AssignTechnician(requestID, ispID, technicianID string) (*models.JobRequest, error) {
	tx, err := r.writer.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var ok bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users u JOIN isp_technicians it ON it.technician_id=u.id AND it.isp_id=$2 AND it.status='active' WHERE u.id=$1 AND u.role='technician' AND u.status='active' AND u.isp_id=$2)`, technicianID, ispID).Scan(&ok); err != nil || !ok {
		return nil, sql.ErrNoRows
	}
	res, err := tx.Exec(`UPDATE service_requests SET assigned_isp_id=$1,assigned_technician_id=$2,status='scheduled',is_available=false,updated_at=now()
		WHERE id=$3 AND deleted_at IS NULL AND ((target_isp_id=$1 AND status IN ('open','accepted','assigned')) OR (assigned_isp_id=$1 AND status IN ('accepted','assigned')))`, ispID, technicianID, requestID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	_, err = tx.Exec(`INSERT INTO work_engagements(id,customer_id,isp_id,technician_id,request_id,status)
		SELECT gen_random_uuid(),customer_id,$1,$2,id,'active' FROM service_requests WHERE id=$3
		AND NOT EXISTS(SELECT 1 FROM work_engagements WHERE request_id=$3 AND technician_id=$2 AND status='active')`, ispID, technicianID, requestID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return scanJob(r.writer.QueryRow(`SELECT `+jobSelectColumns+` FROM service_requests WHERE id=$1 AND deleted_at IS NULL`, requestID))
}

func (r *JobRepository) TerminateJob(requestID, actorID, reason string, endedAt time.Time) error {
	res, err := r.writer.Exec(`UPDATE service_requests SET status='cancelled',is_available=false,terminated_at=$1,termination_reason=$2,updated_at=now() WHERE id=$3 AND (customer_id=$4 OR target_isp_id=$4 OR assigned_isp_id=$4 OR target_technician_id=$4 OR assigned_technician_id=$4) AND deleted_at IS NULL`, endedAt, reason, requestID, actorID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	_, _ = r.writer.Exec(`UPDATE work_engagements SET status='terminated',ended_at=$2,termination_reason=$3 WHERE request_id=$1 AND status='active'`, requestID, endedAt, reason)
	return nil
}
