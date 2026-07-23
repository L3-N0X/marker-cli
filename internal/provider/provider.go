// Package provider is the single registry of OCR backends. Every fact about a
// backend that the rest of the program needs — how it authenticates, which
// settings it supports, and how to build its converter — lives in one entry
// here. Adding a new backend is a matter of appending to All and writing its
// converter package; no other file needs to learn about it.
package provider

import (
	"fmt"

	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/converter/datalab"
	"github.com/l3-n0x/marker-cli/internal/converter/mistral"
	"github.com/l3-n0x/marker-cli/internal/converter/pythonapi"
	"github.com/l3-n0x/marker-cli/internal/converter/selfhosted"
)

// Auth is how a backend proves who you are.
type Auth int

const (
	// AuthAPIKey backends need a secret key, stored in the OS keyring.
	AuthAPIKey Auth = iota
	// AuthEndpoint backends need only a host:port, kept in the config file.
	AuthEndpoint
)

// Setting keys are the option identifiers shared by the config file, the
// `convert` flags and the interactive settings panel. They name backend-scoped
// settings only; the general settings (extract, metadata, …) apply everywhere
// and are not listed per provider.
const (
	SettingMarkerEndpoint = "marker-endpoint"
	SettingPythonEndpoint = "python-endpoint"
	SettingLangs          = "langs"
	SettingForceOCR       = "force-ocr"
	SettingPaginate       = "paginate"
	SettingMaxPages       = "max-pages"
	SettingStripOCR       = "strip-existing-ocr"
	SettingUseLLM         = "use-llm"
	SettingSkipCache      = "skip-cache"
	SettingImageLimit     = "image-limit"
	SettingImageMinSize   = "image-min-size"
	SettingDeleteRemote   = "delete-remote"
)

// Creds carries whatever a backend needs to be constructed: an API key for
// AuthAPIKey providers, an endpoint for AuthEndpoint ones.
type Creds struct {
	APIKey   string
	Endpoint string
}

// Provider describes one backend.
type Provider struct {
	Name  string // short id, e.g. "datalab"
	Label string // human description for menus

	Auth   Auth
	KeyURL string // where to get a key (AuthAPIKey)
	EnvVar string // env var checked as a key fallback (AuthAPIKey)

	EndpointField   string // which config endpoint field to read (AuthEndpoint)
	DefaultEndpoint string // default host:port (AuthEndpoint)

	// Settings lists the backend-scoped option keys this provider supports, in
	// the order they should appear in the settings panel.
	Settings []string

	// New builds the converter from resolved credentials.
	New func(Creds) converter.Converter
}

// All is the registry, in menu order.
var All = []Provider{
	{
		Name:     "mistral",
		Label:    "MistralAI OCR (hosted, API key)",
		Auth:     AuthAPIKey,
		KeyURL:   "https://console.mistral.ai/api-keys",
		EnvVar:   "MISTRAL_API_KEY",
		Settings: []string{SettingDeleteRemote, SettingImageLimit, SettingImageMinSize, SettingPaginate},
		New:      func(c Creds) converter.Converter { return mistral.New(c.APIKey) },
	},
	{
		Name:   "datalab",
		Label:  "Datalab Marker (hosted, API key)",
		Auth:   AuthAPIKey,
		KeyURL: "https://www.datalab.to/app/keys",
		EnvVar: "DATALAB_API_KEY",
		Settings: []string{
			SettingLangs, SettingForceOCR, SettingPaginate,
			SettingMaxPages, SettingStripOCR, SettingUseLLM, SettingSkipCache,
		},
		New: func(c Creds) converter.Converter { return datalab.New(c.APIKey) },
	},
	{
		Name:            "selfhosted",
		Label:           "Self-hosted Marker API (Docker)",
		Auth:            AuthEndpoint,
		EndpointField:   SettingMarkerEndpoint,
		DefaultEndpoint: "localhost:8000",
		Settings:        []string{SettingMarkerEndpoint},
		New:             func(c Creds) converter.Converter { return selfhosted.New(c.Endpoint) },
	},
	{
		Name:            "python-local",
		Label:           "Marker Python API (local, reads files itself)",
		Auth:            AuthEndpoint,
		EndpointField:   SettingPythonEndpoint,
		DefaultEndpoint: "localhost:8001",
		Settings:        []string{SettingPythonEndpoint, SettingLangs, SettingForceOCR, SettingPaginate},
		New:             func(c Creds) converter.Converter { return pythonapi.NewLocal(c.Endpoint) },
	},
	{
		Name:            "python-cloud",
		Label:           "Marker Python API (upload)",
		Auth:            AuthEndpoint,
		EndpointField:   SettingPythonEndpoint,
		DefaultEndpoint: "localhost:8001",
		Settings:        []string{SettingPythonEndpoint, SettingLangs, SettingForceOCR, SettingPaginate},
		New:             func(c Creds) converter.Converter { return pythonapi.NewCloud(c.Endpoint) },
	},
}

// Lookup finds a provider by name.
func Lookup(name string) (Provider, error) {
	for _, p := range All {
		if p.Name == name {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown provider %q (known: %s)", name, Names())
}

// Names returns every provider name, comma-separated, for help text.
func Names() string {
	out := ""
	for i, p := range All {
		if i > 0 {
			out += ", "
		}
		out += p.Name
	}
	return out
}
