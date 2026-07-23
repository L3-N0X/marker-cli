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

// Config holds the settings that survive between runs. Most fields map to a
// `convert` flag; the flag wins when it is set explicitly. The provider-scoped
// fields are shared where the Obsidian plugin shares them (e.g. one Langs value
// serves Datalab and both Python backends), so switching provider keeps common
// settings.
type Config struct {
	Provider string `json:"provider"`

	// General settings, applied by every backend.
	Extract         string `json:"extract"`
	AssetsSubfolder bool   `json:"assets_subfolder"`
	Metadata        bool   `json:"metadata"`
	MovePDF         bool   `json:"move_pdf"`
	DeleteOriginal  bool   `json:"delete_original"`

	// Endpoints for the self-hosted / Python backends.
	MarkerEndpoint string `json:"marker_endpoint"`
	PythonEndpoint string `json:"python_endpoint"`

	// Backend-scoped settings (only some backends read each of these).
	Paginate         bool   `json:"paginate"`
	Langs            string `json:"langs"`
	ForceOCR         bool   `json:"force_ocr"`
	MaxPages         int    `json:"max_pages"`
	StripExistingOCR bool   `json:"strip_existing_ocr"`
	UseLLM           bool   `json:"use_llm"`
	SkipCache        bool   `json:"skip_cache"`
	ImageLimit       int    `json:"image_limit"`
	ImageMinSize     int    `json:"image_min_size"`
	DeleteRemote     bool   `json:"delete_remote"`

	// Configured records which endpoint providers have been set up via `login`,
	// so the interactive switcher can show only ready-to-use backends. API-key
	// providers are tracked by the keyring, not here.
	Configured map[string]bool `json:"configured,omitempty"`
}

// Default returns the built-in defaults, which mirror the Obsidian plugin's
// DEFAULT_SETTINGS.
func Default() Config {
	return Config{
		Provider:         "mistral",
		Extract:          "all",
		AssetsSubfolder:  true,
		Metadata:         false,
		MovePDF:          false,
		DeleteOriginal:   false,
		MarkerEndpoint:   "localhost:8000",
		PythonEndpoint:   "localhost:8001",
		Paginate:         false,
		Langs:            "en",
		ForceOCR:         false,
		MaxPages:         0,
		StripExistingOCR: false,
		UseLLM:           false,
		SkipCache:        false,
		ImageLimit:       0,
		ImageMinSize:     0,
		DeleteRemote:     false,
		Configured:       map[string]bool{},
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
