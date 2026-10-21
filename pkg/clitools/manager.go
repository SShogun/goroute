package clitools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/storage"
)

type ToolDetection struct {
	Installed   bool   `json:"installed"`
	Runnable    bool   `json:"runnable"`
	Version     string `json:"version,omitempty"`
	Command     string `json:"command,omitempty"`
	CommandPath string `json:"commandPath,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type ToolConfigStatus struct {
	Status           string `json:"status"` // "configured", "not_configured", "partial"
	Endpoint         string `json:"endpoint,omitempty"`
	LastConfiguredAt *int64 `json:"lastConfiguredAt,omitempty"`
}

type ToolBatchStatus struct {
	Detection ToolDetection    `json:"detection"`
	Config    ToolConfigStatus `json:"config"`
	Error     string           `json:"error,omitempty"`
}

type ToolBatchStatusMap map[string]ToolBatchStatus

type BackupEntry struct {
	BackupID  string `json:"backupId"`
	Tool      string `json:"tool"`
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	CreatedAt string `json:"createdAt"`
	SizeBytes int64  `json:"sizeBytes"`
}

type ApplyRequest struct {
	ToolID  string `json:"toolId"`
	BaseURL string `json:"baseUrl,omitempty"`
	APIKey  string `json:"apiKey,omitempty"`
	Model   string `json:"model,omitempty"`
	DryRun  bool   `json:"dryRun,omitempty"`
}

func getBackupDir(toolId string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".goroute", "backups", toolId)
	os.MkdirAll(dir, 0755)
	return dir
}

// BackupConfigFile saves a timestamped copy of the config file.
func BackupConfigFile(toolId, filePath string) (*BackupEntry, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	backupDir := getBackupDir(toolId)
	backupName := fmt.Sprintf("%s_%d.bak", filepath.Base(filePath), time.Now().Unix())
	targetPath := filepath.Join(backupDir, backupName)

	if err := os.WriteFile(targetPath, data, 0600); err != nil {
		return nil, err
	}

	fi, _ := os.Stat(targetPath)
	var size int64
	if fi != nil {
		size = fi.Size()
	}

	return &BackupEntry{
		BackupID:  backupName,
		Tool:      toolId,
		Path:      targetPath,
		Filename:  backupName,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		SizeBytes: size,
	}, nil
}

// ListBackups returns all backups for a given tool (or all tools if empty).
func ListBackups(toolId string) ([]BackupEntry, error) {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".goroute", "backups")

	var list []BackupEntry
	var toolDirs []string

	if toolId != "" {
		toolDirs = append(toolDirs, filepath.Join(baseDir, toolId))
	} else {
		entries, err := os.ReadDir(baseDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					toolDirs = append(toolDirs, filepath.Join(baseDir, e.Name()))
				}
			}
		}
	}

	for _, dir := range toolDirs {
		t := filepath.Base(dir)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".bak") {
				info, _ := f.Info()
				var size int64
				var modTime time.Time
				if info != nil {
					size = info.Size()
					modTime = info.ModTime()
				}
				list = append(list, BackupEntry{
					BackupID:  f.Name(),
					Tool:      t,
					Path:      filepath.Join(dir, f.Name()),
					Filename:  f.Name(),
					CreatedAt: modTime.UTC().Format(time.RFC3339),
					SizeBytes: size,
				})
			}
		}
	}

	return list, nil
}

// RestoreBackup restores a backup file over the active config file.
func RestoreBackup(toolId, backupId string) error {
	home, _ := os.UserHomeDir()
	backupPath := filepath.Join(home, ".goroute", "backups", toolId, backupId)
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup %s: %w", backupId, err)
	}

	tool, exists := Registry[toolId]
	if !exists {
		return fmt.Errorf("unknown tool %s", toolId)
	}

	targetPath := strings.Replace(tool.SettingsFile, "~", home, 1)
	os.MkdirAll(filepath.Dir(targetPath), 0755)
	return os.WriteFile(targetPath, data, 0644)
}

// DeleteBackup deletes a specific backup file.
func DeleteBackup(toolId, backupId string) error {
	home, _ := os.UserHomeDir()
	backupPath := filepath.Join(home, ".goroute", "backups", toolId, backupId)
	return os.Remove(backupPath)
}

// GetAllStatuses scans all registered CLI tools in parallel and checks runtime and config.
func GetAllStatuses() (ToolBatchStatusMap, error) {
	home, _ := os.UserHomeDir()
	statuses := make(ToolBatchStatusMap)
	var mu sync.Mutex

	var lastConfiguredMap map[string]int64
	db := storage.GetDB()
	if db != nil {
		lastConfiguredMap, _ = db.GetAllCliToolLastConfigured()
	}

	var wg sync.WaitGroup
	for id, tool := range Registry {
		wg.Add(1)
		go func(id string, tool CliTool) {
			defer wg.Done()

			var det ToolDetection
			binName := tool.DefaultCommand
			if binName != "" {
				if p, err := exec.LookPath(binName); err == nil && p != "" {
					det.Installed = true
					det.Runnable = true
					det.Command = binName
					det.CommandPath = p

					// Version probe with timeout (skip GUI apps)
					if binName != "antigravity" && binName != "cursor" && binName != "windsurf" && binName != "zed" {
						ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
						cmd := exec.CommandContext(ctx, binName, "--version")
						if out, err := cmd.Output(); err == nil {
							v := strings.TrimSpace(string(out))
							if len(v) > 40 {
								v = v[:40]
							}
							det.Version = v
						}
						cancel()
					}
				}
			}

			cfgStatus := ToolConfigStatus{
				Status: "not_configured",
			}

			expandedPath := strings.Replace(tool.SettingsFile, "~", home, 1)
			if fi, err := os.Stat(expandedPath); err == nil && !fi.IsDir() {
				det.Installed = true // config exists, mark as installed
				data, _ := os.ReadFile(expandedPath)
				content := string(data)
				if strings.Contains(content, "8128") || strings.Contains(content, "goroute") {
					cfgStatus.Status = "configured"
					if strings.Contains(content, "8128/v1") {
						cfgStatus.Endpoint = "http://127.0.0.1:8128/v1"
					} else {
						cfgStatus.Endpoint = "http://127.0.0.1:8128"
					}
				} else {
					cfgStatus.Status = "partial"
				}
			}

			if lastConfiguredMap != nil {
				if ts, ok := lastConfiguredMap[id]; ok {
					cfgStatus.LastConfiguredAt = &ts
				}
			}

			mu.Lock()
			statuses[id] = ToolBatchStatus{
				Detection: det,
				Config:    cfgStatus,
			}
			mu.Unlock()
		}(id, tool)
	}

	wg.Wait()
	return statuses, nil
}

// DetectTool probes a single tool.
func DetectTool(toolId string) (*ToolBatchStatus, error) {
	all, err := GetAllStatuses()
	if err != nil {
		return nil, err
	}
	if st, ok := all[toolId]; ok {
		return &st, nil
	}
	return nil, fmt.Errorf("unknown tool %s", toolId)
}

// ApplyTool executes configuration with automated backup and state tracking.
func ApplyTool(req ApplyRequest) (map[string]interface{}, error) {
	tool, exists := Registry[req.ToolID]
	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", req.ToolID)
	}

	home, _ := os.UserHomeDir()
	configPath := strings.Replace(tool.SettingsFile, "~", home, 1)

	// Dry run preview
	if req.DryRun {
		return map[string]interface{}{
			"success":    true,
			"dryRun":     true,
			"toolId":     req.ToolID,
			"configPath": configPath,
			"message":    fmt.Sprintf("Would configure %s at %s", tool.Name, configPath),
		}, nil
	}

	// 1. Ensure backup
	backup, _ := BackupConfigFile(req.ToolID, configPath)

	// 2. Run configure tool
	msg, err := ConfigureTool(req.ToolID)
	if err != nil {
		return nil, fmt.Errorf("failed to configure %s: %w", req.ToolID, err)
	}

	// 3. Persist state in SQLite
	now := time.Now().Unix()
	db := storage.GetDB()
	if db != nil {
		endpoint := req.BaseURL
		if endpoint == "" {
			endpoint = "http://127.0.0.1:8128/v1"
		}
		_ = db.SaveCliToolState(req.ToolID, now, configPath, endpoint)
	}

	res := map[string]interface{}{
		"success":    true,
		"message":    msg,
		"toolId":     req.ToolID,
		"configPath": configPath,
	}
	if backup != nil {
		res["backup"] = backup
	}
	return res, nil
}

// ReadSettingsFile reads tool config file as JSON or raw content.
func ReadSettingsFile(toolId string) (map[string]interface{}, error) {
	tool, exists := Registry[toolId]
	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", toolId)
	}
	home, _ := os.UserHomeDir()
	path := strings.Replace(tool.SettingsFile, "~", home, 1)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var res map[string]interface{}
	if err := json.Unmarshal(data, &res); err != nil {
		// return as raw content if not JSON
		return map[string]interface{}{
			"raw": string(data),
		}, nil
	}
	return res, nil
}
