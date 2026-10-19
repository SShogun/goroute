package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port       int    `yaml:"port"`
	Host       string `yaml:"host"`
	StorageDir string `yaml:"storage_dir"`
}

type AccountConfig struct {
	ID           string    `yaml:"id"`
	Email        string    `yaml:"email,omitempty"`
	RefreshToken string    `yaml:"refresh_token,omitempty"`
	AccessToken  string    `yaml:"access_token,omitempty"`
	ExpiresAt    time.Time `yaml:"expires_at,omitempty"`
	ProjectID    string    `yaml:"project_id,omitempty"`
}

type ProviderConfig struct {
	ID           string            `yaml:"id"`
	Type         string            `yaml:"type"` // "openai", "antigravity", "kiro"
	BaseURL      string            `yaml:"base_url,omitempty"`
	APIKey       string            `yaml:"api_key,omitempty"`
	RefreshToken string            `yaml:"refresh_token,omitempty"`
	ProfileARN   string            `yaml:"profile_arn,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty"`
	Accounts     []AccountConfig   `yaml:"accounts,omitempty"`
}

type ComboTarget struct {
	ProviderID string `yaml:"provider_id" json:"provider_id"`
	Model      string `yaml:"model" json:"model"`
}

type ComboConfig struct {
	Name     string        `yaml:"name" json:"name"`
	Strategy string        `yaml:"strategy" json:"strategy"` // "failover", "round_robin"
	Targets  []ComboTarget `yaml:"targets" json:"targets"`
}

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Providers []ProviderConfig `yaml:"providers"`
	Combos    []ComboConfig    `yaml:"combos"`
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Server: ServerConfig{
			Port:       8128,
			Host:       "127.0.0.1",
			StorageDir: filepath.Join(home, ".goroute"),
		},
		Providers: []ProviderConfig{},
		Combos:    []ComboConfig{},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("serialize yaml: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}
