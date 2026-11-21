// Package config loads and saves the application settings.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is the full set of user-tunable settings.
type Config struct {
	DownloadDir               string `json:"downloadDir"`
	StateDir                  string `json:"stateDir"`
	MaxConcurrentDownloads    int    `json:"maxConcurrentDownloads"`
	MaxConnectionsPerDownload int    `json:"maxConnectionsPerDownload"`
	MinSegmentSize            int64  `json:"minSegmentSize"`
	ChunkSize                 int    `json:"chunkSize"`
	GlobalSpeedLimit          int64  `json:"globalSpeedLimit"` // bytes/sec, 0 = unlimited
	RetryMaxAttempts          int    `json:"retryMaxAttempts"`
	RetryInitialDelayMS       int    `json:"retryInitialDelayMs"`
	RetryMaxDelayMS           int    `json:"retryMaxDelayMS"`
}

// Default returns a configuration with sensible values for personal use.
func Default() *Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return &Config{
		DownloadDir:               filepath.Join(home, "Downloads"),
		StateDir:                  filepath.Join(home, ".local", "share", "easydownload"),
		MaxConcurrentDownloads:    3,
		MaxConnectionsPerDownload: 8,
		MinSegmentSize:            1024 * 1024, // 1 MiB
		ChunkSize:                 64 * 1024,   // 64 KiB
		GlobalSpeedLimit:          0,
		RetryMaxAttempts:          5,
		RetryInitialDelayMS:       1000,
		RetryMaxDelayMS:           30000,
	}
}

func (c *Config) validate() error {
	if c.DownloadDir == "" {
		return errors.New("downloadDir must not be empty")
	}
	if c.StateDir == "" {
		return errors.New("stateDir must not be empty")
	}
	if c.MaxConcurrentDownloads < 1 {
		c.MaxConcurrentDownloads = 1
	}
	if c.MaxConnectionsPerDownload < 1 {
		c.MaxConnectionsPerDownload = 1
	}
	if c.MinSegmentSize < 1024 {
		c.MinSegmentSize = 1024
	}
	if c.ChunkSize < 4096 {
		c.ChunkSize = 4096
	}
	if c.GlobalSpeedLimit < 0 {
		c.GlobalSpeedLimit = 0
	}
	if c.RetryMaxAttempts < 1 {
		c.RetryMaxAttempts = 1
	}
	if c.RetryInitialDelayMS < 100 {
		c.RetryInitialDelayMS = 100
	}
	if c.RetryMaxDelayMS < c.RetryInitialDelayMS {
		c.RetryMaxDelayMS = c.RetryInitialDelayMS
	}
	return nil
}

// Path returns the location of the config file.
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "easydownload", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "easydownload", "config.json"), nil
}

// Load reads the config file, creating it with defaults when missing.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if err := Save(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the config file atomically.
func Save(cfg *Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
