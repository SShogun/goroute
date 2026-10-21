package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ResponsesContinuationState contains reconstructed input/output items.
type ResponsesContinuationState struct {
	ResponseID string      `json:"response_id"`
	APIKeyID   string      `json:"api_key_id"`
	Model      string      `json:"model"`
	Provider   string      `json:"provider"`
	Input      interface{} `json:"input"`
	Output     interface{} `json:"output"`
	CreatedAt  time.Time   `json:"created_at"`
}

var continuationInit sync.Once

func initContinuationTable(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS responses_continuations (
		response_id TEXT PRIMARY KEY,
		api_key_id TEXT,
		call_id TEXT,
		model TEXT,
		provider TEXT,
		input_json TEXT,
		output_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_resp_cont_key ON responses_continuations(api_key_id, response_id);
	CREATE INDEX IF NOT EXISTS idx_resp_cont_created ON responses_continuations(created_at DESC);
	`
	_, err := db.Exec(schema)
	return err
}

func ensureContinuationInit(db *sql.DB) {
	continuationInit.Do(func() {
		_ = initContinuationTable(db)
	})
}

// SaveContinuationState persists state for Responses API previous_response_id virtualization.
func (s *DBStore) SaveContinuationState(responseID, apiKeyID, callID, model, provider string, input, output interface{}) error {
	if s == nil || s.db == nil || responseID == "" {
		return nil
	}
	ensureContinuationInit(s.db)

	inputJSON, _ := json.Marshal(input)
	outputJSON, _ := json.Marshal(output)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO responses_continuations (response_id, api_key_id, call_id, model, provider, input_json, output_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(response_id) DO UPDATE SET
			input_json = excluded.input_json,
			output_json = excluded.output_json,
			created_at = CURRENT_TIMESTAMP
	`, responseID, apiKeyID, callID, model, provider, string(inputJSON), string(outputJSON))
	return err
}

// GetContinuationState retrieves continuation state scoped by apiKeyID.
func (s *DBStore) GetContinuationState(responseID, apiKeyID string) (*ResponsesContinuationState, error) {
	if s == nil || s.db == nil || responseID == "" {
		return nil, fmt.Errorf("record not found")
	}
	ensureContinuationInit(s.db)

	s.mu.Lock()
	defer s.mu.Unlock()

	var state ResponsesContinuationState
	var inputStr, outputStr string
	var query string
	var args []interface{}

	if apiKeyID != "" {
		query = `SELECT response_id, api_key_id, model, provider, input_json, output_json, created_at
		         FROM responses_continuations WHERE response_id = ? AND (api_key_id = ? OR api_key_id = '')`
		args = []interface{}{responseID, apiKeyID}
	} else {
		query = `SELECT response_id, api_key_id, model, provider, input_json, output_json, created_at
		         FROM responses_continuations WHERE response_id = ?`
		args = []interface{}{responseID}
	}

	err := s.db.QueryRow(query, args...).Scan(
		&state.ResponseID,
		&state.APIKeyID,
		&state.Model,
		&state.Provider,
		&inputStr,
		&outputStr,
		&state.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if inputStr != "" {
		_ = json.Unmarshal([]byte(inputStr), &state.Input)
	}
	if outputStr != "" {
		_ = json.Unmarshal([]byte(outputStr), &state.Output)
	}

	return &state, nil
}

// PruneContinuations removes expired continuation records.
func (s *DBStore) PruneContinuations(maxAgeHours int) (int64, error) {
	if s == nil || s.db == nil || maxAgeHours <= 0 {
		return 0, nil
	}
	ensureContinuationInit(s.db)

	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAgeHours) * time.Hour).UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM responses_continuations WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
