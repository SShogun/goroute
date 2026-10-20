package storage

import (
	"fmt"
	"time"
)

// PurgeResult holds metrics from a purge operation.
type PurgeResult struct {
	Deleted          int64 `json:"deleted"`
	DeletedArtifacts int64 `json:"deletedArtifacts"`
	Errors           int   `json:"errors"`
}

// PurgeCallLogs deletes call logs older than cutoff (or all logs if cutoff is zero).
func (s *DBStore) PurgeCallLogs(cutoff time.Time) (PurgeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := PurgeResult{}
	if s.db == nil {
		return res, fmt.Errorf("database not initialized")
	}

	var query string
	var args []any

	if cutoff.IsZero() {
		query = "DELETE FROM call_logs"
	} else {
		query = "DELETE FROM call_logs WHERE timestamp < ?"
		args = append(args, cutoff.Format(time.RFC3339))
	}

	result, err := s.db.Exec(query, args...)
	if err != nil {
		res.Errors++
		return res, err
	}

	rowsDeleted, _ := result.RowsAffected()
	res.Deleted = rowsDeleted

	return res, nil
}
