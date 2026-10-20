package storage

import (
	"fmt"
	"sync"
	"time"
)

// BatchRecord matches OpenAI / OmniRoute batch jobs.
type BatchRecord struct {
	ID               string    `json:"id"`
	Endpoint         string    `json:"endpoint"`
	Status           string    `json:"status"` // "validating", "in_progress", "completed", "failed", "cancelled"
	InputFileID      string    `json:"inputFileId"`
	OutputFileID     string    `json:"outputFileId,omitempty"`
	ErrorFileID      string    `json:"errorFileId,omitempty"`
	TotalRequests    int       `json:"totalRequests"`
	CompletedRequests int      `json:"completedRequests"`
	FailedRequests   int       `json:"failedRequests"`
	CreatedAt        time.Time `json:"createdAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

// AuditLogRecord matches OmniRoute's administrative compliance action record.
type AuditLogRecord struct {
	ID           string    `json:"id"`
	Action       string    `json:"action"`
	Actor        string    `json:"actor"`
	Target       string    `json:"target,omitempty"`
	ResourceType string    `json:"resourceType,omitempty"`
	Status       string    `json:"status"`
	Details      string    `json:"details,omitempty"`
	IPAddress    string    `json:"ipAddress,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// EnterpriseStore handles batches, audit logs, and circuit breaker metrics.
type EnterpriseStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalEnterpriseStore *EnterpriseStore
	enterpriseOnce        sync.Once
)

func GetEnterpriseStore() *EnterpriseStore {
	enterpriseOnce.Do(func() {
		globalEnterpriseStore = &EnterpriseStore{
			db: GetDB(),
		}
		globalEnterpriseStore.ensureTables()
	})
	return globalEnterpriseStore
}

func (s *EnterpriseStore) ensureTables() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS batches (
			id TEXT PRIMARY KEY,
			endpoint TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'validating',
			input_file_id TEXT NOT NULL,
			output_file_id TEXT,
			error_file_id TEXT,
			total_requests INTEGER DEFAULT 0,
			completed_requests INTEGER DEFAULT 0,
			failed_requests INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);

		CREATE INDEX IF NOT EXISTS idx_batches_status ON batches(status);

		CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT 'admin',
			target TEXT,
			resource_type TEXT,
			status TEXT NOT NULL DEFAULT 'success',
			details TEXT,
			ip_address TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
	`)
}

// ListBatches returns registered batch jobs.
func (s *EnterpriseStore) ListBatches(limit int) ([]BatchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var batches []BatchRecord
	if s.db == nil || s.db.db == nil {
		return batches, nil
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.db.Query(`
		SELECT id, endpoint, status, input_file_id, output_file_id, error_file_id, total_requests, completed_requests, failed_requests, created_at, completed_at
		FROM batches
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var b BatchRecord
		var outID, errID, compStr *string
		var crStr string
		if err := rows.Scan(
			&b.ID,
			&b.Endpoint,
			&b.Status,
			&b.InputFileID,
			&outID,
			&errID,
			&b.TotalRequests,
			&b.CompletedRequests,
			&b.FailedRequests,
			&crStr,
			&compStr,
		); err == nil {
			if outID != nil {
				b.OutputFileID = *outID
			}
			if errID != nil {
				b.ErrorFileID = *errID
			}
			b.CreatedAt, _ = time.Parse(time.RFC3339, crStr)
			if compStr != nil {
				t, _ := time.Parse(time.RFC3339, *compStr)
				b.CompletedAt = &t
			}
			batches = append(batches, b)
		}
	}
	return batches, nil
}

// CreateBatch inserts a new batch execution job.
func (s *EnterpriseStore) CreateBatch(endpoint, inputFileID string, totalRequests int) (BatchRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := BatchRecord{
		ID:            fmt.Sprintf("batch_%d", time.Now().UnixNano()),
		Endpoint:      endpoint,
		Status:        "in_progress",
		InputFileID:   inputFileID,
		TotalRequests: totalRequests,
		CreatedAt:     time.Now().UTC(),
	}

	if s.db == nil || s.db.db == nil {
		return b, fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT INTO batches (id, endpoint, status, input_file_id, total_requests, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, b.ID, b.Endpoint, b.Status, b.InputFileID, b.TotalRequests, b.CreatedAt.Format(time.RFC3339))
	return b, err
}

// RecordAudit logs an administrative event into compliance audit_logs.
func (s *EnterpriseStore) RecordAudit(rec AuditLogRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("aud_%d", time.Now().UnixNano())
	}
	if rec.Actor == "" {
		rec.Actor = "system"
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	_, err := s.db.db.Exec(`
		INSERT INTO audit_logs (id, action, actor, target, resource_type, status, details, ip_address, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.Action, rec.Actor, rec.Target, rec.ResourceType, rec.Status, rec.Details, rec.IPAddress, rec.Timestamp.Format(time.RFC3339))
	return err
}

// QueryAuditLogs retrieves audit logs with limit/offset.
func (s *EnterpriseStore) QueryAuditLogs(limit, offset int) ([]AuditLogRecord, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var logs []AuditLogRecord
	if s.db == nil || s.db.db == nil {
		return logs, 0, nil
	}
	if limit <= 0 {
		limit = 50
	}

	var total int
	_ = s.db.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&total)

	rows, err := s.db.db.Query(`
		SELECT id, action, actor, target, resource_type, status, details, ip_address, timestamp
		FROM audit_logs
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var a AuditLogRecord
		var targ, resT, det, ip *string
		var tsStr string
		if err := rows.Scan(&a.ID, &a.Action, &a.Actor, &targ, &resT, &a.Status, &det, &ip, &tsStr); err == nil {
			if targ != nil {
				a.Target = *targ
			}
			if resT != nil {
				a.ResourceType = *resT
			}
			if det != nil {
				a.Details = *det
			}
			if ip != nil {
				a.IPAddress = *ip
			}
			a.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
			logs = append(logs, a)
		}
	}
	return logs, total, nil
}
