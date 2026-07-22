package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/l3-n0x/marker-cli/internal/config"
	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/secrets"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change persisted defaults",
	}
	cmd.AddCommand(newConfigShowCmd(), newConfigSetCmd(), newConfigPathCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the current defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, kv := range configPairs(cfg) {
				fmt.Fprintf(w, "%s\t%s\n", kv[0], kv[1])
			}
			fmt.Fprintf(w, "api key\t%s\n", secrets.Source(cfg.Provider))
			return w.Flush()
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change one default, e.g. `config set extract text`",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := setConfigValue(&cfg, args[0], args[1]); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✓ %s = %s\n", args[0], args[1])
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

// configPairs renders the config as display rows, in a stable order.
func configPairs(cfg config.Config) [][2]string {
	return [][2]string{
		{"provider", cfg.Provider},
		{"extract", cfg.Extract},
		{"paginate", strconv.FormatBool(cfg.Paginate)},
		{"image-limit", strconv.Itoa(cfg.ImageLimit)},
		{"image-min-size", strconv.Itoa(cfg.ImageMinSize)},
		{"assets-subfolder", strconv.FormatBool(cfg.AssetsSubfolder)},
		{"metadata", strconv.FormatBool(cfg.Metadata)},
		{"move-pdf", strconv.FormatBool(cfg.MovePDF)},
		{"delete-original", strconv.FormatBool(cfg.DeleteOriginal)},
		{"delete-remote", strconv.FormatBool(cfg.DeleteRemote)},
	}
}

// setConfigValue applies a `config set` assignment, validating as it goes.
func setConfigValue(cfg *config.Config, key, value string) error {
	parseBool := func() (bool, error) {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s expects true or false, got %q", key, value)
		}
		return b, nil
	}
	parseInt := func() (int, error) {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%s expects a non-negative number, got %q", key, value)
		}
		return n, nil
	}

	var err error
	switch strings.ToLower(key) {
	case "provider":
		if _, err := lookupProvider(value); err != nil {
			return err
		}
		cfg.Provider = value
	case "extract":
		if !converter.Extract(value).Valid() {
			return fmt.Errorf("extract expects all, text or images, got %q", value)
		}
		cfg.Extract = value
	case "paginate":
		cfg.Paginate, err = parseBool()
	case "image-limit":
		cfg.ImageLimit, err = parseInt()
	case "image-min-size":
		cfg.ImageMinSize, err = parseInt()
	case "assets-subfolder":
		cfg.AssetsSubfolder, err = parseBool()
	case "metadata":
		cfg.Metadata, err = parseBool()
	case "move-pdf":
		cfg.MovePDF, err = parseBool()
	case "delete-original":
		cfg.DeleteOriginal, err = parseBool()
	case "delete-remote":
		cfg.DeleteRemote, err = parseBool()
	default:
		return fmt.Errorf("unknown setting %q — run `marker-cli config show` for the list", key)
	}
	return err
}
