package storage

import (
	"sort"
)

// CallLogFilterOptions represents available filter choices for frontend dropdowns.
type CallLogFilterOptions struct {
	Providers      []string        `json:"providers"`
	Models         []string        `json:"models"`
	Accounts       []string        `json:"accounts"`
	ConfiguredKeys []ConfiguredKey `json:"configuredKeys"`
}

type ConfiguredKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetFilterOptions aggregates all distinct providers, models, and accounts seen in call_logs.
func (s *DBStore) GetFilterOptions() (CallLogFilterOptions, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	opts := CallLogFilterOptions{
		Providers:      []string{},
		Models:         []string{},
		Accounts:       []string{},
		ConfiguredKeys: []ConfiguredKey{},
	}

	if s.db == nil {
		return opts, nil
	}

	// 1. Providers
	if rows, err := s.db.Query(`SELECT DISTINCT provider FROM call_logs WHERE provider IS NOT NULL AND provider != ''`); err == nil {
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err == nil && p != "" {
				opts.Providers = append(opts.Providers, p)
			}
		}
		rows.Close()
	}

	// 2. Models
	if rows, err := s.db.Query(`SELECT DISTINCT model FROM call_logs WHERE model IS NOT NULL AND model != ''`); err == nil {
		for rows.Next() {
			var m string
			if err := rows.Scan(&m); err == nil && m != "" {
				opts.Models = append(opts.Models, m)
			}
		}
		rows.Close()
	}

	// 3. Accounts
	if rows, err := s.db.Query(`SELECT DISTINCT account_id FROM call_logs WHERE account_id IS NOT NULL AND account_id != ''`); err == nil {
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err == nil && a != "" {
				opts.Accounts = append(opts.Accounts, a)
			}
		}
		rows.Close()
	}

	sort.Strings(opts.Providers)
	sort.Strings(opts.Models)
	sort.Strings(opts.Accounts)

	return opts, nil
}
