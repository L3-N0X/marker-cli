// Package secrets stores API keys in the operating system's keyring.
//
// Keys are never written to the config file. Lookup order is: OS keyring
// first, then the provider's conventional environment variable, so the tool
// still works on headless machines and in CI.
package secrets

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

// service is the keyring service name all entries are filed under.
const service = "marker-cli"

// ErrNoKey is returned by Get when neither the keyring nor the environment
// holds a key for the provider. Callers should tell the user to run
// `marker-cli login`.
var ErrNoKey = errors.New("no API key found")

// envVars maps a provider name to the environment variable checked as a
// fallback when the keyring has no entry.
var envVars = map[string]string{
	"mistral": "MISTRAL_API_KEY",
	"datalab": "DATALAB_API_KEY",
}

// Get returns the API key for provider, preferring the OS keyring and falling
// back to the provider's environment variable.
func Get(provider string) (string, error) {
	key, err := keyring.Get(service, provider)
	if err == nil && key != "" {
		return key, nil
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		// The keyring exists but is unusable (locked, no Secret Service, ...).
		// Fall through to the environment rather than failing outright.
		if env := envFor(provider); env != "" {
			return env, nil
		}
		return "", fmt.Errorf("reading key from keyring: %w", err)
	}
	if env := envFor(provider); env != "" {
		return env, nil
	}
	return "", ErrNoKey
}

// Set stores the API key for provider in the OS keyring.
func Set(provider, key string) error {
	if err := keyring.Set(service, provider, key); err != nil {
		return fmt.Errorf("saving key to keyring: %w", err)
	}
	return nil
}

// Delete removes the provider's key from the OS keyring. Deleting a key that
// is not there is not an error.
func Delete(provider string) error {
	err := keyring.Delete(service, provider)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("deleting key from keyring: %w", err)
	}
	return nil
}

// Source describes where a key came from, for display purposes.
func Source(provider string) string {
	if key, err := keyring.Get(service, provider); err == nil && key != "" {
		return "OS keyring"
	}
	if name := envVars[provider]; name != "" && os.Getenv(name) != "" {
		return "$" + name
	}
	return "not set"
}

func envFor(provider string) string {
	name := envVars[provider]
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}
