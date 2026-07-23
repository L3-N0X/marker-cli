package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m startModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m startModel) render() string {
	if m.width == 0 {
		return "loading…"
	}

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")

	switch m.st {
	case stateRunning, stateResults:
		b.WriteString(m.runView())
	default:
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, m.filesPanel(), m.configPanel()))
	}

	b.WriteString("\n\n")
	b.WriteString(m.statusLine())
	b.WriteString("\n")
	b.WriteString(m.helpLine())
	return b.String()
}

// widths splits the terminal into the two panel widths, borders included.
func (m startModel) widths() (int, int) {
	total := max(m.width, 50)
	right := clamp(total*2/5, 30, 44)
	left := total - right
	return left, right
}

func (m startModel) header() string {
	title := titleStyle.Render("marker-cli")
	// Paths are truncated from the left: the folder you are in matters more
	// than the root it hangs off.
	dir := dimStyle.Render(truncateLeft(prettyPath(m.dir), max(20, m.width-30)))

	right := ""
	if n := len(m.selected); n > 0 {
		right = cursorStyle.Render(fmt.Sprintf("%d selected", n))
	}

	left := title + dimStyle.Render(" · ") + dir
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// ------------------------------------------------------------- files panel

func (m startModel) filesPanel() string {
	w, _ := m.widths()
	inner := w - 4

	head := sectionTitle("FILES", m.focus == paneFiles)
	if f := m.filter.Value(); f != "" {
		head += dimStyle.Render("  /" + f)
	}
	lines := []string{head}

	switch {
	case m.loadErr != nil:
		lines = append(lines, errorStyle.Render(truncate(m.loadErr.Error(), inner)))
	case len(m.entries) == 0:
		lines = append(lines, labelStyle.Render("no PDFs here — press / to clear the filter"))
	}

	end := min(m.offset+m.listHeight, len(m.entries))
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.fileRow(m.entries[i], i == m.cursor, inner))
	}

	// A hint that the list scrolls past the window.
	if len(m.entries) > m.listHeight {
		lines[0] = padRight(lines[0], inner-8) +
			dimStyle.Render(fmt.Sprintf("%3d/%-3d", m.cursor+1, len(m.entries)))
	}

	return m.panel(lines, w, m.focus == paneFiles)
}

func (m startModel) fileRow(e fileEntry, atCursor bool, width int) string {
	cursor := "  "
	if atCursor {
		cursor = "❯ "
		if m.focus != paneFiles {
			cursor = "· "
		}
	}

	if e.isDir {
		name := e.name + "/"
		if e.up {
			name = ".."
		}
		// The row under the cursor swaps blue for the accent, the same way file
		// rows do — bold-on-blue was too close to a plain folder to read as a
		// highlight. The trailing "/" still marks it as a directory.
		if atCursor && m.focus == paneFiles {
			return cursorStyle.Render(cursor) + "    " + cursorStyle.Render(truncate(name, width-6))
		}
		return cursor + "    " + dirStyle.Render(truncate(name, width-6))
	}

	box := "[ ] "
	if m.isSelected(e.path) {
		box = successStyle.Render("[✓] ")
	}

	// Tail columns are dropped first when the panel is narrow.
	size := humanSize(e.size)
	tag := ""
	if m.opts.Converted != nil && m.opts.Converted(e.path, m.dir, m.cfg) {
		tag = "exists"
		if m.force {
			tag = "replace"
		}
	}

	tail := ""
	switch {
	case width >= 46:
		tail = fmt.Sprintf("%9s  %-7s", size, tag)
	case width >= 34:
		tail = fmt.Sprintf("%9s", size)
	}

	nameWidth := width - 6 - lipgloss.Width(tail)
	name := padRight(truncate(e.name, nameWidth), nameWidth)

	styled := name
	if atCursor && m.focus == paneFiles {
		styled = cursorStyle.Render(name)
		cursor = cursorStyle.Render(cursor)
	}

	tailStyled := dimStyle.Render(tail)
	if tag == "exists" {
		tailStyled = dimStyle.Render(fmt.Sprintf("%9s  ", size)) + warnStyle.Render(padRight(tag, 7))
	} else if tag == "replace" {
		tailStyled = dimStyle.Render(fmt.Sprintf("%9s  ", size)) + errorStyle.Render(padRight(tag, 7))
	}

	return cursor + box + styled + tailStyled
}

// ------------------------------------------------------------ config panel

func (m startModel) configPanel() string {
	_, w := m.widths()
	inner := w - 4

	// Top block: the option list, followed by a short description of the option
	// under the cursor so the user can see what each setting does.
	top := []string{sectionTitle("SETTINGS", m.focus == paneConfig)}

	specs := m.visibleSpecs()
	for i, spec := range specs {
		top = append(top, m.optionRow(spec, i == m.optCursor, inner))
	}

	if m.optCursor < len(specs) {
		top = append(top, "", dimStyle.Render(strings.Repeat("─", inner)))
		for _, line := range wrap(specs[m.optCursor].desc, inner) {
			top = append(top, labelStyle.Render(line))
		}
	}

	// Footer block: output location, what is queued, and the unsaved marker,
	// pinned to the bottom so the panel reads as list-then-status.
	footer := []string{dimStyle.Render(strings.Repeat("─", inner))}
	footer = append(footer, dimStyle.Render("output  ")+truncate("→ "+prettyPath(m.dir), inner-8))

	n := len(m.targets())
	summary := fmt.Sprintf("%d file(s) queued", n)
	if len(m.selected) == 0 && n == 1 {
		summary = "1 file under the cursor"
	}
	footer = append(footer, dimStyle.Render("queue   ")+truncate(summary, inner-8))

	if m.dirty {
		footer = append(footer, warnStyle.Render("unsaved — press s to keep these"))
	}

	// Fit the top block into the space above the footer, then pad so the footer
	// sits at the bottom of the fixed-height panel. When the option list is long
	// the description is what gets dropped first, since it is only a hint.
	avail := max(1, m.panelHeight()-len(footer))
	if len(top) > avail {
		top = top[:avail]
	}
	for len(top) < avail {
		top = append(top, "")
	}

	return m.panel(append(top, footer...), w, m.focus == paneConfig)
}

func (m startModel) optionRow(spec optionSpec, atCursor bool, width int) string {
	cursor := "  "
	if atCursor {
		cursor = "❯ "
		if m.focus != paneConfig {
			cursor = "· "
		}
	}

	value := m.optionValue(spec)
	label := spec.key
	if spec.session {
		label += "*"
	}

	// The label is chrome and the value is the data, so the value keeps the
	// terminal's own foreground and only the row under the cursor takes the
	// accent. Colouring every value made the pane read as uniformly orange.
	labelFace := dimStyle
	if atCursor && m.focus == paneConfig {
		labelFace = cursorStyle
		cursor = cursorStyle.Render(cursor)
	}

	var mark, rendered string
	if spec.kind == optBool {
		if value == "true" {
			mark = successStyle.Render("✓ ")
		} else {
			mark = dimStyle.Render("✗ ")
		}
		rendered = padRight(labelFace.Render(truncate(label, width-4)), width-4)
	} else {
		mark = "  "
		labelWidth := min(17, max(8, width-12))
		// Truncate one cell short of the column so the value never runs into
		// the label on a narrow pane.
		rendered = padRight(labelFace.Render(truncate(label, labelWidth-1)), labelWidth) +
			truncate(value, width-4-labelWidth)
	}

	return cursor + mark + rendered
}

// ------------------------------------------------------------ running view

func (m startModel) runView() string {
	body := m.prog.render()
	if m.st == stateResults {
		if m.prog.cancelled {
			body += "\n" + warnStyle.Render("cancelled")
		}
		body += "\n" + dimStyle.Render("press any key to go back to the file list")
	}

	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	// Long runs scroll: keep the newest lines rather than the oldest.
	if room := m.panelHeight() - 2; room > 0 && len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	lines = append([]string{sectionTitle("CONVERTING", true), ""}, lines...)
	return m.panel(lines, m.width, true)
}

// ------------------------------------------------------------------ chrome

// panel frames content lines in a fixed-height box of the given outer width.
func (m startModel) panel(lines []string, width int, focused bool) string {
	style := panelStyle
	if focused {
		style = focusedPanelStyle
	}
	inner := width - 4

	height := m.panelHeight()
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}

	rows := make([]string, len(lines))
	for i, l := range lines {
		rows[i] = padRight(l, inner)
	}
	// lipgloss counts border and padding inside Width, so the usable text
	// column is width-4 — the same figure callers lay their rows out to.
	return style.Width(width).Render(strings.Join(rows, "\n"))
}

// panelHeight is the number of content rows in a panel: the section title
// plus the scrolling list.
func (m startModel) panelHeight() int { return m.listHeight + 1 }

func sectionTitle(s string, focused bool) string {
	if focused {
		return titleStyle.Render(s)
	}
	return dimStyle.Render(s)
}

func (m startModel) statusLine() string {
	if m.st == stateFolder {
		return keyStyle.Render("convert into folder: ") + m.folder.View()
	}
	if m.st == stateFilter {
		return keyStyle.Render("filter: ") + m.filter.View()
	}
	if m.st == stateEditOption {
		return keyStyle.Render(m.editing.key+": ") + m.optEdit.View()
	}
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return errorStyle.Render("✗ " + truncate(m.status, m.width-2))
	}
	return successStyle.Render("✓ " + truncate(m.status, m.width-2))
}

func (m startModel) helpLine() string {
	var segs []string

	switch m.st {
	case stateFilter:
		segs = []string{"type to filter", "enter apply", "esc clear"}
	case stateFolder:
		segs = []string{"enter convert into folder", "esc cancel"}
	case stateEditOption:
		segs = []string{"enter save", "esc cancel"}
	case stateRunning:
		segs = []string{"esc cancel"}
	case stateResults:
		segs = []string{"any key to go back"}
	default:
		if m.focus == paneConfig {
			segs = []string{"↑↓ move", "space toggle", "←→ change", "s save defaults", "R reset", "tab files", "q quit"}
		} else {
			segs = []string{
				"↑↓ move", "←→ folders", "space select", "enter convert here", "f into folder…",
				"/ filter", "a all", "c clear", "tab settings", "q quit",
			}
		}
	}

	// Drop hints from the end until the line fits rather than wrapping.
	for len(segs) > 1 && lipgloss.Width(strings.Join(segs, " · ")) > m.width {
		segs = segs[:len(segs)-1]
	}
	return helpStyle.Render(strings.Join(segs, " · "))
}

// ----------------------------------------------------------------- strings

// truncate shortens s to width display cells, adding an ellipsis. It is only
// safe on unstyled text or on text whose styling ends at the end of the string.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// wrap greedily breaks s into lines no wider than width, splitting on spaces.
// A single word longer than the line is hard-truncated with an ellipsis.
func wrap(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line)+1+lipgloss.Width(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
		if lipgloss.Width(line) > width {
			lines = append(lines, truncate(line, width))
			line = ""
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// truncateLeft shortens s from the front, keeping its tail.
func truncateLeft(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

func padRight(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
