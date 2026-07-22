// Package cmd wires up the marker-cli command line.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridable at build time with
// -ldflags "-X github.com/l3-n0x/marker-cli/internal/cmd.version=v1.2.3".
var version = "dev"

var (
	flagVerbose bool
	flagNoTUI   bool
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "marker-cli",
		Short: "Convert PDFs to Markdown with MistralAI OCR",
		Long: "marker-cli converts PDFs to Markdown, tables, formulas and images included.\n\n" +
			"Run `marker-cli login` once to store your API key in the OS keyring, then\n" +
			"either `marker-cli start` to pick files in a full-screen UI, or\n" +
			"`marker-cli convert -i doc.pdf -o out/` for a one-shot conversion.\n" +
			"A bare `marker-cli` in a terminal opens the UI.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "print extra detail")
	root.PersistentFlags().BoolVar(&flagNoTUI, "no-tui", false, "disable the interactive UI and log plain lines")

	convert := newConvertCmd()
	root.AddCommand(convert, newStartCmd(), newLoginCmd(), newLogoutCmd(), newConfigCmd())

	// Make `marker-cli -i a.pdf -o out/` work by falling through to convert,
	// and a bare `marker-cli` in a terminal open the interactive browser.
	root.Flags().AddFlagSet(convert.Flags())
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && cmd.Flags().NFlag() == 0 && useTUI() {
			return runStart(cmd, ".")
		}
		return convert.RunE(cmd, args)
	}

	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}
