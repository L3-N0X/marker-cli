// Package tui holds the Bubble Tea interfaces: an interactive sign-in flow
// and a live progress view for conversions.
package tui

import "charm.land/lipgloss/v2"

var (
	accent = lipgloss.Color("#a27ded") // matches the Obsidian plugin's brand colour
	subtle = lipgloss.Color("241")
	red    = lipgloss.Color("203")
	green  = lipgloss.Color("78")

	titleStyle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	helpStyle    = lipgloss.NewStyle().Foreground(subtle)
	errorStyle   = lipgloss.NewStyle().Foreground(red)
	successStyle = lipgloss.NewStyle().Foreground(green)
	labelStyle   = lipgloss.NewStyle().Foreground(subtle)
)
