package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/l3-n0x/marker-cli/internal/config"
	"github.com/l3-n0x/marker-cli/internal/provider"
	"github.com/l3-n0x/marker-cli/internal/secrets"
	"github.com/l3-n0x/marker-cli/internal/tui"
)

// stdin is a single shared reader so successive plain-mode prompts don't lose
// input to separate readers' buffers (a bufio.Reader may read ahead past one
// line).
var stdin = bufio.NewReader(os.Stdin)

func newLoginCmd() *cobra.Command {
	var providerName string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Set up a provider and make it the default",
		Long: "Pick a provider, then sign in: API-key backends (MistralAI, Datalab) prompt for\n" +
			"a key stored in your OS keyring; endpoint backends (self-hosted, Python) prompt\n" +
			"for a host:port that is tested and saved to the config. The provider you set up\n" +
			"becomes the default for conversions when --provider is omitted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(providerName)
		},
	}

	cmd.Flags().StringVar(&providerName, "provider", "", "provider to set up ("+provider.Names()+"); prompts if omitted")
	return cmd
}

func runLogin(providerName string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Configured == nil {
		cfg.Configured = map[string]bool{}
	}

	var p provider.Provider
	if providerName != "" {
		p, err = provider.Lookup(providerName)
		if err != nil {
			return err
		}
	} else {
		p, err = pickProvider(cfg)
		if err != nil {
			return err
		}
	}

	if err := loginTo(&cfg, p); err != nil {
		return err
	}

	cfg.Provider = p.Name
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ %s is set up and is now the default provider\n", p.Name)
	return nil
}

// pickProvider asks which provider to set up, annotating each with its current
// status.
func pickProvider(cfg config.Config) (provider.Provider, error) {
	if !useTUI() {
		return pickProviderPlain(cfg)
	}
	items := make([]tui.PickerItem, len(provider.All))
	for i, p := range provider.All {
		items[i] = tui.PickerItem{Label: p.Label, Desc: providerStatus(p, cfg)}
	}
	idx, ok, err := tui.RunPicker("which provider do you want to set up?", items)
	if err != nil {
		return provider.Provider{}, err
	}
	if !ok {
		return provider.Provider{}, fmt.Errorf("cancelled")
	}
	return provider.All[idx], nil
}

func pickProviderPlain(cfg config.Config) (provider.Provider, error) {
	fmt.Fprintln(os.Stderr, "Which provider do you want to set up?")
	for i, p := range provider.All {
		fmt.Fprintf(os.Stderr, "  %d) %s — %s\n", i+1, p.Name, providerStatus(p, cfg))
	}
	fmt.Fprint(os.Stderr, "Enter a number: ")

	line, _ := stdin.ReadString('\n')
	choice := strings.TrimSpace(line)
	for i, p := range provider.All {
		if choice == fmt.Sprint(i+1) || strings.EqualFold(choice, p.Name) {
			return p, nil
		}
	}
	return provider.Provider{}, fmt.Errorf("invalid choice %q", choice)
}

// providerStatus is the one-line status shown in the picker.
func providerStatus(p provider.Provider, cfg config.Config) string {
	if providerConfigured(p, cfg) {
		return "configured"
	}
	if p.Auth == provider.AuthEndpoint {
		return "not set up"
	}
	return "no key"
}

// loginTo runs the sign-in appropriate to p's auth kind, persisting the result
// (key to the keyring, endpoint to cfg).
func loginTo(cfg *config.Config, p provider.Provider) error {
	if p.Auth == provider.AuthEndpoint {
		return loginEndpoint(cfg, p)
	}
	return loginAPIKey(p)
}

func loginAPIKey(p provider.Provider) error {
	validate := func(ctx context.Context, key string) error {
		return p.New(provider.Creds{APIKey: key}).TestConnection(ctx)
	}

	var key string
	if useTUI() {
		value, ok, err := tui.RunPrompt(tui.PromptConfig{
			Title:    "sign in to " + p.Name,
			Hint:     "Get a key at " + p.KeyURL,
			Password: true,
			Validate: validate,
		})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled — no key was saved")
		}
		key = value
	} else {
		value, err := plainPrompt(fmt.Sprintf("Enter your %s API key (%s): ", p.Name, p.KeyURL), "", validate)
		if err != nil {
			return err
		}
		key = value
	}

	if err := secrets.Set(p.Name, key); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "✓ key validated and saved to your OS keyring")
	return nil
}

func loginEndpoint(cfg *config.Config, p provider.Provider) error {
	current := endpointFor(p, *cfg)
	validate := func(ctx context.Context, endpoint string) error {
		return p.New(provider.Creds{Endpoint: endpoint}).TestConnection(ctx)
	}

	var endpoint string
	if useTUI() {
		value, ok, err := tui.RunPrompt(tui.PromptConfig{
			Title:    "configure " + p.Name,
			Hint:     "Endpoint of the Marker API (host:port)",
			Initial:  current,
			Validate: validate,
		})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled — endpoint not saved")
		}
		endpoint = value
	} else {
		value, err := plainPrompt(fmt.Sprintf("Enter the %s endpoint [%s]: ", p.Name, current), current, validate)
		if err != nil {
			return err
		}
		endpoint = value
	}

	setEndpoint(cfg, p, endpoint)
	cfg.Configured[p.Name] = true
	fmt.Fprintf(os.Stderr, "✓ endpoint %s reachable and saved\n", endpoint)
	return nil
}

// setEndpoint writes value to the config field p reads.
func setEndpoint(cfg *config.Config, p provider.Provider, value string) {
	switch p.EndpointField {
	case provider.SettingMarkerEndpoint:
		cfg.MarkerEndpoint = value
	case provider.SettingPythonEndpoint:
		cfg.PythonEndpoint = value
	}
}

// plainPrompt reads a value from stdin (falling back to fallback when the line
// is empty), validates it, and returns it. It also makes piping a value work,
// e.g. `echo $KEY | marker-cli login --provider mistral`.
func plainPrompt(prompt, fallback string, validate tui.Validator) (string, error) {
	fmt.Fprint(os.Stderr, prompt)

	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading input: %w", err)
	}
	value := strings.TrimSpace(line)
	if value == "" {
		value = fallback
	}
	if value == "" {
		return "", fmt.Errorf("no value given")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := validate(ctx, value); err != nil {
		return "", fmt.Errorf("rejected: %w", err)
	}
	return value, nil
}

func newLogoutCmd() *cobra.Command {
	var providerName string

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove a stored API key or endpoint setup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if providerName == "" {
				providerName = cfg.Provider
			}
			p, err := provider.Lookup(providerName)
			if err != nil {
				return err
			}

			if p.Auth == provider.AuthEndpoint {
				if cfg.Configured != nil {
					delete(cfg.Configured, p.Name)
				}
				if err := config.Save(cfg); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "✓ %s is no longer marked as set up\n", p.Name)
				return nil
			}

			if err := secrets.Delete(p.Name); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✓ removed the %s key from your OS keyring\n", p.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&providerName, "provider", "", "provider to sign out of ("+provider.Names()+")")
	return cmd
}
