package cmd

import (
	"errors"
	"fmt"

	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/converter/mistral"
	"github.com/l3-n0x/marker-cli/internal/secrets"
)

// provider describes one OCR backend the CLI can talk to. Adding Datalab or a
// self-hosted Marker endpoint later means adding an entry here.
type provider struct {
	Name   string
	Label  string
	KeyURL string
	// New builds a converter for the given API key.
	New func(apiKey string) converter.Converter
}

var providers = []provider{
	{
		Name:   "mistral",
		Label:  "MistralAI OCR (free API key)",
		KeyURL: "https://console.mistral.ai/api-keys",
		New:    func(apiKey string) converter.Converter { return mistral.New(apiKey) },
	},
}

// lookupProvider finds a provider by name.
func lookupProvider(name string) (provider, error) {
	for _, p := range providers {
		if p.Name == name {
			return p, nil
		}
	}
	return provider{}, fmt.Errorf("unknown provider %q (known: %s)", name, providerNames())
}

func providerNames() string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name)
	}
	return joinComma(names)
}

// newConverter builds a converter for name using the stored API key.
func newConverter(name string) (converter.Converter, error) {
	p, err := lookupProvider(name)
	if err != nil {
		return nil, err
	}
	key, err := secrets.Get(p.Name)
	if errors.Is(err, secrets.ErrNoKey) {
		return nil, fmt.Errorf("no %s API key found — run `marker-cli login`", p.Name)
	}
	if err != nil {
		return nil, err
	}
	return p.New(key), nil
}

func joinComma(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
