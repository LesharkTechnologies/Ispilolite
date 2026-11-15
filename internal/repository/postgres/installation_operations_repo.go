package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type InstallationOperationsRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewInstallationOperationsRepository() (*InstallationOperationsRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &InstallationOperationsRepository{reader: reader, writer: writer}, nil
}

func (r *InstallationOperationsRepository) CheckServiceability(locationID, county, town, estate, building string) ([]*models.ServiceabilityRecord, error) {
	rows, err := r.reader.Query(`SELECT s.id,s.isp_id,COALESCE(u.name,''),COALESCE(s.location_id::text,''),s.county,s.town,s.estate,s.building,s.technology,s.status,s.estimated_days,s.reason,s.active,s.created_at,s.updated_at
		FROM isp_serviceability s JOIN users u ON u.id=s.isp_id
		WHERE s.active=true AND u.status='active'
		AND (NULLIF($1,'') IS NULL OR s.location_id=NULLIF($1,'')::uuid)
		AND ($2='' OR lower(s.county)=lower($2))
		AND ($3='' OR lower(s.town)=lower($3))
		AND ($4='' OR lower(s.estate)=lower($4))
		AND ($5='' OR lower(s.building)=lower($5))
		ORDER BY CASE s.status WHEN 'serviceable' THEN 0 WHEN 'likely_serviceable' THEN 1 WHEN 'survey_required' THEN 2 ELSE 3 END,s.estimated_days,s.updated_at DESC`, locationID, county, town, estate, building)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.ServiceabilityRecord, 0)
	for rows.Next() {
		item := &models.ServiceabilityRecord{}
		if err := rows.Scan(&item.ID, &item.ISPID, &item.ISPName, &item.LocationID, &item.County, &item.Town, &item.Estate, &item.Building, &item.Technology, &item.Status, &item.EstimatedDays, &item.Reason, &item.Active, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *InstallationOperationsRepository) UpsertServiceability(item *models.ServiceabilityRecord) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	item.County = strings.TrimSpace(item.County)
	item.Town = strings.TrimSpace(item.Town)
	item.Estate = strings.TrimSpace(item.Estate)
	item.Building = strings.TrimSpace(item.Building)
	item.Technology = strings.TrimSpace(item.Technology)
	item.Status = strings.TrimSpace(item.Status)
	if item.Status == "" {
		item.Status = "serviceable"
	}
	if item.EstimatedDays < 0 || item.EstimatedDays > 365 {
		return errors.New("estimated_days must be between 0 and 365")
	}
	var existingID string
	err := r.reader.QueryRow(`SELECT id FROM isp_serviceability WHERE isp_id=$1 AND COALESCE(location_id::text,'')=COALESCE(NULLIF($2,'')::text,'') AND lower(county)=lower($3) AND lower(town)=lower($4) AND lower(estate)=lower($5) AND lower(building)=lower($6)`, item.ISPID, item.LocationID, item.County, item.Town, item.Estate, item.Building).Scan(&existingID)
	if err == nil {
		item.ID = existingID
		_, err = r.writer.Exec(`UPDATE isp_serviceability SET technology=$1,status=$2,estimated_days=$3,reason=$4,active=true,updated_at=now() WHERE id=$5 AND isp_id=$6`, item.Technology, item.Status, item.EstimatedDays, item.Reason, existingID, item.ISPID)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = r.writer.Exec(`INSERT INTO isp_serviceability(id,isp_id,location_id,county,town,estate,building,technology,status,estimated_days,reason,active)
		VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,true)`, item.ID, item.ISPID, item.LocationID, item.County, item.Town, item.Estate, item.Building, item.Technology, item.Status, item.EstimatedDays, item.Reason)
	return err
}

func (r *InstallationOperationsRepository) ListServiceability(ispID string) ([]*models.ServiceabilityRecord, error) {
	rows, err := r.reader.Query(`SELECT s.id,s.isp_id,COALESCE(u.name,''),COALESCE(s.location_id::text,''),s.county,s.town,s.estate,s.building,s.technology,s.status,s.estimated_days,s.reason,s.active,s.created_at,s.updated_at
		FROM isp_serviceability s JOIN users u ON u.id=s.isp_id WHERE s.isp_id=$1 ORDER BY s.updated_at DESC`, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.ServiceabilityRecord, 0)
	for rows.Next() {
		item := &models.ServiceabilityRecord{}
		if err := rows.Scan(&item.ID, &item.ISPID, &item.ISPName, &item.LocationID, &item.County, &item.Town, &item.Estate, &item.Building, &item.Technology, &item.Status, &item.EstimatedDays, &item.Reason, &item.Active, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *InstallationOperationsRepository) DeleteServiceability(id, ispID string) error {
	result, err := r.writer.Exec(`UPDATE isp_serviceability SET active=false,updated_at=now() WHERE id=$1 AND isp_id=$2`, id, ispID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *InstallationOperationsRepository) AddWaitlist(item *models.ServiceabilityWaitlist) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	_, err := r.writer.Exec(`INSERT INTO serviceability_waitlist(id,customer_id,isp_id,location_id,county,town,estate,building,service_type) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)`, item.ID, item.CustomerID, item.ISPID, item.LocationID, item.County, item.Town, item.Estate, item.Building, item.ServiceType)
	return err
}

func (r *InstallationOperationsRepository) ListWaitlist(ispID string) ([]*models.ServiceabilityWaitlist, error) {
	rows, err := r.reader.Query(`SELECT id,COALESCE(customer_id::text,''),COALESCE(isp_id::text,''),COALESCE(location_id::text,''),county,town,estate,building,service_type,created_at FROM serviceability_waitlist WHERE isp_id=$1 OR (isp_id IS NULL AND EXISTS(SELECT 1 FROM isp_serviceability s WHERE s.isp_id=$1 AND lower(s.county)=lower(serviceability_waitlist.county) AND lower(s.town)=lower(serviceability_waitlist.town))) ORDER BY created_at DESC`, ispID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.ServiceabilityWaitlist, 0)
	for rows.Next() {
		item := &models.ServiceabilityWaitlist{}
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.ISPID, &item.LocationID, &item.County, &item.Town, &item.Estate, &item.Building, &item.ServiceType, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *InstallationOperationsRepository) SaveProof(item *models.InstallationProof) error {
	photos, err := json.Marshal(item.PhotoURLs)
	if err != nil {
		return err
	}
	checklist, err := json.Marshal(item.Checklist)
	if err != nil {
		return err
	}
	result, err := r.writer.Exec(`INSERT INTO installation_proofs(id,request_id,technician_id,check_in_at,check_out_at,latitude,longitude,photo_urls,checklist,download_mbps,upload_mbps,latency_ms,packet_loss_percent,device_serial,notes,status)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'submitted'
		FROM service_requests j WHERE j.id=$2 AND (j.assigned_technician_id=$3 OR j.target_technician_id=$3)
		ON CONFLICT(request_id) DO UPDATE SET technician_id=EXCLUDED.technician_id,check_in_at=EXCLUDED.check_in_at,check_out_at=EXCLUDED.check_out_at,latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,photo_urls=EXCLUDED.photo_urls,checklist=EXCLUDED.checklist,download_mbps=EXCLUDED.download_mbps,upload_mbps=EXCLUDED.upload_mbps,latency_ms=EXCLUDED.latency_ms,packet_loss_percent=EXCLUDED.packet_loss_percent,device_serial=EXCLUDED.device_serial,notes=EXCLUDED.notes,status='submitted',updated_at=now()`, item.ID, item.RequestID, item.TechnicianID, item.CheckInAt, item.CheckOutAt, item.Latitude, item.Longitude, photos, checklist, item.DownloadMbps, item.UploadMbps, item.LatencyMs, item.PacketLossPercent, item.DeviceSerial, item.Notes)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *InstallationOperationsRepository) GetProof(requestID, actorID string) (*models.InstallationProof, error) {
	item := &models.InstallationProof{}
	var photos, checklist []byte
	var confirmed sql.NullTime
	err := r.reader.QueryRow(`SELECT p.id,p.request_id,p.technician_id,p.check_in_at,p.check_out_at,p.latitude,p.longitude,p.photo_urls,p.checklist,p.download_mbps,p.upload_mbps,p.latency_ms,p.packet_loss_percent,p.device_serial,p.notes,p.status,p.customer_confirmed_at,p.created_at,p.updated_at
		FROM installation_proofs p JOIN service_requests j ON j.id=p.request_id
		WHERE p.request_id=$1 AND (j.customer_id=$2 OR j.target_isp_id=$2 OR j.assigned_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_technician_id=$2)`, requestID, actorID).Scan(&item.ID, &item.RequestID, &item.TechnicianID, &item.CheckInAt, &item.CheckOutAt, &item.Latitude, &item.Longitude, &photos, &checklist, &item.DownloadMbps, &item.UploadMbps, &item.LatencyMs, &item.PacketLossPercent, &item.DeviceSerial, &item.Notes, &item.Status, &confirmed, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(photos, &item.PhotoURLs)
	_ = json.Unmarshal(checklist, &item.Checklist)
	if confirmed.Valid {
		item.CustomerConfirmedAt = &confirmed.Time
	}
	return item, nil
}

func (r *InstallationOperationsRepository) ConfirmProof(requestID, customerID string) error {
	result, err := r.writer.Exec(`UPDATE installation_proofs p SET customer_confirmed_at=now(),customer_confirmed_by=$2,status='approved',updated_at=now() FROM service_requests j WHERE p.request_id=$1 AND j.id=p.request_id AND j.customer_id=$2 AND p.status IN ('submitted','approved')`, requestID, customerID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ProofReady is used by completion gates. It intentionally validates only
// durable customer approval and required evidence, leaving ISP-specific quality
// thresholds configurable in future migrations.
func (r *InstallationOperationsRepository) ProofReady(requestID string) (bool, error) {
	var ready bool
	err := r.reader.QueryRow(`SELECT EXISTS(SELECT 1 FROM installation_proofs WHERE request_id=$1 AND customer_confirmed_at IS NOT NULL AND jsonb_array_length(photo_urls)>=1 AND device_serial<>'' AND checklist<> '{}'::jsonb)`, requestID).Scan(&ready)
	return ready, err
}
