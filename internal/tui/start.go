package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/l3-n0x/marker-cli/internal/config"
)

// StartOptions injects everything the browser needs from the command layer, so
// this package stays free of provider and output-layout knowledge.
type StartOptions struct {
	// Dir is the directory to open in.
	Dir string
	// Config holds the settings the browser starts with.
	Config config.Config
	// Providers lists the selectable backend names.
	Providers []string
	// Prepare turns a request into a Runner, or fails fast when something is
	// missing — no API key, a destination that already exists, and so on.
	Prepare func(files []string, cfg config.Config, outDir string, force bool) (Runner, error)
	// Converted reports whether pdf already has markdown at its destination.
	Converted func(pdf, outDir string, cfg config.Config) bool
	// Save persists cfg as the new defaults.
	Save func(cfg config.Config) error
}

// startState is the screen the browser is currently on.
type startState int

const (
	stateBrowse startState = iota
	stateFilter
	stateFolder
	stateRunning
	stateResults
)

// pane identifies the focused half of the split view.
type pane int

const (
	paneFiles pane = iota
	paneConfig
)

// fileEntry is one row in the file panel.
type fileEntry struct {
	name  string
	path  string // absolute
	isDir bool
	up    bool // the ".." row
	size  int64
}

type startModel struct {
	opts StartOptions
	ctx  context.Context

	st    startState
	cfg   config.Config
	force bool // session-only: overwrite existing markdown
	dirty bool // config changed since the last save

	dir      string
	entries  []fileEntry
	selected []string // absolute paths, in the order they were picked

	focus     pane
	cursor    int
	offset    int
	optCursor int

	filter  textinput.Model
	folder  textinput.Model
	loadErr error

	status    string
	statusErr bool

	prog    progressModel
	outDir  string
	running bool

	width, height, listHeight int
}

// RunStart opens the interactive file browser.
func RunStart(ctx context.Context, opts StartOptions) error {
	m, err := newStartModel(ctx, opts)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func newStartModel(ctx context.Context, opts StartOptions) (startModel, error) {
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return startModel{}, err
	}

	filter := textinput.New()
	filter.Placeholder = "filter"
	filter.SetWidth(24)
	styleInput(&filter)

	folder := textinput.New()
	folder.Placeholder = "notes/papers"
	folder.SetWidth(36)
	styleInput(&folder)

	m := startModel{
		opts:       opts,
		ctx:        ctx,
		cfg:        opts.Config,
		dir:        dir,
		filter:     filter,
		folder:     folder,
		listHeight: 10,
	}
	m.reload()
	m.focusFirstFile()
	return m, nil
}

func (m startModel) Init() tea.Cmd { return nil }

func (m startModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// title + blank (2), panel borders (2), blank + status + help (3)
		m.listHeight = max(3, msg.Height-8)
		m.clampCursor()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case allDoneMsg:
		m.running = false
		m.state(stateResults)
		return m, nil
	}

	if m.stateIs(stateRunning) {
		var cmd tea.Cmd
		m.prog, cmd = m.prog.update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *startModel) state(s startState)       { m.st = s }
func (m startModel) stateIs(s startState) bool { return m.st == s }

// current is the entry under the cursor, if any.
func (m startModel) current() (fileEntry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return fileEntry{}, false
	}
	return m.entries[m.cursor], true
}

// ---------------------------------------------------------------- key input

func (m startModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Ctrl+C always gets out, whatever is on screen.
	if key == "ctrl+c" {
		if m.running {
			m.prog = m.prog.stop()
		}
		return m, tea.Quit
	}

	switch m.st {
	case stateFilter:
		switch key {
		case "esc":
			m.filter.SetValue("")
			m.filter.Blur()
			m.state(stateBrowse)
			m.reload()
			return m, nil
		case "enter":
			m.filter.Blur()
			m.state(stateBrowse)
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.reload()
		return m, cmd

	case stateFolder:
		switch key {
		case "esc":
			m.folder.Blur()
			m.state(stateBrowse)
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.folder.Value())
			if name == "" {
				m.fail("enter a folder name, or press esc to cancel")
				return m, nil
			}
			m.folder.Blur()
			return m.startRun(name)
		}
		var cmd tea.Cmd
		m.folder, cmd = m.folder.Update(msg)
		return m, cmd

	case stateRunning:
		if key == "esc" {
			m.prog = m.prog.stop()
			return m, nil
		}
		var cmd tea.Cmd
		m.prog, cmd = m.prog.update(msg)
		return m, cmd

	case stateResults:
		// Any key returns to the browser.
		m.state(stateBrowse)
		m.afterRun()
		return m, nil
	}

	// Browsing.
	switch key {
	case "q":
		return m, tea.Quit
	case "tab", "shift+tab":
		// Tab is the only way to move between the panes, so that left/right
		// keep a single meaning inside whichever pane has focus.
		m.focus = 1 - m.focus
		return m, nil
	case "left", "right", "h", "l":
		// In the config pane they adjust the value under the cursor; in the
		// file list they walk the tree, as in a file manager: left goes to the
		// parent, right descends into the folder under the cursor.
		if m.focus == paneConfig {
			m.adjustOption(delta(key))
			return m, nil
		}
		if delta(key) < 0 {
			m.enter(filepath.Dir(m.dir))
			return m, nil
		}
		if e, ok := m.current(); ok && e.isDir {
			m.enter(e.path)
		}
		return m, nil
	case "up", "k", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "j", "ctrl+n":
		m.move(1)
		return m, nil
	case "pgup":
		m.move(-m.listHeight)
		return m, nil
	case "pgdown":
		m.move(m.listHeight)
		return m, nil
	case "home", "g":
		m.move(-1 << 30)
		return m, nil
	case "end", "G":
		m.move(1 << 30)
		return m, nil
	}

	if m.focus == paneConfig {
		return m.configKey(key)
	}
	return m.filesKey(key)
}

func (m startModel) filesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "space":
		e, ok := m.current()
		if !ok || e.isDir {
			return m, nil
		}
		m.toggle(e.path)
		m.move(1)

	case "enter":
		e, ok := m.current()
		if ok && e.isDir {
			m.enter(e.path)
			return m, nil
		}
		return m.startRun("")

	case "backspace", "-":
		m.enter(filepath.Dir(m.dir))

	case "f":
		m.folder.SetValue("")
		m.folder.Focus()
		m.state(stateFolder)
		return m, textinput.Blink

	case "/":
		m.filter.Focus()
		m.state(stateFilter)
		return m, textinput.Blink

	case "a":
		m.toggleAll()

	case "c":
		m.selected = nil
		m.note("selection cleared")

	case "r":
		m.reload()
		m.note("reloaded " + m.dir)
	}
	return m, nil
}

func (m startModel) configKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "space", "enter":
		m.adjustOption(1)
	case "s":
		if m.opts.Save == nil {
			return m, nil
		}
		if err := m.opts.Save(m.cfg); err != nil {
			m.fail(err.Error())
			return m, nil
		}
		m.dirty = false
		m.note("saved as defaults")
	case "R":
		m.cfg = config.Default()
		m.dirty = true
		m.note("reset to built-in defaults (press s to save)")
	}
	return m, nil
}

// delta maps a horizontal key to a step through an option's values.
func delta(key string) int {
	if key == "left" || key == "h" {
		return -1
	}
	return 1
}

// move walks the cursor of the focused panel and keeps the scroll window on it.
func (m *startModel) move(n int) {
	if m.focus == paneConfig {
		m.optCursor = clamp(m.optCursor+n, 0, len(optionSpecs)-1)
		return
	}
	m.cursor = clamp(m.cursor+n, 0, len(m.entries)-1)
	m.clampCursor()
}

func (m *startModel) clampCursor() {
	m.cursor = clamp(m.cursor, 0, max(0, len(m.entries)-1))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.listHeight {
		m.offset = m.cursor - m.listHeight + 1
	}
	m.offset = clamp(m.offset, 0, max(0, len(m.entries)-m.listHeight))
}

// ------------------------------------------------------------- file listing

// reload rereads the current directory, applying the filter.
func (m *startModel) reload() {
	m.entries = nil
	m.loadErr = nil

	items, err := os.ReadDir(m.dir)
	if err != nil {
		m.loadErr = err
		return
	}

	if parent := filepath.Dir(m.dir); parent != m.dir {
		m.entries = append(m.entries, fileEntry{name: "..", path: parent, isDir: true, up: true})
	}

	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	var dirs, files []fileEntry

	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		path := filepath.Join(m.dir, name)

		if it.IsDir() {
			dirs = append(dirs, fileEntry{name: name, path: path, isDir: true})
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".pdf") {
			continue
		}
		var size int64
		if info, err := it.Info(); err == nil {
			size = info.Size()
		}
		files = append(files, fileEntry{name: name, path: path, size: size})
	}

	sort.Slice(dirs, func(i, j int) bool { return less(dirs[i].name, dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return less(files[i].name, files[j].name) })

	m.entries = append(m.entries, dirs...)
	m.entries = append(m.entries, files...)
	m.clampCursor()
}

// enter descends into (or climbs out of) dir, resetting the cursor.
func (m *startModel) enter(dir string) {
	prev := m.dir
	m.dir = dir
	m.cursor, m.offset = 0, 0
	m.filter.SetValue("")
	m.reload()

	if m.loadErr != nil {
		m.fail(m.loadErr.Error())
		m.dir = prev
		m.reload()
		return
	}
	// Coming back up, land on the folder we just left; going down, skip the
	// directory rows and land on the first PDF.
	for i, e := range m.entries {
		if e.path == prev && !e.up {
			m.cursor = i
			m.clampCursor()
			return
		}
	}
	m.focusFirstFile()
}

// focusFirstFile puts the cursor on the first PDF, so that a fresh directory
// opens ready to select rather than sitting on "..".
func (m *startModel) focusFirstFile() {
	for i, e := range m.entries {
		if !e.isDir {
			m.cursor = i
			break
		}
	}
	m.clampCursor()
}

func less(a, b string) bool { return strings.ToLower(a) < strings.ToLower(b) }

// ---------------------------------------------------------------- selection

func (m *startModel) isSelected(path string) bool {
	for _, p := range m.selected {
		if p == path {
			return true
		}
	}
	return false
}

func (m *startModel) toggle(path string) {
	for i, p := range m.selected {
		if p == path {
			m.selected = append(m.selected[:i], m.selected[i+1:]...)
			return
		}
	}
	m.selected = append(m.selected, path)
}

// toggleAll selects every PDF in view, or clears them if they are all selected.
func (m *startModel) toggleAll() {
	all := true
	n := 0
	for _, e := range m.entries {
		if e.isDir {
			continue
		}
		n++
		if !m.isSelected(e.path) {
			all = false
		}
	}
	if n == 0 {
		return
	}
	for _, e := range m.entries {
		if e.isDir {
			continue
		}
		if all == m.isSelected(e.path) {
			m.toggle(e.path)
		}
	}
}

// targets is what a conversion would run on: the selection, or else the file
// under the cursor.
func (m startModel) targets() []string {
	if len(m.selected) > 0 {
		return m.selected
	}
	if e, ok := m.current(); ok && !e.isDir {
		return []string{e.path}
	}
	return nil
}

// ---------------------------------------------------------------- running

// startRun kicks off a conversion of the current targets into subdir (relative
// to the browsed directory; empty means "right here").
func (m startModel) startRun(subdir string) (tea.Model, tea.Cmd) {
	files := m.targets()
	if len(files) == 0 {
		m.fail("nothing to convert — select a PDF with space")
		m.state(stateBrowse)
		return m, nil
	}

	outDir := m.dir
	if subdir != "" {
		outDir = filepath.Join(m.dir, subdir)
	}

	run, err := m.opts.Prepare(files, m.cfg, outDir, m.force)
	if err != nil {
		m.fail(err.Error())
		m.state(stateBrowse)
		return m, nil
	}

	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}

	m.outDir = outDir
	m.prog = newProgressModel(m.ctx, names, run)
	m.running = true
	m.status, m.statusErr = "", false
	m.state(stateRunning)
	return m, m.prog.Init()
}

// afterRun drops the files that converted cleanly from the selection and
// refreshes the listing, since conversions may have moved or deleted PDFs.
func (m *startModel) afterRun() {
	ok := make(map[string]bool, len(m.prog.results))
	for _, r := range m.prog.results {
		if r.Err == nil {
			ok[r.Name] = true
		}
	}
	kept := m.selected[:0]
	for _, p := range m.selected {
		if !ok[filepath.Base(p)] {
			kept = append(kept, p)
		}
	}
	m.selected = kept

	failed := 0
	for _, r := range m.prog.results {
		if r.Err != nil {
			failed++
		}
	}
	switch {
	case m.prog.cancelled:
		m.fail("cancelled")
	case failed > 0:
		m.fail(fmt.Sprintf("%d of %d conversions failed", failed, len(m.prog.results)))
	case len(m.prog.results) > 0:
		m.note(fmt.Sprintf("converted %d file(s) → %s", len(m.prog.results), prettyPath(m.outDir)))
	}
	m.reload()
}

// ------------------------------------------------------------------- status

func (m *startModel) note(s string) { m.status, m.statusErr = s, false }
func (m *startModel) fail(s string) { m.status, m.statusErr = s, true }

// ---------------------------------------------------------- config options

type optionKind int

const (
	optBool optionKind = iota
	optEnum
	optInt
)

// optionSpec describes one row of the settings panel. Every option steps
// through a fixed ladder of values, so ← and → are always meaningful and no
// value can ever be invalid.
type optionSpec struct {
	key    string
	desc   string
	kind   optionKind
	values []string // nil for provider, which is filled in at runtime
	// session marks options that are not part of the persisted config.
	session bool
}

var optionSpecs = []optionSpec{
	{key: "provider", desc: "OCR backend", kind: optEnum},
	{key: "extract", desc: "what to pull out of the PDF", kind: optEnum, values: []string{"all", "text", "images"}},
	{key: "paginate", desc: "rule between pages", kind: optBool},
	{key: "image-limit", desc: "max images (0 = all)", kind: optInt, values: []string{"0", "1", "2", "5", "10", "20", "50", "100"}},
	{key: "image-min-size", desc: "skip images smaller than", kind: optInt, values: []string{"0", "50", "100", "200", "300", "500", "1000"}},
	{key: "assets-subfolder", desc: "images in their own folder", kind: optBool},
	{key: "metadata", desc: "YAML frontmatter", kind: optBool},
	{key: "move-pdf", desc: "move the PDF next to the markdown", kind: optBool},
	{key: "delete-original", desc: "delete the PDF afterwards", kind: optBool},
	{key: "delete-remote", desc: "delete the upload afterwards", kind: optBool},
	{key: "force", desc: "overwrite existing markdown", kind: optBool, session: true},
}

var boolValues = []string{"false", "true"}

// choices returns the ladder for spec, resolving the runtime-dependent ones.
func (m startModel) choices(spec optionSpec) []string {
	switch {
	case spec.kind == optBool:
		return boolValues
	case spec.key == "provider":
		if len(m.opts.Providers) == 0 {
			return []string{m.cfg.Provider}
		}
		return m.opts.Providers
	default:
		return spec.values
	}
}

func (m startModel) optionValue(spec optionSpec) string {
	c := m.cfg
	switch spec.key {
	case "provider":
		return c.Provider
	case "extract":
		return c.Extract
	case "paginate":
		return strconv.FormatBool(c.Paginate)
	case "image-limit":
		return strconv.Itoa(c.ImageLimit)
	case "image-min-size":
		return strconv.Itoa(c.ImageMinSize)
	case "assets-subfolder":
		return strconv.FormatBool(c.AssetsSubfolder)
	case "metadata":
		return strconv.FormatBool(c.Metadata)
	case "move-pdf":
		return strconv.FormatBool(c.MovePDF)
	case "delete-original":
		return strconv.FormatBool(c.DeleteOriginal)
	case "delete-remote":
		return strconv.FormatBool(c.DeleteRemote)
	case "force":
		return strconv.FormatBool(m.force)
	}
	return ""
}

func (m *startModel) setOptionValue(spec optionSpec, v string) {
	b := v == "true"
	n, _ := strconv.Atoi(v)

	switch spec.key {
	case "provider":
		m.cfg.Provider = v
	case "extract":
		m.cfg.Extract = v
	case "paginate":
		m.cfg.Paginate = b
	case "image-limit":
		m.cfg.ImageLimit = n
	case "image-min-size":
		m.cfg.ImageMinSize = n
	case "assets-subfolder":
		m.cfg.AssetsSubfolder = b
	case "metadata":
		m.cfg.Metadata = b
	case "move-pdf":
		m.cfg.MovePDF = b
		// The two are mutually exclusive, so turning one on turns the other off
		// rather than failing at conversion time.
		if b {
			m.cfg.DeleteOriginal = false
		}
	case "delete-original":
		m.cfg.DeleteOriginal = b
		if b {
			m.cfg.MovePDF = false
		}
	case "delete-remote":
		m.cfg.DeleteRemote = b
	case "force":
		m.force = b
	}
	if !spec.session {
		m.dirty = true
	}
}

// adjustOption steps the option under the config cursor by n. Booleans and
// enums wrap around; numeric ladders clamp at their ends.
func (m *startModel) adjustOption(n int) {
	if m.optCursor >= len(optionSpecs) {
		return
	}
	spec := optionSpecs[m.optCursor]
	values := m.choices(spec)
	if len(values) == 0 {
		return
	}

	cur := m.optionValue(spec)
	i := indexOf(values, cur)
	if i < 0 {
		// A value from the config file that is not on the ladder: start from
		// the nearest rung so the first keypress still does something sane.
		i = nearest(values, cur)
	}

	if spec.kind == optInt {
		i = clamp(i+n, 0, len(values)-1)
	} else {
		i = ((i+n)%len(values) + len(values)) % len(values)
	}
	m.setOptionValue(spec, values[i])
}

func indexOf(values []string, v string) int {
	for i, s := range values {
		if s == v {
			return i
		}
	}
	return -1
}

// nearest finds the rung closest to a numeric value that is off the ladder.
func nearest(values []string, v string) int {
	want, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	best, bestDist := 0, 1<<62
	for i, s := range values {
		n, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		if d := abs(n - want); d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// ------------------------------------------------------------------ helpers

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// prettyPath shortens a path for display, using ~ and dropping the browsed
// directory prefix where it helps.
func prettyPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return filepath.Join("~", rel)
		}
	}
	return p
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
