// Package tui holds the Bubble Tea interfaces: an interactive sign-in flow
// and a live progress view for conversions.
package tui

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// The palette deliberately leans on the terminal's own 16 ANSI colours rather
// than pinning hex values, so a user's colour scheme stays coherent instead of
// fighting hardcoded shades. Orange is the exception: it has no ANSI slot, so
// the accent is the one colour we pin — lipgloss degrades it to the nearest
// palette entry where truecolor is unavailable.
var (
	accent     = lipgloss.Color("#ff9d4d") // brand orange
	accentDeep = lipgloss.Color("#c2410c") // the dark end of the progress ramp

	// Body text is left unset so it inherits the terminal foreground; only the
	// secondary tiers get a colour.
	dim = lipgloss.Color("8")  // bright black: help, borders, de-emphasised rows
	fg  = lipgloss.Color("7")  // secondary text that still needs to be legible
	red = lipgloss.Color("9")  // errors
	grn = lipgloss.Color("10") // success
	ylw = lipgloss.Color("11") // warnings
	blu = lipgloss.Color("12") // directories
)

var (
	titleStyle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	helpStyle    = lipgloss.NewStyle().Foreground(dim)
	errorStyle   = lipgloss.NewStyle().Foreground(red)
	successStyle = lipgloss.NewStyle().Foreground(grn)
	labelStyle   = lipgloss.NewStyle().Foreground(fg)
	warnStyle    = lipgloss.NewStyle().Foreground(ylw)

	// Panels for the two-pane `start` browser. The focused one is tinted.
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dim).
			Padding(0, 1)
	focusedPanelStyle = panelStyle.BorderForeground(accent)

	// A row under the cursor, and a row belonging to an unfocused panel.
	cursorStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(dim)
	dirStyle    = lipgloss.NewStyle().Foreground(blu)
	keyStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
)

// styleInput applies the palette to a text input. Without this the bubbles
// defaults apply: a placeholder at 256-colour 240 that is all but invisible on
// a dark background, and a white block cursor that inverts whatever it sits on.
func styleInput(m *textinput.Model) {
	s := m.Styles()
	for _, st := range []*textinput.StyleState{&s.Focused, &s.Blurred} {
		st.Placeholder = lipgloss.NewStyle().Foreground(dim)
		st.Suggestion = lipgloss.NewStyle().Foreground(dim)
		st.Prompt = lipgloss.NewStyle().Foreground(accent)
	}
	s.Focused.Text = lipgloss.NewStyle()
	s.Blurred.Text = lipgloss.NewStyle().Foreground(dim)
	s.Cursor.Color = accent
	m.SetStyles(s)
}

// newProgressBar returns a progress bar on the accent ramp. The bubbles default
// is a purple-to-pink gradient that belongs to no theme in particular.
func newProgressBar(width int) progress.Model {
	m := progress.New(
		progress.WithWidth(width),
		progress.WithColors(accentDeep, accent),
	)
	m.EmptyColor = dim
	m.PercentageStyle = lipgloss.NewStyle().Foreground(fg)
	return m
}
