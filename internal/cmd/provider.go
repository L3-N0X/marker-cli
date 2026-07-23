package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/l3-n0x/marker-cli/internal/config"
	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/provider"
	"github.com/l3-n0x/marker-cli/internal/secrets"
)

// converterFor builds the converter for the provider named in cfg, resolving
// its credentials from the keyring (API-key backends) or the config endpoints
// (endpoint backends).
func converterFor(cfg config.Config) (converter.Converter, error) {
	p, err := provider.Lookup(cfg.Provider)
	if err != nil {
		return nil, err
	}
	creds, err := credsFor(p, cfg)
	if err != nil {
		return nil, err
	}
	return p.New(creds), nil
}

// credsFor resolves the credentials p needs from cfg and the keyring.
func credsFor(p provider.Provider, cfg config.Config) (provider.Creds, error) {
	switch p.Auth {
	case provider.AuthAPIKey:
		key, err := secrets.Get(p.Name)
		if errors.Is(err, secrets.ErrNoKey) {
			return provider.Creds{}, fmt.Errorf("no %s API key found — run `marker-cli login`", p.Name)
		}
		if err != nil {
			return provider.Creds{}, err
		}
		return provider.Creds{APIKey: key}, nil
	case provider.AuthEndpoint:
		return provider.Creds{Endpoint: endpointFor(p, cfg)}, nil
	}
	return provider.Creds{}, fmt.Errorf("provider %q has no auth method", p.Name)
}

// endpointFor returns the effective endpoint for an endpoint-based provider.
func endpointFor(p provider.Provider, cfg config.Config) string {
	switch p.EndpointField {
	case provider.SettingMarkerEndpoint:
		return orDefaultStr(cfg.MarkerEndpoint, p.DefaultEndpoint)
	case provider.SettingPythonEndpoint:
		return orDefaultStr(cfg.PythonEndpoint, p.DefaultEndpoint)
	}
	return p.DefaultEndpoint
}

// providerConfigured reports whether p is ready to use: an API-key backend has a
// key, an endpoint backend has been set up via `login`.
func providerConfigured(p provider.Provider, cfg config.Config) bool {
	switch p.Auth {
	case provider.AuthAPIKey:
		_, err := secrets.Get(p.Name)
		return err == nil
	case provider.AuthEndpoint:
		return cfg.Configured[p.Name]
	}
	return false
}

// configuredProviders returns the names of every ready-to-use provider, in
// registry order.
func configuredProviders(cfg config.Config) []string {
	var names []string
	for _, p := range provider.All {
		if providerConfigured(p, cfg) {
			names = append(names, p.Name)
		}
	}
	return names
}

// reqFromConfig builds the conversion request from the persisted settings. Each
// backend reads only the fields it supports.
func reqFromConfig(cfg config.Config, extract converter.Extract) converter.Request {
	return converter.Request{
		Extract:          extract,
		Paginate:         cfg.Paginate,
		Langs:            cfg.Langs,
		ForceOCR:         cfg.ForceOCR,
		MaxPages:         cfg.MaxPages,
		StripExistingOCR: cfg.StripExistingOCR,
		UseLLM:           cfg.UseLLM,
		SkipCache:        cfg.SkipCache,
		ImageLimit:       cfg.ImageLimit,
		ImageMinSize:     cfg.ImageMinSize,
		DeleteRemote:     cfg.DeleteRemote,
	}
}

func orDefaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
