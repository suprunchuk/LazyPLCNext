package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ======================================================================================
// CONFIG & LOGGING
// ======================================================================================

type Config struct {
	WorkDir      string `json:"work_dir"`
	UseNerdFonts bool   `json:"use_nerd_fonts"`
}

func WriteLog(msg string) {
	temp := os.Getenv("TEMP")
	logPath := filepath.Join(temp, LogFileName)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	_, _ = fmt.Fprintf(f, "[%s] %s\n", timestamp, msg)
}

// parseConfig decodes launcher_config.json and migrates the legacy "work_dirs"
// list to the single "work_dir" field: only the first entry was ever used.
func parseConfig(data []byte) (Config, error) {
	var raw struct {
		WorkDirs     []string `json:"work_dirs"`
		WorkDir      string   `json:"work_dir"`
		UseNerdFonts bool     `json:"use_nerd_fonts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	cfg := Config{WorkDir: raw.WorkDir, UseNerdFonts: raw.UseNerdFonts}
	if cfg.WorkDir == "" && len(raw.WorkDirs) > 0 {
		cfg.WorkDir = raw.WorkDirs[0]
	}
	return cfg, nil
}

func configPath() string {
	exePath, _ := os.Executable()
	return filepath.Join(filepath.Dir(exePath), ConfigFileName)
}

func loadConfig() (Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return Config{}, err
	}
	return parseConfig(data)
}

func saveConfig(cfg Config) error {
	file, err := os.Create(configPath())
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}
