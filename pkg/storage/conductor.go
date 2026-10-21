package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ConductorTask represents an autonomous coding agent orchestration job.
type ConductorTask struct {
	ID        string    `json:"id"`
	RepoURL   string    `json:"repoUrl"`
	Prompt    string    `json:"prompt"`
	BaseRef   string    `json:"baseRef,omitempty"`
	Status    string    `json:"status"` // "queued", "running", "completed", "failed"
	Model     string    `json:"model,omitempty"`
	CLI       string    `json:"cli,omitempty"` // "opencode", "claude", "aider", "codex"
	CreatedAt time.Time `json:"createdAt"`
}

// FleetAgent represents an active worker node in the Conductor fleet.
type FleetAgent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CLI       string    `json:"cli"`
	Status    string    `json:"status"` // "idle", "busy", "offline"
	TaskID    string    `json:"taskId,omitempty"`
	LastSeen  time.Time `json:"lastSeen"`
}

// SkillRecord represents an agent tool skill in the skills registry.
type SkillRecord struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Mode        string   `json:"mode"` // "on", "off", "auto"
	Tags        []string `json:"tags"`
	Source      string   `json:"source"` // "local", "skillsmp", "skillssh"
	Enabled     bool     `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ConductorStore handles conductor tasks, fleet nodes, and skills.
type ConductorStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalConductorStore *ConductorStore
	conductorOnce        sync.Once
)

func GetConductorStore() *ConductorStore {
	conductorOnce.Do(func() {
		globalConductorStore = &ConductorStore{
			db: GetDB(),
		}
		globalConductorStore.ensureTables()
	})
	return globalConductorStore
}

func (s *ConductorStore) ensureTables() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS conductor_tasks (
			id TEXT PRIMARY KEY,
			repo_url TEXT NOT NULL,
			prompt TEXT NOT NULL,
			base_ref TEXT,
			status TEXT NOT NULL DEFAULT 'queued',
			model TEXT,
			cli TEXT DEFAULT 'opencode',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'auto',
			tags TEXT,
			source TEXT DEFAULT 'local',
			enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
}

func (s *ConductorStore) ListTasks(limit int) ([]ConductorTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tasks []ConductorTask
	if s.db == nil || s.db.db == nil {
		return tasks, nil
	}
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.db.Query(`
		SELECT id, repo_url, prompt, base_ref, status, model, cli, created_at
		FROM conductor_tasks
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var t ConductorTask
		var bRef, mod, cl *string
		var crStr string
		if err := rows.Scan(&t.ID, &t.RepoURL, &t.Prompt, &bRef, &t.Status, &mod, &cl, &crStr); err == nil {
			if bRef != nil {
				t.BaseRef = *bRef
			}
			if mod != nil {
				t.Model = *mod
			}
			if cl != nil {
				t.CLI = *cl
			}
			t.CreatedAt, _ = time.Parse(time.RFC3339, crStr)
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

func (s *ConductorStore) CreateTask(repoURL, prompt, baseRef, cli, model string) (ConductorTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cli == "" {
		cli = "opencode"
	}

	t := ConductorTask{
		ID:        fmt.Sprintf("task_%d", time.Now().UnixNano()),
		RepoURL:   repoURL,
		Prompt:    prompt,
		BaseRef:   baseRef,
		Status:    "queued",
		Model:     model,
		CLI:       cli,
		CreatedAt: time.Now().UTC(),
	}

	if s.db == nil || s.db.db == nil {
		return t, fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT INTO conductor_tasks (id, repo_url, prompt, base_ref, status, model, cli, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.RepoURL, t.Prompt, t.BaseRef, t.Status, t.Model, t.CLI, t.CreatedAt.Format(time.RFC3339))
	return t, err
}

func (s *ConductorStore) ListSkills() ([]SkillRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var skills []SkillRecord
	if s.db == nil || s.db.db == nil {
		return skills, nil
	}

	rows, err := s.db.db.Query(`SELECT id, name, description, mode, tags, source, enabled, created_at FROM skills ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sk SkillRecord
		var tagsStr string
		var src string
		var enInt int
		var crStr string
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Mode, &tagsStr, &src, &enInt, &crStr); err == nil {
			_ = json.Unmarshal([]byte(tagsStr), &sk.Tags)
			sk.Source = src
			sk.Enabled = (enInt == 1)
			sk.CreatedAt, _ = time.Parse(time.RFC3339, crStr)
			skills = append(skills, sk)
		}
	}
	return skills, nil
}

func (s *ConductorStore) CreateSkill(name, description, mode, source string, tags []string) (SkillRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if mode == "" {
		mode = "auto"
	}
	if source == "" {
		source = "local"
	}

	sk := SkillRecord{
		ID:          fmt.Sprintf("sk_%d", time.Now().UnixNano()),
		Name:        name,
		Description: description,
		Mode:        mode,
		Tags:        tags,
		Source:      source,
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}

	if s.db == nil || s.db.db == nil {
		return sk, fmt.Errorf("database not initialized")
	}

	tBytes, _ := json.Marshal(tags)
	_, err := s.db.db.Exec(`
		INSERT INTO skills (id, name, description, mode, tags, source, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?)
	`, sk.ID, sk.Name, sk.Description, sk.Mode, string(tBytes), sk.Source, sk.CreatedAt.Format(time.RFC3339))
	return sk, err
}
