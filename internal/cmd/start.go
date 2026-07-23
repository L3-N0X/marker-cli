package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/l3-n0x/marker-cli/internal/config"
	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/output"
	"github.com/l3-n0x/marker-cli/internal/tui"
)

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [directory]",
		Short: "Browse PDFs and convert them in a full-screen UI",
		Long: "start opens a two-pane browser: PDFs on the left, conversion settings on\n" +
			"the right. Pick files with space, convert them with enter, and change any\n" +
			"setting without leaving the terminal.",
		Example: "  marker-cli start\n  marker-cli start ~/Downloads",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			return runStart(cmd, dir)
		},
	}
}

func runStart(cmd *cobra.Command, dir string) error {
	if !useTUI() {
		return errors.New("`start` needs an interactive terminal — use `marker-cli convert` instead")
	}

	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// The switcher only offers ready-to-use providers; fall back to the current
	// one so the panel is never empty when nothing is configured yet.
	names := configuredProviders(cfg)
	if len(names) == 0 {
		names = []string{cfg.Provider}
	}

	return tui.RunStart(ctx, tui.StartOptions{
		Dir:       dir,
		Config:    cfg,
		Providers: names,
		Prepare:   prepareRun,
		Converted: destinationExists,
		Save:      config.Save,
	})
}

// prepareRun validates a request from the browser and hands back the work to
// run. Anything that can fail cheaply — a missing API key, a destination that
// already exists — fails here, before a single byte is uploaded.
func prepareRun(files []string, cfg config.Config, outDir string, force bool) (tui.Runner, error) {
	extract := converter.Extract(strings.ToLower(cfg.Extract))
	if !extract.Valid() {
		return nil, fmt.Errorf("invalid extract %q: use all, text or images", cfg.Extract)
	}

	conv, err := converterFor(cfg)
	if err != nil {
		return nil, err
	}

	opts := convertOptions(outDir, cfg, force)
	for _, in := range files {
		if err := output.CheckDestination(in, opts); err != nil {
			return nil, fmt.Errorf("%w — turn on `force` to overwrite", err)
		}
	}

	return makeRunner(conv, files, reqFromConfig(cfg, extract), opts), nil
}

// destinationExists reports whether pdf would land on top of markdown that is
// already there, so the browser can flag it before the user hits enter.
func destinationExists(pdf, outDir string, cfg config.Config) bool {
	layout := output.ResolveLayout(pdf, convertOptions(outDir, cfg, false))
	_, err := os.Stat(layout.MarkdownPath)
	return err == nil
}

func convertOptions(outDir string, cfg config.Config, force bool) output.Options {
	return output.Options{
		Output:          outDir,
		Extract:         converter.Extract(strings.ToLower(cfg.Extract)),
		AssetsSubfolder: cfg.AssetsSubfolder,
		Metadata:        cfg.Metadata,
		MovePDF:         cfg.MovePDF,
		DeleteOriginal:  cfg.DeleteOriginal,
		Force:           force,
	}
}
