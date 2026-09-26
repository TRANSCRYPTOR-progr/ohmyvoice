package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Model         string `json:"model"`
	Language      string `json:"language"`
	Threads       int    `json:"threads"`
	SoundFeedback bool   `json:"sound_feedback"`
	AutoStart     bool   `json:"autostart"`
	Hotkey        string `json:"hotkey"`
}

func Load(baseDir string) *Config {
	cfgPath := filepath.Join(baseDir, "config.json")
	cfg := &Config{
		Model:         "ggml-small.bin",
		Language:      "ru",
		Threads:       6,
		SoundFeedback: true,
		AutoStart:     IsAutoStartEnabled(),
		Hotkey:        "caps_lock",
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		_ = cfg.Save(baseDir)
		return cfg
	}

	_ = json.Unmarshal(data, cfg)
	if cfg.Hotkey == "" {
		cfg.Hotkey = "caps_lock"
	}
	cfg.AutoStart = IsAutoStartEnabled()
	return cfg
}

func (c *Config) Save(baseDir string) error {
	cfgPath := filepath.Join(baseDir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}
