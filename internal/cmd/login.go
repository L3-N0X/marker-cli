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
	"github.com/l3-n0x/marker-cli/internal/secrets"
	"github.com/l3-n0x/marker-cli/internal/tui"
)

func newLoginCmd() *cobra.Command {
	var providerName string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store an API key securely in your OS keyring",
		Long: "Prompts for an API key, verifies it against the provider, and stores it in\n" +
			"your operating system's keyring. The key is never written to the config file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if providerName == "" {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				providerName = cfg.Provider
			}
			p, err := lookupProvider(providerName)
			if err != nil {
				return err
			}

			validate := func(ctx context.Context, apiKey string) error {
				return p.New(apiKey).TestConnection(ctx)
			}

			if !useTUI() {
				return loginPlain(p, validate)
			}

			saved, err := tui.RunLogin(p.Name, p.KeyURL, validate)
			if err != nil {
				return err
			}
			if !saved {
				return fmt.Errorf("cancelled — no key was saved")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&providerName, "provider", "", "provider to sign in to ("+providerNames()+")")
	return cmd
}

// loginPlain is the fallback when there is no TTY to draw a TUI on. It reads
// the key from stdin, which also makes `echo $KEY | marker-cli login` work.
func loginPlain(p provider, validate tui.Validator) error {
	fmt.Fprintf(os.Stderr, "Enter your %s API key (%s): ", p.Name, p.KeyURL)

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("reading API key: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return fmt.Errorf("no API key given")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := validate(ctx, key); err != nil {
		return fmt.Errorf("key rejected by %s: %w", p.Name, err)
	}
	if err := secrets.Set(p.Name, key); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "✓ key validated and saved to your OS keyring")
	return nil
}

func newLogoutCmd() *cobra.Command {
	var providerName string

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove a stored API key from your OS keyring",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if providerName == "" {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				providerName = cfg.Provider
			}
			p, err := lookupProvider(providerName)
			if err != nil {
				return err
			}
			if err := secrets.Delete(p.Name); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✓ removed the %s key from your OS keyring\n", p.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&providerName, "provider", "", "provider to sign out of ("+providerNames()+")")
	return cmd
}
