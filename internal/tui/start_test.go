package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/l3-n0x/marker-cli/internal/config"
	"github.com/l3-n0x/marker-cli/internal/converter"
)

// newTestModel builds a browser over a temp directory holding the named PDFs.
func newTestModel(t *testing.T, opts StartOptions, files ...string) startModel {
	t.Helper()

	dir := t.TempDir()
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("%PDF-1.4\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	opts.Dir = dir
	if opts.Config == (config.Config{}) {
		opts.Config = config.Default()
	}

	m, err := newStartModel(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	return next.(startModel)
}

// press feeds a key to the model, the way Bubble Tea would.
func press(t *testing.T, m startModel, key string) startModel {
	t.Helper()

	msg := tea.KeyPressMsg{Text: key, Code: rune(key[0])}
	switch key {
	case "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	}

	next, _ := m.handleKey(msg)
	out, ok := next.(startModel)
	if !ok {
		t.Fatalf("handleKey(%q) returned %T", key, next)
	}
	return out
}

func TestSelectAndConvert(t *testing.T) {
	var got []string
	prepared := 0

	m := newTestModel(t, StartOptions{
		Prepare: func(files []string, cfg config.Config, outDir string, force bool) (Runner, error) {
			prepared++
			got = append([]string(nil), files...)
			return func(ctx context.Context, i int, progress chan<- converter.Progress) (string, error) {
				return filepath.Join(outDir, "out.md"), nil
			}, nil
		},
	}, "a.pdf", "b.pdf", "c.pdf")

	// Select the first and third PDF; space also advances the cursor.
	m = press(t, m, "space")
	m = press(t, m, "down")
	m = press(t, m, "space")

	if len(m.selected) != 2 {
		t.Fatalf("selected %d files, want 2", len(m.selected))
	}

	m = press(t, m, "enter")
	if !m.stateIs(stateRunning) {
		t.Fatalf("state is %v, want running", m.st)
	}
	if prepared != 1 {
		t.Fatalf("Prepare called %d times, want 1", prepared)
	}
	if len(got) != 2 || filepath.Base(got[0]) != "a.pdf" || filepath.Base(got[1]) != "c.pdf" {
		t.Fatalf("converted %v, want a.pdf and c.pdf", got)
	}
}

// TestRunCycleReturnsToBrowser walks a whole conversion: the job starts,
// finishes, the results screen appears, and dismissing it drops the converted
// file from the selection.
func TestRunCycleReturnsToBrowser(t *testing.T) {
	m := newTestModel(t, StartOptions{
		Prepare: func([]string, config.Config, string, bool) (Runner, error) {
			return func(context.Context, int, chan<- converter.Progress) (string, error) {
				return "notes/a.md", nil
			}, nil
		},
	}, "a.pdf")

	m = press(t, m, "space")
	m = press(t, m, "enter")

	// Start the only job, then hand its outcome back the way the commands do.
	next, _ := m.Update(startJobMsg{})
	m = next.(startModel)
	result := <-m.prog.done

	next, _ = m.Update(jobDoneMsg(result))
	m = next.(startModel)
	next, _ = m.Update(startJobMsg{})
	m = next.(startModel)
	next, _ = m.Update(allDoneMsg{})
	m = next.(startModel)

	if !m.stateIs(stateResults) {
		t.Fatalf("state is %v, want results", m.st)
	}
	if len(m.prog.results) != 1 || m.prog.results[0].Err != nil {
		t.Fatalf("results = %+v, want one success", m.prog.results)
	}
	if !strings.Contains(m.render(), "a.pdf") {
		t.Fatal("results view does not mention the converted file")
	}

	m = press(t, m, "enter")
	if !m.stateIs(stateBrowse) {
		t.Fatalf("state is %v, want browse", m.st)
	}
	if len(m.selected) != 0 {
		t.Fatalf("selection = %v, want it cleared after a clean run", m.selected)
	}
}

func TestPrepareFailureStaysInBrowser(t *testing.T) {
	m := newTestModel(t, StartOptions{
		Prepare: func([]string, config.Config, string, bool) (Runner, error) {
			return nil, errors.New("no mistral API key found")
		},
	}, "a.pdf")

	m = press(t, m, "space")
	m = press(t, m, "enter")

	if !m.stateIs(stateBrowse) {
		t.Fatalf("state is %v, want browse", m.st)
	}
	if !m.statusErr || !strings.Contains(m.status, "API key") {
		t.Fatalf("status = %q (err=%v), want the prepare error", m.status, m.statusErr)
	}
}

func TestConvertWithNoSelectionUsesCursor(t *testing.T) {
	var got []string
	m := newTestModel(t, StartOptions{
		Prepare: func(files []string, _ config.Config, _ string, _ bool) (Runner, error) {
			got = files
			return func(context.Context, int, chan<- converter.Progress) (string, error) { return "", nil }, nil
		},
	}, "only.pdf")

	m = press(t, m, "enter")
	if len(got) != 1 || filepath.Base(got[0]) != "only.pdf" {
		t.Fatalf("converted %v, want only.pdf", got)
	}
}

func TestFolderPromptConvertsIntoSubdirectory(t *testing.T) {
	var outDir string
	m := newTestModel(t, StartOptions{
		Prepare: func(_ []string, _ config.Config, dir string, _ bool) (Runner, error) {
			outDir = dir
			return func(context.Context, int, chan<- converter.Progress) (string, error) { return "", nil }, nil
		},
	}, "a.pdf")

	m = press(t, m, "f")
	if !m.stateIs(stateFolder) {
		t.Fatalf("state is %v, want folder prompt", m.st)
	}
	for _, r := range "notes" {
		m = press(t, m, string(r))
	}
	m = press(t, m, "enter")

	if want := filepath.Join(m.dir, "notes"); outDir != want {
		t.Fatalf("output dir = %q, want %q", outDir, want)
	}
}

func TestOptionLaddersAndExclusivity(t *testing.T) {
	m := newTestModel(t, StartOptions{}, "a.pdf")
	m.focus = paneConfig

	// extract wraps through its three values.
	m.optCursor = indexOfSpec("extract")
	for _, want := range []string{"text", "images", "all"} {
		m.adjustOption(1)
		if m.cfg.Extract != want {
			t.Fatalf("extract = %q, want %q", m.cfg.Extract, want)
		}
	}

	// Numeric ladders clamp instead of wrapping.
	m.optCursor = indexOfSpec("image-limit")
	m.adjustOption(-1)
	if m.cfg.ImageLimit != 0 {
		t.Fatalf("image-limit = %d, want it clamped at 0", m.cfg.ImageLimit)
	}
	m.adjustOption(1)
	if m.cfg.ImageLimit != 1 {
		t.Fatalf("image-limit = %d, want 1", m.cfg.ImageLimit)
	}

	// move-pdf and delete-original cannot both be on.
	m.optCursor = indexOfSpec("move-pdf")
	m.adjustOption(1)
	m.optCursor = indexOfSpec("delete-original")
	m.adjustOption(1)
	if !m.cfg.DeleteOriginal || m.cfg.MovePDF {
		t.Fatalf("move-pdf=%v delete-original=%v, want only delete-original", m.cfg.MovePDF, m.cfg.DeleteOriginal)
	}
}

// TestOffLadderValueSnapsToNearest covers a config file holding a value that is
// not one of the rungs, e.g. image-min-size 120.
func TestOffLadderValueSnapsToNearest(t *testing.T) {
	cfg := config.Default()
	cfg.ImageMinSize = 120

	m := newTestModel(t, StartOptions{Config: cfg}, "a.pdf")
	m.optCursor = indexOfSpec("image-min-size")
	m.adjustOption(1)

	if m.cfg.ImageMinSize != 200 {
		t.Fatalf("image-min-size = %d, want 200", m.cfg.ImageMinSize)
	}
}

func TestDirectoryNavigation(t *testing.T) {
	m := newTestModel(t, StartOptions{}, "a.pdf")
	sub := filepath.Join(m.dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m.reload()
	m.focusFirstFile()

	// Rows are: .., sub/, a.pdf, and the cursor opens on the PDF.
	m = press(t, m, "up")
	m = press(t, m, "enter")
	if m.dir != sub {
		t.Fatalf("dir = %q, want %q", m.dir, sub)
	}

	m = press(t, m, "backspace")
	if m.dir == sub {
		t.Fatal("backspace did not climb out of the subdirectory")
	}
	if e, ok := m.current(); !ok || e.name != "sub" {
		t.Fatalf("cursor on %+v, want the sub folder we came from", e)
	}
}

func indexOfSpec(key string) int {
	for i, s := range optionSpecs {
		if s.key == key {
			return i
		}
	}
	return -1
}
