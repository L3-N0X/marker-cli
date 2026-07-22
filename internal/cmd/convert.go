package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/l3-n0x/marker-cli/internal/config"
	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/output"
	"github.com/l3-n0x/marker-cli/internal/tui"
)

type convertFlags struct {
	inputs       []string
	outputPath   string
	provider     string
	extract      string
	paginate     bool
	imageLimit   int
	imageMinSize int
	assets       bool
	metadata     bool
	movePDF      bool
	deleteOrig   bool
	deleteRemote bool
	force        bool
}

func newConvertCmd() *cobra.Command {
	var f convertFlags

	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert one or more PDFs to Markdown",
		Example: "  marker-cli convert -i paper.pdf -o notes/\n" +
			"  marker-cli convert -i a.pdf -i b.pdf -o notes/\n" +
			"  marker-cli convert -i paper.pdf -o notes/paper.md --extract text",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare positional arguments are treated as inputs too, so
			// `marker-cli convert *.pdf` does the obvious thing.
			f.inputs = append(f.inputs, args...)
			if len(f.inputs) == 0 {
				return cmd.Help()
			}
			return runConvert(cmd, &f)
		},
	}

	defaults := config.Default()
	if cfg, err := config.Load(); err == nil {
		defaults = cfg
	}

	fl := cmd.Flags()
	fl.StringArrayVarP(&f.inputs, "input", "i", nil, "PDF to convert (repeat for several)")
	fl.StringVarP(&f.outputPath, "output", "o", ".", "output directory, or a path ending in .md")
	fl.StringVar(&f.provider, "provider", defaults.Provider, "OCR backend to use ("+providerNames()+")")
	fl.StringVar(&f.extract, "extract", defaults.Extract, "what to extract: all, text or images")
	fl.BoolVar(&f.paginate, "paginate", defaults.Paginate, "insert a horizontal rule between pages")
	fl.IntVar(&f.imageLimit, "image-limit", defaults.ImageLimit, "maximum images to extract (0 = no limit)")
	fl.IntVar(&f.imageMinSize, "image-min-size", defaults.ImageMinSize, "minimum image width/height to extract (0 = no minimum)")
	fl.BoolVar(&f.assets, "assets-subfolder", defaults.AssetsSubfolder, "put images in a separate assets folder")
	fl.BoolVar(&f.metadata, "metadata", defaults.Metadata, "write metadata as YAML frontmatter")
	fl.BoolVar(&f.movePDF, "move-pdf", defaults.MovePDF, "move the source PDF next to the markdown")
	fl.BoolVar(&f.deleteOrig, "delete-original", defaults.DeleteOriginal, "delete the source PDF after conversion")
	fl.BoolVar(&f.deleteRemote, "delete-remote", defaults.DeleteRemote, "delete the uploaded file from the provider afterwards")
	fl.BoolVar(&f.force, "force", false, "overwrite existing markdown files")

	return cmd
}

func runConvert(cmd *cobra.Command, f *convertFlags) error {
	extract := converter.Extract(strings.ToLower(f.extract))
	if !extract.Valid() {
		return fmt.Errorf("invalid --extract %q: use all, text or images", f.extract)
	}
	if f.movePDF && f.deleteOrig {
		return errors.New("--move-pdf and --delete-original are mutually exclusive")
	}

	inputs, err := validateInputs(f.inputs)
	if err != nil {
		return err
	}

	conv, err := newConverter(f.provider)
	if err != nil {
		return err
	}

	opts := output.Options{
		Output:          f.outputPath,
		Extract:         extract,
		AssetsSubfolder: f.assets,
		Metadata:        f.metadata,
		MovePDF:         f.movePDF,
		DeleteOriginal:  f.deleteOrig,
		Force:           f.force,
	}

	// Refuse to clobber before spending any upload or OCR time.
	for _, in := range inputs {
		if err := output.CheckDestination(in, opts); err != nil {
			return err
		}
	}

	run := makeRunner(conv, inputs, converter.Request{
		Extract:      extract,
		Paginate:     f.paginate,
		ImageLimit:   f.imageLimit,
		ImageMinSize: f.imageMinSize,
		DeleteRemote: f.deleteRemote,
	}, opts)

	names := make([]string, len(inputs))
	for i, in := range inputs {
		names[i] = filepath.Base(in)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var results []tui.JobResult
	if useTUI() {
		results, err = tui.RunConversions(ctx, names, run)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	} else {
		results = runPlain(ctx, names, run)
	}

	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d conversions failed", failed, len(inputs))
	}
	if len(results) < len(inputs) {
		return errors.New("cancelled")
	}
	return nil
}

// makeRunner builds the unit of work the progress view drives: convert
// inputs[i] with the shared request template, then write it out.
func makeRunner(conv converter.Converter, inputs []string, req converter.Request, opts output.Options) tui.Runner {
	return func(ctx context.Context, i int, progress chan<- converter.Progress) (string, error) {
		r := req
		r.Path = inputs[i]

		res, err := conv.Convert(ctx, r, progress)
		if err != nil {
			return "", err
		}
		written, err := output.Write(inputs[i], res, opts)
		if err != nil {
			return "", err
		}
		summary := written.MarkdownPath
		if n := len(written.ImagePaths); n > 0 {
			summary += fmt.Sprintf(" (+%d images)", n)
		}
		return summary, nil
	}
}

// runPlain is the non-interactive path: plain lines on stderr, so stdout stays
// clean for pipes.
func runPlain(ctx context.Context, names []string, run tui.Runner) []tui.JobResult {
	results := make([]tui.JobResult, 0, len(names))

	for i, name := range names {
		progress := make(chan converter.Progress)
		go func() {
			for p := range progress {
				if flagVerbose {
					detail := p.Detail
					if detail != "" {
						detail = " (" + detail + ")"
					}
					fmt.Fprintf(os.Stderr, "  %s%s\n", p.Stage, detail)
				}
			}
		}()

		fmt.Fprintf(os.Stderr, "Converting %s…\n", name)
		summary, err := run(ctx, i, progress)
		close(progress)

		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s: %v\n", name, err)
		} else {
			fmt.Fprintf(os.Stderr, "✓ %s → %s\n", name, summary)
		}
		results = append(results, tui.JobResult{Name: name, Summary: summary, Err: err})

		if ctx.Err() != nil {
			break
		}
	}
	return results
}

// validateInputs checks each path exists, is a regular file and looks like a
// PDF, so a typo fails before any upload happens.
func validateInputs(inputs []string) ([]string, error) {
	seen := make(map[string]bool, len(inputs))
	out := make([]string, 0, len(inputs))

	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			return nil, fmt.Errorf("input %s: %w", in, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("input %s is a directory, not a PDF", in)
		}
		if !strings.EqualFold(filepath.Ext(in), ".pdf") {
			return nil, fmt.Errorf("input %s is not a .pdf file", in)
		}
		abs, err := filepath.Abs(in)
		if err != nil {
			return nil, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, in)
	}

	if len(out) == 0 {
		return nil, errors.New("no input PDFs given (use -i)")
	}
	return out, nil
}

// useTUI reports whether the interactive UI should be shown. Piped or
// redirected output falls back to plain logging.
func useTUI() bool {
	if flagNoTUI {
		return false
	}
	return term.IsTerminal(os.Stdout.Fd()) && term.IsTerminal(os.Stdin.Fd())
}
