// Package config persists non-secret defaults to disk. API keys live in the
// OS keyring instead — see the secrets package.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config holds the settings that survive between runs. Every field here maps
// to a `convert` flag; the flag wins when it is set explicitly.
type Config struct {
	Provider        string `json:"provider"`
	Extract         string `json:"extract"`
	Paginate        bool   `json:"paginate"`
	ImageLimit      int    `json:"image_limit"`
	ImageMinSize    int    `json:"image_min_size"`
	AssetsSubfolder bool   `json:"assets_subfolder"`
	Metadata        bool   `json:"metadata"`
	MovePDF         bool   `json:"move_pdf"`
	DeleteOriginal  bool   `json:"delete_original"`
	DeleteRemote    bool   `json:"delete_remote"`
}

// Default returns the built-in defaults, which mirror the Obsidian plugin's
// DEFAULT_SETTINGS.
func Default() Config {
	return Config{
		Provider:        "mistral",
		Extract:         "all",
		Paginate:        false,
		ImageLimit:      0,
		ImageMinSize:    0,
		AssetsSubfolder: true,
		Metadata:        false,
		MovePDF:         false,
		DeleteOriginal:  false,
		DeleteRemote:    false,
	}
}

// Path returns the location of the config file, e.g.
// ~/.config/marker-cli/config.json.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(dir, "marker-cli", "config.json"), nil
}

// Load reads the config file, returning defaults if it does not exist yet.
func Load() (Config, error) {
	cfg := Default()

	path, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	// Unmarshal onto the defaults so keys missing from the file keep their
	// default value rather than becoming the zero value.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config file, creating its directory if needed.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
